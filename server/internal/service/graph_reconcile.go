package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/graph"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// RunCoordinator is owned by the server lifecycle. Every pass examines committed
// nonterminal runs, including those whose notification or operation was lost.
func (s *GraphService) RunCoordinator(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.ReconcileOnce(ctx); err != nil && ctx.Err() == nil {
			slog.Error("graph reconciliation failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *GraphService) ReconcileOnce(ctx context.Context) error {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT workspace_id::text,id::text FROM graph_run WHERE state NOT IN ('succeeded','rejected','failed','cancelled') ORDER BY updated_at,id LIMIT 100`)
	if err != nil {
		return err
	}
	var ids [][2]string
	for rows.Next() {
		var pair [2]string
		if err = rows.Scan(&pair[0], &pair[1]); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, pair)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	var joined error
	for _, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, e := s.ReconcileRun(ctx, id[0], id[1]); e != nil {
			if diagnosticTx, beginErr := s.TxStarter.Begin(ctx); beginErr == nil {
				_, writeErr := diagnosticTx.Exec(ctx, `UPDATE graph_operation SET last_error=$3,available_at=now()+interval '3 seconds' WHERE workspace_id=$1 AND run_id=$2 AND consumed_at IS NULL`, id[0], id[1], e.Error())
				if writeErr == nil {
					writeErr = diagnosticTx.Commit(ctx)
				}
				if writeErr != nil {
					_ = diagnosticTx.Rollback(ctx)
				}
			}
			joined = errors.Join(joined, fmt.Errorf("run %s: %w", id[1], e))
		}
	}
	return joined
}

func (s *GraphService) ReconcileRun(ctx context.Context, ws, id string) (GraphRunRecord, error) {
	var run GraphRunRecord
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return run, err
	}
	defer tx.Rollback(ctx)
	// Multiple coordinator instances skip busy runs instead of blocking a batch.
	if err = graphLockWorkspace(ctx, tx, ws); err != nil {
		return run, err
	}
	var runData []byte
	err = tx.QueryRow(ctx, `SELECT document FROM graph_run WHERE workspace_id=$1 AND id=$2 FOR UPDATE SKIP LOCKED`, ws, id).Scan(&runData)
	if errors.Is(err, pgx.ErrNoRows) {
		return run, nil
	}
	if err != nil {
		return run, err
	}
	if err = json.Unmarshal(runData, &run); err != nil {
		return run, err
	}
	if graphTerminal(run.State) {
		return run, nil
	}
	before, err := graphHash(run)
	if err != nil {
		return run, err
	}
	if err = graphRefreshAttempts(ctx, tx, &run); err != nil {
		return run, err
	}
	for i := range run.Nodes {
		for j := range run.Nodes[i].Attempts {
			a := &run.Nodes[i].Attempts[j]
			if a.State == "cancelled" && a.StoppedAt == nil {
				if _, err = graphStopUnclaimedTx(ctx, tx, a); err != nil {
					return run, err
				}
			}
		}
	}
	version, err := graphLoadVersion(ctx, tx, ws, run.VersionID)
	if err != nil {
		return run, err
	}
	var notify []db.AgentTaskQueue
	var stop []string
	if run.Deadline != nil && !run.Deadline.After(time.Now()) && run.ControlState != "cancel_requested" {
		run.ControlState = "cancel_requested"
		run.TerminationReason = "deadline"
		graphEvent(&run, "run.deadline", "", "")
	}
	if run.ControlState == "cancel_requested" {
		allStopped := true
		for i := range run.Nodes {
			n := &run.Nodes[i]
			for j := range n.Attempts {
				a := &n.Attempts[j]
				if a.StoppedAt != nil {
					continue
				}
				if a.State == "queued" || a.State == "deferred" {
					var stopped bool
					stopped, err = graphStopUnclaimedTx(ctx, tx, a)
					if err != nil {
						return run, err
					}
					if !stopped {
						allStopped = false
						stop = append(stop, a.TaskID)
						continue
					}
				} else {
					allStopped = false
					stop = append(stop, a.TaskID)
				}
			}
		}
		if allStopped {
			state := "cancelled"
			if run.TerminationReason == "deadline" {
				state = "failed"
			}
			graphFinish(&run, state, run.TerminationReason)
		}
	} else {
		for i := range run.Nodes {
			node := &run.Nodes[i]
			if node.NodeType == "start" || node.State == "succeeded" {
				continue
			}
			if node.State == "blocked" && node.BlockReason != "execution_unknown" {
				break
			}
			if node.NodeType == "approval" {
				if run.ControlState == "pause" || !s.AdvanceEnabled {
					break
				}
				if run.Approval == nil {
					candidate := graph.ApprovalCandidate{VersionHash: version.ContentHash, InputHash: run.InputHash, Artifacts: run.Artifacts, AcceptedOutputs: map[string]string{}, EvidenceHashes: []string{}}
					for _, n := range run.Nodes {
						for _, sub := range n.Submissions {
							if sub.ID == n.AcceptedSubmissionID {
								candidate.AcceptedOutputs[n.NodeKey] = sub.ContentHash
								candidate.EvidenceHashes = append(candidate.EvidenceHashes, sub.ContentHash)
							}
						}
					}
					digest, e := graph.ApprovalDigest(candidate)
					if e != nil {
						return run, e
					}
					approver := ""
					for _, n := range version.Definition.Nodes {
						if n.Key == node.NodeKey {
							approver = n.ApproverID
						}
					}
					run.Approval = &GraphApprovalRecord{ID: uuid.NewString(), ApproverID: approver, CandidateDigest: digest, State: "pending", Revision: 1, CreatedAt: time.Now().UTC()}
					graphEvent(&run, "approval.requested", "", digest)
				}
				var exists bool
				if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM member WHERE workspace_id=$1 AND id=$2)`, ws, run.Approval.ApproverID).Scan(&exists); err != nil {
					return run, err
				}
				if !exists {
					run.State = "blocked"
					run.BlockReason = "approver_unavailable"
					node.State = "blocked"
					node.BlockReason = "approver_unavailable"
				} else {
					run.State = "waiting_approval"
					run.BlockReason = ""
					node.State = "waiting_approval"
					node.BlockReason = ""
				}
				break
			}
			if node.NodeType != "agent" {
				break
			}
			if len(node.Attempts) > 0 && (node.State == "active" || node.State == "validating" || node.BlockReason == "execution_unknown") {
				a := &node.Attempts[len(node.Attempts)-1]
				if a.State == "queued" || a.State == "dispatched" || a.State == "running" || a.State == "deferred" || a.State == "waiting_local_directory" {
					var started *time.Time
					var created time.Time
					if err = tx.QueryRow(ctx, `SELECT started_at,created_at FROM agent_task_queue WHERE id=$1`, a.TaskID).Scan(&started, &created); err != nil {
						return run, err
					}
					if started != nil && node.Spec != nil && time.Since(*started) > time.Duration(node.Spec.Node.TimeoutSeconds)*time.Second {
						run.State = "blocked"
						run.BlockReason = "execution_unknown"
						node.State = "blocked"
						node.BlockReason = "execution_unknown"
						stop = append(stop, a.TaskID)
					} else if started == nil && time.Since(created) > 30*time.Minute {
						run.State = "blocked"
						run.BlockReason = "queue_timeout"
						node.State = "blocked"
						node.BlockReason = "queue_timeout"
						stopped, stopErr := graphStopUnclaimedTx(ctx, tx, a)
						if stopErr != nil {
							return run, stopErr
						}
						if !stopped {
							stop = append(stop, a.TaskID)
						}
					}
					break
				}
				var submission *GraphSubmissionRecord
				if a.Receipt != nil {
					for j := range node.Submissions {
						if node.Submissions[j].ID == a.Receipt.SubmissionID {
							submission = &node.Submissions[j]
							break
						}
					}
				}
				var result *graph.Result
				outputValid, artifactsValid := false, false
				delivery := ""
				retryable := false
				if submission != nil && node.Spec != nil {
					result = &submission.Result
					outputValid = graph.ValidateResult(node.Spec.Node, *result) == nil
					artifactsValid = true
					if result.Outcome == graph.OutcomePass && node.Spec.Node.ExecutionMode == graph.ExecutionVerifyCommit {
						artifactsValid = node.Spec.Repository != nil && result.TestedCommitSHA == node.Spec.Repository.CommitSHA
					}
					if len(result.Artifacts) > 0 {
						artifactsValid = artifactsValid && s.ValidateArtifacts != nil && a.Receipt != nil && s.ValidateArtifacts(ctx, tx, run, *node, *result, *a.Receipt) == nil
					}
				}
				if a.Receipt != nil {
					delivery = a.Receipt.DeliveryStatus
					retryable = a.Receipt.Retryable
				}
				decision := graph.DecideCompletion(graph.Completion{RunState: graph.RunState(run.State), ControlState: graph.ControlState(run.ControlState), Expected: a.Manifest.Identity, Result: result, TaskState: a.State, ExecutionStopped: a.StoppedAt != nil, DeliveryStatus: delivery, OutputValid: outputValid, ArtifactsValid: artifactsValid, Retryable: retryable, Attempts: len(node.Attempts), MaxAttempts: node.MaxAttempts})
				node.State = string(decision.NodeState)
				node.BlockReason = decision.Reason
				run.State = string(decision.RunState)
				run.BlockReason = decision.Reason
				if decision.Retry && a.Receipt != nil && a.StoppedAt != nil {
					if delay := retryDelayForAttempt(a.Receipt.FailureReason, int32(a.AttemptNo)); delay > 0 {
						at := a.StoppedAt.Add(delay)
						node.NextAttemptAt = &at
					}
				}
				if node.State == "succeeded" && submission != nil {
					node.AcceptedSubmissionID = submission.ID
					node.Output = submission.Result.Output
					node.BlockReason = ""
					run.BlockReason = ""
					if err = graphRetainArtifacts(ctx, tx, &run, *node, submission.Result); err != nil {
						return run, err
					}
					graphEvent(&run, "node.accepted", a.TaskID, node.NodeKey)
				}
				if graphTerminal(run.State) {
					graphFinish(&run, run.State, decision.Reason)
					break
				}
				if node.State == "blocked" {
					break
				}
				if node.State == "succeeded" {
					continue
				}
			}
			if run.ControlState != "run" || !s.AdvanceEnabled {
				break
			}
			if node.State == "pending" {
				node.State = "ready"
			}
			if node.State != "ready" {
				break
			}
			if node.NextAttemptAt != nil && node.NextAttemptAt.After(time.Now()) {
				break
			}
			if s.AuthorizeRun == nil || s.CreateTask == nil {
				return run, graphError("capability_missing", "Graph execution adapters unavailable")
			}
			if e := s.authorizeVersion(ctx, tx, ws, run.InitiatorID, version, run.RuntimeBindings); e != nil {
				run.State = "blocked"
				run.BlockReason = "authorization_revoked"
				node.State = "blocked"
				node.BlockReason = "authorization_revoked"
				graphEvent(&run, "node.blocked", "", e.Error())
				break
			}
			if len(node.Attempts) >= node.MaxAttempts {
				node.State = "failed"
				graphFinish(&run, "failed", "budget_exhausted")
				break
			}
			if len(node.Attempts) > 0 && node.Attempts[len(node.Attempts)-1].StoppedAt == nil {
				node.State = "blocked"
				node.BlockReason = "execution_unknown"
				run.State = "blocked"
				run.BlockReason = node.BlockReason
				break
			}
			if node.Spec == nil {
				var definitionNode graph.Node
				for _, n := range version.Definition.Nodes {
					if n.Key == node.NodeKey {
						definitionNode = n
					}
				}
				outputs := map[string]json.RawMessage{}
				for _, n := range run.Nodes {
					if n.AcceptedSubmissionID != "" {
						outputs[n.NodeKey] = n.Output
					}
				}
				input, e := graph.ResolveInputs(definitionNode, run.Input, outputs)
				if e != nil {
					node.State = "blocked"
					node.BlockReason = "input_invalid"
					run.State = "blocked"
					run.BlockReason = node.BlockReason
					graphEvent(&run, "node.blocked", "", e.Error())
					break
				}
				normalized, hash, e := graph.CanonicalJSON(input)
				if e != nil {
					return run, e
				}
				snapshot := version.Snapshots[node.NodeKey]
				var repository *graph.RepositoryInput
				if run.Repository != nil {
					r := *run.Repository
					repository = &r
					for _, artifact := range run.Artifacts {
						if artifact.Type == graph.ArtifactGitCommit && artifact.RepositoryID == r.RepositoryID {
							repository.CommitSHA = artifact.CommitSHA
							repository.ArtifactID = artifact.ID
						}
					}
				}
				node.Input = normalized
				node.InputHash = hash
				node.Spec = &graph.NodeSpec{ProtocolVersion: graph.ProtocolVersion, Node: definitionNode, Input: normalized, InputHash: hash, AgentConfig: snapshot.AgentConfig, Skills: snapshot.Skills, Repository: repository}
			}
			number := len(node.Attempts) + 1
			taskID := uuid.NewString()
			identity := graph.AttemptIdentity{RunID: run.ID, NodeRunID: node.ID, TaskID: taskID, AttemptNo: number, ExecutionGeneration: int64(number), InputHash: node.InputHash}
			manifest, e := graph.NewAttemptManifest(*node.Spec, identity)
			if e != nil {
				node.State = "blocked"
				node.BlockReason = "specification_invalid"
				run.State = "blocked"
				run.BlockReason = node.BlockReason
				graphEvent(&run, "node.blocked", "", e.Error())
				break
			}
			manifestJSON, manifestHash, e := graph.CanonicalManifest(manifest)
			if e != nil {
				return run, e
			}
			// Bind before inserting the queue row: the database admission trigger
			// can reject every ordinary writer while allowing this exact identity.
			_, err = tx.Exec(ctx, `INSERT INTO graph_task_attempt(task_id,workspace_id,run_id,node_run_id,attempt_no,execution_generation,input_hash,manifest,manifest_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, taskID, ws, run.ID, node.ID, number, identity.ExecutionGeneration, node.InputHash, manifestJSON, manifestHash)
			if err != nil {
				return run, err
			}
			previous := ""
			if len(node.Attempts) > 0 {
				previous = node.Attempts[len(node.Attempts)-1].TaskID
			}
			task, e := s.CreateTask(ctx, tx, GraphTaskCreateParams{WorkspaceID: ws, RunID: run.ID, NodeRunID: node.ID, TaskID: taskID, IssueID: node.IssueID, AgentID: node.Spec.Node.AgentID, RuntimeID: run.RuntimeBindings[node.NodeKey], InitiatorID: run.InitiatorID, AttemptNo: number, Generation: identity.ExecutionGeneration, PreviousTaskID: previous, Manifest: manifest, ManifestJSON: manifestJSON, ManifestHash: manifestHash})
			if e != nil {
				return run, e
			}
			node.Attempts = append(node.Attempts, GraphAttemptRecord{TaskID: taskID, AttemptNo: number, Generation: identity.ExecutionGeneration, State: "queued", Manifest: manifest, ManifestHash: manifestHash, CreatedAt: time.Now().UTC()})
			node.NextAttemptAt = nil
			node.State = "active"
			node.BlockReason = ""
			run.State = "active"
			run.BlockReason = ""
			graphEvent(&run, "attempt.created", run.InitiatorID, taskID)
			notify = append(notify, task)
			break
		}
	}
	after, err := graphHash(run)
	if err != nil {
		return run, err
	}
	if before != after {
		if err = graphPersistRun(ctx, tx, &run); err != nil {
			return run, err
		}
	} else {
		if _, err = tx.Exec(ctx, `UPDATE graph_run SET updated_at=now() WHERE workspace_id=$1 AND id=$2`, ws, id); err != nil {
			return run, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE graph_operation SET consumed_at=now(),last_error=NULL WHERE workspace_id=$1 AND run_id=$2 AND consumed_at IS NULL`, ws, id); err != nil {
		return run, err
	}
	if err = tx.Commit(ctx); err != nil {
		return run, err
	}
	if before != after && s.NotifyRun != nil {
		s.NotifyRun(ctx, run)
	}
	for _, task := range notify {
		if s.NotifyTask != nil {
			s.NotifyTask(ctx, task)
		}
	}
	for _, taskID := range stop {
		if s.RequestStop != nil {
			if e := s.RequestStop(ctx, taskID); e != nil {
				slog.Warn("graph stop request failed; will retry", "task_id", taskID, "error", e)
			}
		}
	}
	return run, nil
}

func graphRetainArtifacts(ctx context.Context, tx pgx.Tx, run *GraphRunRecord, node GraphNodeRunRecord, result graph.Result) error {
	for _, artifact := range result.Artifacts {
		seen := false
		for _, existing := range run.Artifacts {
			if existing.ID == artifact.ID {
				a, err := graphHash(existing)
				if err != nil {
					return err
				}
				b, err := graphHash(artifact)
				if err != nil {
					return err
				}
				if a != b {
					return graphError("artifact_conflict", "an artifact identity cannot change content")
				}
				seen = true
			}
		}
		if seen {
			continue
		}
		if err := graphUUIDs(artifact.ID); err != nil {
			return err
		}
		hash, err := graphHash(artifact)
		if err != nil {
			return err
		}
		b, err := graphJSON(artifact)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO graph_artifact(id,workspace_id,run_id,node_run_id,task_id,content_hash,document) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(id) DO NOTHING`, artifact.ID, run.WorkspaceID, run.ID, node.ID, artifact.ProducerTaskID, hash, b)
		if err != nil {
			return err
		}
		var storedHash string
		if err = tx.QueryRow(ctx, `SELECT content_hash FROM graph_artifact WHERE id=$1 AND workspace_id=$2`, artifact.ID, run.WorkspaceID).Scan(&storedHash); err != nil {
			return graphError("artifact_conflict", "artifact belongs to another workspace")
		}
		if storedHash != hash {
			return graphError("artifact_conflict", "artifact content or producer cannot change")
		}
		run.Artifacts = append(run.Artifacts, artifact)
	}
	return nil
}
