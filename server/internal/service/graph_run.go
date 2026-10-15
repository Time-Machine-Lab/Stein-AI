package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/graph"
)

func (s *GraphService) StartRun(ctx context.Context, ws, user string, p GraphStartParams) (GraphRunRecord, error) {
	var run GraphRunRecord
	if !s.NewRunsEnabled {
		return run, graphError("feature_disabled", "new Graph runs are disabled")
	}
	if err := graphUUIDs(p.VersionID, p.RootIssueID); err != nil {
		return run, err
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return run, err
	}
	defer tx.Rollback(ctx)
	if _, _, err = graphMember(ctx, tx, ws, user); err != nil {
		return run, err
	}
	replayed, hash, err := graphCommand(ctx, tx, ws, p.IdempotencyKey, []any{"start", user, p}, &run)
	if err != nil {
		return run, err
	}
	if replayed {
		return run, tx.Commit(ctx)
	}
	version, err := graphLoadVersion(ctx, tx, ws, p.VersionID)
	if err != nil {
		return run, err
	}
	if len(p.Input) == 0 {
		p.Input = json.RawMessage(`{}`)
	}
	if err = graph.ValidateJSON(version.Definition.InputSchema, p.Input); err != nil {
		return run, graphError("input_invalid", err.Error())
	}
	input, inputHash, err := graph.CanonicalJSON(p.Input)
	if err != nil {
		return run, err
	}
	if p.Deadline != nil && !p.Deadline.After(time.Now()) {
		return run, graphError("deadline_exceeded", "deadline must be in the future")
	}
	if p.Deadline == nil {
		deadline := time.Now().UTC().Add(24 * time.Hour)
		p.Deadline = &deadline
	}
	if s.AuthorizeRun == nil || s.CreateTask == nil {
		return run, graphError("capability_missing", "Graph execution adapters are unavailable")
	}
	if err = s.authorizeVersion(ctx, tx, ws, user, version, p.RuntimeBindings); err != nil {
		return run, err
	}
	err = graph.WalkNodes(version.Definition, func(path string, node graph.Node) error {
		if node.Type == graph.NodeTypeAgent || node.Type == graph.NodeTypeScript {
			runtime := p.RuntimeBindings[path]
			if err = graphUUIDs(runtime); err != nil {
				return graphError("runtime_required", "every executable node requires a fixed runtime")
			}
			if string(node.ExecutionMode) != "none" && p.Repository == nil {
				return graphError("repository_required", "code nodes require a fixed repository input")
			}
		}
		return nil
	})
	if err != nil {
		return run, err
	}
	var rootSnapshot []byte
	var parentID *string
	err = tx.QueryRow(ctx, `SELECT jsonb_build_object('title',title,'description',description,'project_id',project_id,'workspace_id',workspace_id),parent_issue_id::text FROM issue WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, ws, p.RootIssueID).Scan(&rootSnapshot, &parentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return run, graphError("not_found", "root issue not found")
	}
	if err != nil {
		return run, err
	}
	if parentID != nil {
		return run, graphError("invalid_root", "Graph requires a top-level issue")
	}
	if s.PrepareRun == nil {
		return run, graphError("capability_missing", "Graph run snapshot adapter is unavailable")
	}
	rootSnapshot, err = s.PrepareRun(ctx, tx, ws, user, rootSnapshot, p.Repository)
	if err != nil {
		return run, err
	}
	// Lock every existing child in the same order as ordinary issue admission.
	rows, err := tx.Query(ctx, `SELECT id FROM issue WHERE workspace_id=$1 AND parent_issue_id=$2 ORDER BY id FOR UPDATE`, ws, p.RootIssueID)
	if err != nil {
		return run, err
	}
	for rows.Next() {
		var unused any
		if err = rows.Scan(&unused); err != nil {
			rows.Close()
			return run, err
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return run, err
	}
	var occupied bool
	err = tx.QueryRow(ctx, `SELECT
EXISTS(SELECT 1 FROM graph_run WHERE root_issue_id=$1 AND state NOT IN ('succeeded','rejected','failed','cancelled'))
OR EXISTS(SELECT 1 FROM issue i WHERE i.workspace_id=$2 AND i.parent_issue_id=$1 AND i.status NOT IN ('done','cancelled') AND NOT EXISTS(SELECT 1 FROM graph_issue_owner o WHERE o.issue_id=i.id))
OR EXISTS(SELECT 1 FROM agent_task_queue t JOIN issue i ON i.id=t.issue_id WHERE i.workspace_id=$2 AND (i.id=$1 OR i.parent_issue_id=$1) AND t.status IN ('deferred','queued','dispatched','running','waiting_local_directory'))
OR EXISTS(SELECT 1 FROM graph_task_attempt a JOIN graph_run r ON r.id=a.run_id WHERE r.workspace_id=$2 AND r.root_issue_id=$1 AND a.stopped_at IS NULL)
OR EXISTS(SELECT 1 FROM autopilot_run WHERE issue_id=$1 AND status IN ('issue_created','running'))`, p.RootIssueID, ws).Scan(&occupied)
	if err != nil {
		return run, err
	}
	if occupied {
		return run, graphError("execution_active", "root issue or its children still have unfinished work or unconfirmed execution")
	}
	if p.PreviousRunID != "" {
		previous, e := graphLoadRun(ctx, tx, ws, p.PreviousRunID, false)
		if e != nil {
			return run, e
		}
		if previous.RootIssueID != p.RootIssueID || !graphTerminal(previous.State) {
			return run, graphError("invalid_previous_run", "previous run must be terminal and belong to this root issue")
		}
	}
	ordered, err := graph.OrderedNodes(version.Definition)
	if err != nil {
		return run, err
	}
	inputHash, err = graphHash([]any{json.RawMessage(input), json.RawMessage(rootSnapshot), p.Repository})
	if err != nil {
		return run, err
	}
	now := time.Now().UTC()
	run = GraphRunRecord{ID: uuid.NewString(), WorkspaceID: ws, RootIssueID: p.RootIssueID, VersionID: p.VersionID, PreviousRunID: p.PreviousRunID, InitiatorID: user, State: "active", ControlState: "run", Revision: 0, Input: input, InputHash: inputHash, RootSnapshot: rootSnapshot, RuntimeBindings: p.RuntimeBindings, Repository: p.Repository, Deadline: p.Deadline, CreatedAt: now, UpdatedAt: now, Nodes: []GraphNodeRunRecord{}, Events: []GraphRunEvent{}}
	run.SchemaVersion = version.Definition.SchemaVersion
	run.GraphID = version.GraphID
	run.GraphName = version.Definition.Name
	run.Definition = &version.Definition
	if _, err = tx.Exec(ctx, `SELECT set_config('multica.graph_run_id',$1,true)`, run.ID); err != nil {
		return run, err
	}
	owned, err := tx.Exec(ctx, `INSERT INTO graph_issue_owner(issue_id,workspace_id,run_id) VALUES($1,$2,$3) ON CONFLICT(issue_id) DO UPDATE SET run_id=EXCLUDED.run_id WHERE graph_issue_owner.node_run_id IS NULL AND graph_issue_owner.workspace_id=EXCLUDED.workspace_id`, p.RootIssueID, ws, run.ID)
	if err != nil {
		return run, err
	}
	if owned.RowsAffected() != 1 {
		return run, graphError("invalid_root", "issue is owned by another Graph node")
	}
	if run.SchemaVersion == 2 {
		if err = graphCreateScopeV2(ctx, tx, &run, version.Definition, "", "", 0, input, false); err != nil {
			return run, err
		}
	} else {
		for _, node := range ordered {
			n := GraphNodeRunRecord{ID: uuid.NewString(), NodeKey: node.Key, Label: node.Label, NodeType: string(node.Type), State: "pending", MaxAttempts: node.MaxAttempts, Attempts: []GraphAttemptRecord{}, Submissions: []GraphSubmissionRecord{}}
			if n.NodeType == "start" {
				n.State = "succeeded"
			}
			if n.NodeType == "agent" {
				var number int64
				if err = tx.QueryRow(ctx, `UPDATE workspace SET issue_counter=issue_counter+1 WHERE id=$1 RETURNING issue_counter`, ws).Scan(&number); err != nil {
					return run, err
				}
				n.IssueID = uuid.NewString()
				_, err = tx.Exec(ctx, `INSERT INTO issue(id,workspace_id,title,description,status,priority,assignee_type,assignee_id,creator_type,creator_id,parent_issue_id,project_id,number,position) SELECT $1,workspace_id,$3,$4,'todo','none','agent',$5,'member',$6,id,project_id,$7,0 FROM issue WHERE id=$2 AND workspace_id=$8`, n.IssueID, p.RootIssueID, version.Definition.Name+" · "+node.Key, node.Instructions, node.AgentID, user, number, ws)
				if err != nil {
					return run, err
				}
				_, err = tx.Exec(ctx, `INSERT INTO graph_issue_owner(issue_id,workspace_id,run_id,node_run_id) VALUES($1,$2,$3,$4)`, n.IssueID, ws, run.ID, n.ID)
				if err != nil {
					return run, err
				}
			}
			run.Nodes = append(run.Nodes, n)
		}
	}
	b, err := graphJSON(run)
	if err != nil {
		return run, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO graph_run(id,workspace_id,root_issue_id,version_id,initiator_id,state,control_state,document) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, run.ID, ws, p.RootIssueID, p.VersionID, user, run.State, run.ControlState, b)
	if err != nil {
		return run, err
	}
	for _, n := range run.Nodes {
		nb, e := graphJSON(n)
		if e != nil {
			return run, e
		}
		var issueID any
		if n.IssueID != "" {
			issueID = n.IssueID
		}
		key := n.NodeKey
		if n.InstanceKey != "" {
			key = n.InstanceKey
		}
		_, err = tx.Exec(ctx, `INSERT INTO graph_node_run(id,workspace_id,run_id,node_key,issue_id,state,document) VALUES($1,$2,$3,$4,$5,$6,$7)`, n.ID, ws, run.ID, key, issueID, n.State, nb)
		if err != nil {
			return run, err
		}
	}
	graphEvent(&run, "run.started", user, "")
	if err = graphOperation(ctx, tx, run, "start:"+run.ID, "reconcile"); err != nil {
		return run, err
	}
	if err = graphPersistRun(ctx, tx, &run); err != nil {
		return run, err
	}
	if err = graphSaveCommand(ctx, tx, ws, p.IdempotencyKey, hash, run); err != nil {
		return run, err
	}
	return run, tx.Commit(ctx)
}

func (s *GraphService) ListRuns(ctx context.Context, ws, user string) ([]GraphRunRecord, error) {
	result := []GraphRunRecord{}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, _, err = graphMember(ctx, tx, ws, user); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT document FROM graph_run WHERE workspace_id=$1 ORDER BY created_at DESC LIMIT 100`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var run GraphRunRecord
		if err = graphScan(rows, &run); err != nil {
			return nil, err
		}
		result = append(result, run)
	}
	return result, rows.Err()
}
func (s *GraphService) GetRun(ctx context.Context, ws, user, id string) (GraphRunRecord, error) {
	var run GraphRunRecord
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return run, err
	}
	defer tx.Rollback(ctx)
	if _, _, err = graphMember(ctx, tx, ws, user); err != nil {
		return run, err
	}
	run, err = graphLoadRun(ctx, tx, ws, id, false)
	if err != nil {
		return run, err
	}
	if err = graphRefreshAttempts(ctx, tx, &run); err != nil {
		return run, err
	}
	if graphTerminal(run.State) {
		var unsafe bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM graph_run WHERE workspace_id=$1 AND root_issue_id=$2 AND state NOT IN ('succeeded','rejected','failed','cancelled')) OR EXISTS(SELECT 1 FROM graph_task_attempt a JOIN agent_task_queue t ON t.id=a.task_id WHERE a.workspace_id=$1 AND a.run_id IN (SELECT id FROM graph_run WHERE root_issue_id=$2) AND (a.stopped_at IS NULL OR t.status IN ('deferred','queued','dispatched','running','waiting_local_directory'))) OR NOT EXISTS(SELECT 1 FROM graph_issue_owner WHERE workspace_id=$1 AND issue_id=$2 AND node_run_id IS NULL)`, ws, run.RootIssueID).Scan(&unsafe)
		if err != nil {
			return run, err
		}
		run.CanDetach = !unsafe
	}
	return run, nil
}
func graphRunController(run GraphRunRecord, user, role string) error {
	if run.InitiatorID != user && role != "owner" && role != "admin" {
		return graphError("forbidden", "only the initiator or a workspace administrator can control this run")
	}
	return nil
}

func (s *GraphService) Control(ctx context.Context, ws, user, id, action string, p GraphControlParams) (GraphRunRecord, error) {
	var run GraphRunRecord
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return run, err
	}
	defer tx.Rollback(ctx)
	_, role, err := graphMember(ctx, tx, ws, user)
	if err != nil {
		return run, err
	}
	replayed, hash, err := graphCommand(ctx, tx, ws, p.IdempotencyKey, []any{action, user, id, p}, &run)
	if err != nil {
		return run, err
	}
	if replayed {
		return run, tx.Commit(ctx)
	}
	run, err = graphLoadRun(ctx, tx, ws, id, true)
	if err != nil {
		return run, err
	}
	if err = graphRunController(run, user, role); err != nil {
		return run, err
	}
	if run.Revision != p.ExpectedRevision {
		return run, graphError("revision_conflict", "run changed; reload before controlling it")
	}
	if graphTerminal(run.State) && action != "archive" {
		return run, graphError("terminal_run", "terminal runs cannot be resumed")
	}
	if run.ControlState == "cancel_requested" && action != "cancel" && !graphTerminal(run.State) {
		return run, graphError("cancel_requested", "run is stopping")
	}
	if err = graphRefreshAttempts(ctx, tx, &run); err != nil {
		return run, err
	}
	switch action {
	case "pause":
		run.ControlState = "pause"
	case "resume":
		if run.ControlState != "pause" {
			return run, graphError("invalid_state", "run is not paused")
		}
		run.ControlState = "run"
	case "cancel":
		run.ControlState = "cancel_requested"
		run.TerminationReason = "user_cancelled"
	case "retry", "resolve-block":
		if run.ControlState != "run" {
			return run, graphError("run_paused", "resume before retrying")
		}
		found := false
		for i := range run.Nodes {
			n := &run.Nodes[i]
			if n.ID != p.NodeRunID {
				continue
			}
			found = true
			if run.SchemaVersion == 2 && n.NodeType == "http" {
				if n.State != "blocked" || n.Effect == nil || n.Effect.State == "dispatched" {
					return run, graphError("invalid_state", "only a stopped blocked HTTP invocation can retry")
				}
				if n.Effect.Attempts >= 3 {
					return run, graphError("budget_exhausted", "HTTP attempt budget exhausted")
				}
				if strings.TrimSpace(p.Reason) == "" {
					return run, graphError("retry_reason_required", "verify the remote outcome and provide a reason before retrying this HTTP invocation")
				}
				n.State = "ready"
				n.BlockReason = ""
				n.Effect.State = "retryable"
				n.NextAttemptAt = nil
				run.State = "active"
				run.BlockReason = ""
				continue
			}
			if action == "retry" && len(n.Attempts) == 0 {
				return run, graphError("attempt_missing", "resolve the block before the first attempt")
			}
			gate := graphRetryAllowed
			if action == "resolve-block" {
				gate = graphResolveBlockAllowed
			}
			if err = gate(*n); err != nil {
				return run, err
			}
			if action == "resolve-block" {
				if len(n.Attempts) > 0 {
					if err = graphConfirmStoppedTx(ctx, tx, ws, &n.Attempts[len(n.Attempts)-1], user); err != nil {
						return run, err
					}
				}
				graphGrantUnblock(n, user, p.Reason)
			}
			n.State = "ready"
			n.BlockReason = ""
			run.State = "active"
			run.BlockReason = ""
		}
		if !found {
			return run, graphError("not_found", "node does not belong to this run")
		}
	case "archive":
		if !graphTerminal(run.State) {
			return run, graphError("invalid_state", "only terminal runs can be archived")
		}
		now := time.Now().UTC()
		run.ArchivedAt = &now
	case "approver":
		if role != "owner" && role != "admin" {
			return run, graphError("forbidden", "administrator permission required")
		}
		if run.SchemaVersion == 2 {
			if err = graphReplaceApproverV2(ctx, tx, &run, user, p); err != nil {
				return run, err
			}
			break
		}
		if run.Approval == nil || (run.State != "waiting_approval" && run.BlockReason != "approver_unavailable") {
			return run, graphError("invalid_state", "there is no pending approval")
		}
		if err = graphUUIDs(p.ApproverID); err != nil {
			return run, err
		}
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM member WHERE workspace_id=$1 AND id=$2)`, ws, p.ApproverID).Scan(&exists); err != nil {
			return run, err
		}
		if !exists {
			return run, graphError("approver_unavailable", "member not found")
		}
		if _, err = tx.Exec(ctx, `UPDATE graph_approval SET state='invalidated',document=jsonb_set(document,'{state}','"invalidated"') WHERE workspace_id=$1 AND run_id=$2 AND state='pending'`, ws, id); err != nil {
			return run, err
		}
		run.Approval = &GraphApprovalRecord{ID: uuid.NewString(), ApproverID: p.ApproverID, CandidateDigest: run.Approval.CandidateDigest, State: "pending", Revision: run.Approval.Revision + 1, CreatedAt: time.Now().UTC()}
		run.State = "waiting_approval"
		run.BlockReason = ""
		for i := range run.Nodes {
			if run.Nodes[i].NodeType == "approval" {
				run.Nodes[i].State = "waiting_approval"
				run.Nodes[i].BlockReason = ""
			}
		}
	default:
		return run, graphError("invalid_action", "unsupported Graph control action")
	}
	graphEvent(&run, "run."+action, user, p.Reason)
	if err = graphOperation(ctx, tx, run, fmt.Sprintf("control:%s:%d", id, run.Revision+1), "reconcile"); err != nil {
		return run, err
	}
	if err = graphPersistRun(ctx, tx, &run); err != nil {
		return run, err
	}
	if err = graphSaveCommand(ctx, tx, ws, p.IdempotencyKey, hash, run); err != nil {
		return run, err
	}
	return run, tx.Commit(ctx)
}

func (s *GraphService) Approve(ctx context.Context, ws, user, id string, p GraphApprovalParams) (GraphRunRecord, error) {
	var run GraphRunRecord
	if !s.AdvanceEnabled {
		return run, graphError("run_paused", "Graph advancement is disabled")
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return run, err
	}
	defer tx.Rollback(ctx)
	member, _, err := graphMember(ctx, tx, ws, user)
	if err != nil {
		return run, err
	}
	replayed, hash, err := graphCommand(ctx, tx, ws, p.IdempotencyKey, []any{"approval", user, id, p}, &run)
	if err != nil {
		return run, err
	}
	if replayed {
		return run, tx.Commit(ctx)
	}
	run, err = graphLoadRun(ctx, tx, ws, id, true)
	if err != nil {
		return run, err
	}
	if run.Revision != p.ExpectedRevision {
		return run, graphError("revision_conflict", "run changed")
	}
	if run.SchemaVersion == 2 {
		return s.approveV2(ctx, tx, run, member, user, p, hash)
	}
	if err = graphApprovalAllowed(run, member, p); err != nil {
		return run, err
	}
	now := time.Now().UTC()
	run.Approval.State = p.Decision
	run.Approval.DecidedBy = user
	run.Approval.DecidedAt = &now
	run.Approval.Reason = p.Reason
	if p.Decision == "approved" {
		for i := range run.Nodes {
			if run.Nodes[i].NodeType == "approval" || run.Nodes[i].NodeType == "end" {
				run.Nodes[i].State = "succeeded"
			}
		}
		graphFinish(&run, "succeeded", "")
	} else {
		for i := range run.Nodes {
			if run.Nodes[i].NodeType == "approval" {
				run.Nodes[i].State = "rejected"
			}
		}
		graphFinish(&run, "rejected", p.Reason)
	}
	graphEvent(&run, "approval."+p.Decision, user, p.Reason)
	if err = graphPersistRun(ctx, tx, &run); err != nil {
		return run, err
	}
	if err = graphSaveCommand(ctx, tx, ws, p.IdempotencyKey, hash, run); err != nil {
		return run, err
	}
	return run, tx.Commit(ctx)
}

func (s *GraphService) DetachIssue(ctx context.Context, ws, user, issueID, key string) error {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, role, err := graphMember(ctx, tx, ws, user)
	if err != nil {
		return err
	}
	if err = graphUUIDs(issueID); err != nil {
		return err
	}
	var result map[string]bool
	replayed, hash, err := graphCommand(ctx, tx, ws, key, []any{"detach", user, issueID}, &result)
	if err != nil {
		return err
	}
	if replayed {
		return tx.Commit(ctx)
	}
	var rootID string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM issue WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, ws, issueID).Scan(&rootID); err != nil {
		return err
	}
	var runID string
	var nodeID *string
	if err = tx.QueryRow(ctx, `SELECT run_id::text,node_run_id::text FROM graph_issue_owner WHERE workspace_id=$1 AND issue_id=$2`, ws, issueID).Scan(&runID, &nodeID); err != nil {
		return err
	}
	if nodeID != nil {
		return graphError("graph_managed", "historical node issues cannot be detached")
	}
	run, err := graphLoadRun(ctx, tx, ws, runID, true)
	if err != nil {
		return err
	}
	if err = graphRunController(run, user, role); err != nil {
		return err
	}
	if !graphTerminal(run.State) {
		return graphError("execution_active", "run must be terminal before detaching")
	}
	var unsafe bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM graph_task_attempt a JOIN agent_task_queue t ON t.id=a.task_id WHERE a.workspace_id=$1 AND a.run_id IN(SELECT id FROM graph_run WHERE root_issue_id=$2) AND (a.stopped_at IS NULL OR t.status IN ('deferred','queued','dispatched','running','waiting_local_directory')))`, ws, issueID).Scan(&unsafe)
	if err != nil {
		return err
	}
	if unsafe {
		return graphError("execution_unknown", "all historical executions must be confirmed stopped")
	}
	if _, err = tx.Exec(ctx, `DELETE FROM graph_issue_owner WHERE workspace_id=$1 AND issue_id=$2 AND node_run_id IS NULL`, ws, issueID); err != nil {
		return err
	}
	graphEvent(&run, "issue.detached", user, "")
	if err = graphPersistRun(ctx, tx, &run); err != nil {
		return err
	}
	if err = graphSaveCommand(ctx, tx, ws, key, hash, map[string]bool{"detached": true}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
