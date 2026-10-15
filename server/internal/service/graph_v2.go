package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/graph"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func graphCreateScopeV2(ctx context.Context, tx pgx.Tx, run *GraphRunRecord, d graph.Definition, path, parent string, iteration int, input json.RawMessage, persist bool) error {
	ordered, err := graph.TopologicalNodes(d)
	if err != nil {
		return err
	}
	if len(run.Nodes)+len(ordered) > graph.MaxExecutionInstances {
		return graphError("instance_budget_exhausted", "run exceeds the execution instance budget")
	}
	if !graphPayloadFitsV2(*run, len(input)+len(ordered)*2048) {
		return graphError("storage_budget_exhausted", "child scope exceeds run storage budget")
	}
	if err := graph.ValidateJSON(d.InputSchema, input); err != nil {
		return graphError("input_invalid", err.Error())
	}
	scope := GraphScopeRecord{ID: uuid.NewString(), ParentNodeRunID: parent, DefinitionPath: path, Iteration: iteration, Input: input, State: "active"}
	now := time.Now().UTC()
	for _, node := range ordered {
		staticPath := node.Key
		if path != "" {
			staticPath = path + "/" + node.Key
		}
		n := GraphNodeRunRecord{ID: uuid.NewString(), NodeKey: node.Key, Label: node.Label, NodeType: string(node.Type), State: "pending", ScopeID: scope.ID, InstanceKey: scope.ID + "/" + node.Key, DefinitionPath: staticPath, Iteration: iteration, MaxAttempts: node.MaxAttempts, Attempts: []GraphAttemptRecord{}, Submissions: []GraphSubmissionRecord{}}
		if node.Type == graph.NodeTypeStart {
			n.State = "succeeded"
			n.StartedAt = &now
			n.FinishedAt = &now
		}
		if node.Type == graph.NodeTypeAgent || node.Type == graph.NodeTypeScript {
			var number int64
			if err = tx.QueryRow(ctx, `UPDATE workspace SET issue_counter=issue_counter+1 WHERE id=$1 RETURNING issue_counter`, run.WorkspaceID).Scan(&number); err != nil {
				return err
			}
			n.IssueID = uuid.NewString()
			if _, err = tx.Exec(ctx, `INSERT INTO issue(id,workspace_id,title,description,status,priority,assignee_type,assignee_id,creator_type,creator_id,parent_issue_id,project_id,number,position) SELECT $1,workspace_id,$3,$4,'todo','none','agent',$5,'member',$6,id,project_id,$7,0 FROM issue WHERE id=$2 AND workspace_id=$8`, n.IssueID, run.RootIssueID, d.Name+" · "+staticPath, node.Instructions, node.AgentID, run.InitiatorID, number, run.WorkspaceID); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO graph_issue_owner(issue_id,workspace_id,run_id,node_run_id) VALUES($1,$2,$3,$4)`, n.IssueID, run.WorkspaceID, run.ID, n.ID); err != nil {
				return err
			}
		}
		if persist {
			raw, err := graphJSON(n)
			if err != nil {
				return err
			}
			var issue any
			if n.IssueID != "" {
				issue = n.IssueID
			}
			if _, err = tx.Exec(ctx, `INSERT INTO graph_node_run(id,workspace_id,run_id,node_key,issue_id,state,document) VALUES($1,$2,$3,$4,$5,$6,$7)`, n.ID, run.WorkspaceID, run.ID, n.InstanceKey, issue, n.State, raw); err != nil {
				return err
			}
		}
		run.Nodes = append(run.Nodes, n)
	}
	run.Scopes = append(run.Scopes, scope)
	graphEvent(run, "scope.started", run.InitiatorID, scope.ID)
	return nil
}

func scopeDefinitionV2(root graph.Definition, path string) (graph.Definition, error) {
	if path == "" {
		return root, nil
	}
	parts := strings.Split(path, "/")
	if len(parts)%2 != 0 {
		return root, graphError("scope_invalid", "invalid definition path")
	}
	current := root
	for i := 0; i < len(parts); i += 2 {
		found := false
		for _, n := range current.Nodes {
			if n.Key != parts[i] {
				continue
			}
			if parts[i+1] == "body" && n.Loop != nil {
				current = n.Loop.Body
				found = true
			} else if parts[i+1] == "definition" && n.Subgraph != nil {
				current = n.Subgraph.Definition
				found = true
			}
			break
		}
		if !found {
			return root, graphError("scope_invalid", "definition path is unavailable")
		}
	}
	return current, nil
}

func graphScopeContextV2(run GraphRunRecord, scope GraphScopeRecord) graph.EvaluationContext {
	outputs := map[string]json.RawMessage{}
	for _, n := range run.Nodes {
		if n.ScopeID == scope.ID && n.State == "succeeded" && len(n.Output) > 0 {
			outputs[n.NodeKey] = n.Output
		}
	}
	return graph.EvaluationContext{RunInput: scope.Input, Outputs: outputs, IterationIndex: scope.Iteration}
}
func graphNodeIndexV2(run GraphRunRecord, id string) int {
	for i := range run.Nodes {
		if run.Nodes[i].ID == id {
			return i
		}
	}
	return -1
}
func graphScopeIndexV2(run GraphRunRecord, id string) int {
	for i := range run.Scopes {
		if run.Scopes[i].ID == id {
			return i
		}
	}
	return -1
}
func graphNodeDefinitionV2(run GraphRunRecord, root graph.Definition, n GraphNodeRunRecord) (graph.Node, error) {
	index := graphScopeIndexV2(run, n.ScopeID)
	if index < 0 {
		return graph.Node{}, graphError("scope_invalid", "node scope is missing")
	}
	d, err := scopeDefinitionV2(root, run.Scopes[index].DefinitionPath)
	if err != nil {
		return graph.Node{}, err
	}
	for _, node := range d.Nodes {
		if node.Key == n.NodeKey {
			return node, nil
		}
	}
	return graph.Node{}, graphError("scope_invalid", "node definition is missing")
}
func graphSucceedNodeV2(n *GraphNodeRunRecord) {
	now := time.Now().UTC()
	n.State = "succeeded"
	n.BlockReason = ""
	if n.StartedAt == nil {
		n.StartedAt = &now
	}
	n.FinishedAt = &now
}
func graphBlockNodeV2(run *GraphRunRecord, index int, reason string, err error) {
	n := &run.Nodes[index]
	n.State = "blocked"
	n.BlockReason = reason
	message := n.InstanceKey
	if err != nil {
		message += ": " + err.Error()
	}
	graphEvent(run, "node.blocked", "", message)
}
func graphTerminateV2(run *GraphRunRecord, state, reason string) {
	run.ControlState = "cancel_requested"
	run.TerminationState = state
	run.TerminationReason = reason
	graphEvent(run, "run.stopping", "", reason)
}

func (s *GraphService) reconcileRunV2(ctx context.Context, tx pgx.Tx, run GraphRunRecord, version GraphVersionRecord, before string) (GraphRunRecord, error) {
	if run.SchemaVersion != 2 || len(run.Scopes) == 0 {
		return run, graphError("scope_invalid", "V2 run lacks durable scope metadata")
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('multica.graph_run_id',$1,true)`, run.ID); err != nil {
		return run, err
	}
	var notify []db.AgentTaskQueue
	var stop []string
	var effects []graphHTTPEffectV2
	committed := false
	defer func() {
		if !committed {
			for range effects {
				<-s.effectSlots
			}
		}
	}()
	if run.Deadline != nil && !run.Deadline.After(time.Now()) && run.ControlState != "cancel_requested" {
		graphTerminateV2(&run, "failed", "deadline")
	}
	if run.ControlState != "cancel_requested" {
		if err := graphCheckApprovalsV2(ctx, tx, &run); err != nil {
			return run, err
		}
		for i := range run.Nodes {
			n := &run.Nodes[i]
			if (n.NodeType == "agent" || n.NodeType == "script") && len(n.Attempts) > 0 && (n.State == "active" || n.State == "validating" || n.BlockReason == "execution_unknown") {
				requested, err := s.completeNodeV2(ctx, tx, &run, i)
				if err != nil {
					return run, err
				}
				stop = append(stop, requested...)
			}
		}
	}
	if run.ControlState == "run" && s.AdvanceEnabled {
		for pass := 0; pass < graph.MaxExecutionInstances+1; pass++ {
			progress := false
			if run.ControlState != "run" {
				break
			}
			for i := range run.Nodes {
				n := &run.Nodes[i]
				if n.NodeType == "delay" && n.State == "active" && n.WakeAt != nil && !n.WakeAt.After(time.Now()) {
					graphSucceedNodeV2(n)
					progress = true
				}
				if n.NodeType == "http" && n.Effect != nil && n.Effect.State == "dispatched" && !n.Effect.LeaseUntil.After(time.Now()) {
					definition, e := graphNodeDefinitionV2(run, version.Definition, *n)
					if e != nil {
						return run, e
					}
					if (definition.HTTP.Method == "GET" || definition.HTTP.Method == "HEAD") && n.Effect.Attempts < 3 {
						n.Effect.State = "retryable"
						n.State = "ready"
						n.BlockReason = ""
					} else {
						n.Effect.State = "unknown"
						graphBlockNodeV2(&run, i, "effect_outcome_unknown", nil)
					}
					progress = true
				}
			}
			for si := 0; si < len(run.Scopes); si++ {
				scope := run.Scopes[si]
				if scope.State != "active" {
					continue
				}
				d, err := scopeDefinitionV2(version.Definition, scope.DefinitionPath)
				if err != nil {
					return run, err
				}
				states := map[string]graph.NodeState{}
				ports := map[string]string{}
				indexes := map[string]int{}
				for i, n := range run.Nodes {
					if n.ScopeID != scope.ID {
						continue
					}
					states[n.NodeKey] = graph.NodeState(n.State)
					indexes[n.NodeKey] = i
					if n.SelectedPort != "" {
						ports[n.NodeKey] = n.SelectedPort
					}
				}
				plan, err := graph.PlanReady(d, states, ports)
				if err != nil {
					return run, err
				}
				for _, key := range plan.Skipped {
					n := &run.Nodes[indexes[key]]
					n.State = "skipped"
					now := time.Now().UTC()
					n.FinishedAt = &now
					graphEvent(&run, "node.skipped", "", n.InstanceKey)
					progress = true
				}
				for _, key := range plan.Ready {
					if run.ControlState != "run" {
						break
					}
					index := indexes[key]
					n := run.Nodes[index]
					var definition graph.Node
					for _, candidate := range d.Nodes {
						if candidate.Key == key {
							definition = candidate
							break
						}
					}
					evaluation := graphScopeContextV2(run, scope)
					switch definition.Type {
					case graph.NodeTypeStart, graph.NodeTypeParallel, graph.NodeTypeJoin:
						graphSucceedNodeV2(&run.Nodes[index])
						progress = true
					case graph.NodeTypeEnd:
						output := json.RawMessage(`{}`)
						if len(d.OutputSchema) > 0 {
							output, err = graph.ResolveScopedInputs(d.Outputs, evaluation)
							if err == nil {
								err = graph.ValidateJSON(d.OutputSchema, output)
							}
						}
						if err != nil {
							graphBlockNodeV2(&run, index, "output_invalid", err)
							progress = true
							continue
						}
						if !graphPayloadFitsV2(run, len(output)*2+2048) {
							run.Nodes[index].State = "failed"
							graphTerminateV2(&run, "failed", "storage_budget_exhausted")
							progress = true
							continue
						}
						graphSucceedNodeV2(&run.Nodes[index])
						run.Scopes[si].Output = output
						run.Scopes[si].State = "succeeded"
						progress = true
					case graph.NodeTypeBranch:
						selected, err := graph.EvaluateCondition(*definition.Condition, evaluation)
						if err != nil {
							graphBlockNodeV2(&run, index, "condition_invalid", err)
						} else {
							port := "false"
							if selected {
								port = "true"
							}
							run.Nodes[index].SelectedPort = port
							graphSucceedNodeV2(&run.Nodes[index])
							graphEvent(&run, "branch.selected", "", n.InstanceKey+":"+port)
						}
						progress = true
					case graph.NodeTypeDelay:
						now := time.Now().UTC()
						wake := now.Add(time.Duration(definition.Delay.Seconds) * time.Second)
						run.Nodes[index].StartedAt = &now
						run.Nodes[index].WakeAt = &wake
						run.Nodes[index].State = "active"
						progress = true
					case graph.NodeTypeLoop, graph.NodeTypeSubgraph:
						input, err := graph.ResolveScopedInputs(definition.Inputs, evaluation)
						if err != nil {
							graphBlockNodeV2(&run, index, "input_invalid", err)
							progress = true
							continue
						}
						child := graph.Definition{}
						path := n.DefinitionPath
						iteration := scope.Iteration
						if definition.Loop != nil {
							child = definition.Loop.Body
							path += "/body"
							iteration = 1
						} else {
							child = definition.Subgraph.Definition
							path += "/definition"
						}
						if err = graph.ValidateJSON(child.InputSchema, input); err != nil {
							graphBlockNodeV2(&run, index, "input_invalid", err)
							progress = true
							continue
						}
						now := time.Now().UTC()
						run.Nodes[index].Input = input
						run.Nodes[index].State = "active"
						run.Nodes[index].StartedAt = &now
						if err = graphCreateScopeV2(ctx, tx, &run, child, path, n.ID, iteration, input, true); err != nil {
							if e, ok := err.(*GraphError); ok && e.Code == "storage_budget_exhausted" {
								run.Nodes[index].State = "failed"
								graphTerminateV2(&run, "failed", e.Code)
								progress = true
								continue
							}
							return run, err
						}
						progress = true
					case graph.NodeTypeApproval:
						if err := graphRequestApprovalV2(ctx, tx, &run, index, definition, version); err != nil {
							return run, err
						}
						progress = true
					case graph.NodeTypeAgent, graph.NodeTypeScript:
						if graphActiveExecutionsV2(run) >= graphConcurrencyV2(version.Definition) {
							continue
						}
						if n.NextAttemptAt != nil && n.NextAttemptAt.After(time.Now()) {
							continue
						}
						task, err := s.scheduleNodeV2(ctx, tx, &run, index, definition, scope, version)
						if err != nil {
							return run, err
						}
						if task != nil {
							notify = append(notify, *task)
						}
						progress = true
					case graph.NodeTypeHTTP:
						if graphActiveExecutionsV2(run) >= graphConcurrencyV2(version.Definition) {
							continue
						}
						if n.NextAttemptAt != nil && n.NextAttemptAt.After(time.Now()) {
							continue
						}
						effect, err := s.prepareHTTPV2(&run, index, definition, evaluation)
						if err != nil {
							graphBlockNodeV2(&run, index, "http_input_invalid", err)
							progress = true
						} else if effect != nil {
							effects = append(effects, *effect)
							progress = true
						}
					}
				}
			}
			for si := range run.Scopes {
				scope := run.Scopes[si]
				if scope.State != "succeeded" || scope.ParentNodeRunID == "" {
					continue
				}
				parentIndex := graphNodeIndexV2(run, scope.ParentNodeRunID)
				if parentIndex < 0 {
					return run, graphError("scope_invalid", "parent node missing")
				}
				parent := run.Nodes[parentIndex]
				if parent.State != "active" {
					continue
				}
				// Only the latest child iteration can advance its parent.
				latest := true
				for _, other := range run.Scopes {
					if other.ParentNodeRunID == scope.ParentNodeRunID && other.Iteration > scope.Iteration {
						latest = false
						break
					}
				}
				if !latest {
					continue
				}
				definition, err := graphNodeDefinitionV2(run, version.Definition, parent)
				if err != nil {
					return run, err
				}
				if err = graph.ValidateJSON(definition.OutputSchema, scope.Output); err != nil {
					graphBlockNodeV2(&run, parentIndex, "output_invalid", err)
					progress = true
					continue
				}
				if definition.Loop != nil {
					evaluation := graphScopeContextV2(run, scope)
					again, err := graph.EvaluateCondition(definition.Loop.Condition, evaluation)
					if err != nil {
						graphBlockNodeV2(&run, parentIndex, "condition_invalid", err)
						progress = true
						continue
					}
					if again {
						if scope.Iteration >= definition.Loop.MaxIterations {
							run.Nodes[parentIndex].State = "failed"
							graphTerminateV2(&run, "failed", "iteration_budget_exhausted")
							progress = true
							break
						}
						input, err := graph.ResolveScopedInputs(definition.Loop.Next, evaluation)
						if err != nil {
							graphBlockNodeV2(&run, parentIndex, "input_invalid", err)
							progress = true
							continue
						}
						if err = graph.ValidateJSON(definition.Loop.Body.InputSchema, input); err != nil {
							graphBlockNodeV2(&run, parentIndex, "input_invalid", err)
							progress = true
							continue
						}
						if err = graphCreateScopeV2(ctx, tx, &run, definition.Loop.Body, scope.DefinitionPath, parent.ID, scope.Iteration+1, input, true); err != nil {
							if e, ok := err.(*GraphError); ok && e.Code == "storage_budget_exhausted" {
								run.Nodes[parentIndex].State = "failed"
								graphTerminateV2(&run, "failed", e.Code)
								progress = true
								break
							}
							return run, err
						}
						progress = true
						continue
					}
				}
				run.Nodes[parentIndex].Output = scope.Output
				graphSucceedNodeV2(&run.Nodes[parentIndex])
				progress = true
			}
			if !progress {
				break
			}
		}
	}
	if run.ControlState == "cancel_requested" {
		// Effects prepared earlier in this same pass were never sent. Termination may
		// have been requested after they were prepared (for example by an exhausted
		// loop budget), so cancel them instead of dispatching after the commit.
		for _, effect := range effects {
			if index := graphNodeIndexV2(run, effect.NodeRunID); index >= 0 {
				n := &run.Nodes[index]
				if n.Effect != nil && n.Effect.ClaimToken == effect.ClaimToken && n.Effect.State == "dispatched" {
					now := time.Now().UTC()
					n.Effect.State = "cancelled"
					n.State = "cancelled"
					n.FinishedAt = &now
				}
			}
			<-s.effectSlots
		}
		effects = nil
	}
	if run.ControlState == "cancel_requested" {
		allStopped, requested, err := graphDrainV2(ctx, tx, &run)
		if err != nil {
			return run, err
		}
		stop = append(stop, requested...)
		if allStopped {
			state := run.TerminationState
			if state == "" {
				state = "cancelled"
			}
			graphFinish(&run, state, run.TerminationReason)
		}
	} else {
		graphAggregateStateV2(&run)
	}
	graphCurrentApprovalV2(&run)
	after, err := graphHash(run)
	if err != nil {
		return run, err
	}
	if before != after {
		if err = graphPersistRun(ctx, tx, &run); err != nil {
			return run, err
		}
	} else {
		if _, err = tx.Exec(ctx, `UPDATE graph_run SET updated_at=now() WHERE workspace_id=$1 AND id=$2`, run.WorkspaceID, run.ID); err != nil {
			return run, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE graph_operation SET consumed_at=now(),last_error=NULL WHERE workspace_id=$1 AND run_id=$2 AND consumed_at IS NULL`, run.WorkspaceID, run.ID); err != nil {
		return run, err
	}
	if err = tx.Commit(ctx); err != nil {
		return run, err
	}
	committed = true
	if before != after && s.NotifyRun != nil {
		s.NotifyRun(ctx, run)
	}
	for _, task := range notify {
		if s.NotifyTask != nil {
			s.NotifyTask(ctx, task)
		}
	}
	for _, id := range stop {
		if s.RequestStop != nil {
			_ = s.RequestStop(ctx, id)
		}
	}
	for _, effect := range effects {
		go s.executeHTTPV2(ctx, effect)
	}
	return run, nil
}

func graphConcurrencyV2(d graph.Definition) int {
	if d.MaxConcurrency == 0 {
		return graph.DefaultMaxConcurrency
	}
	return d.MaxConcurrency
}
func graphActiveExecutionsV2(run GraphRunRecord) int {
	count := 0
	for _, n := range run.Nodes {
		if len(n.Attempts) > 0 && n.Attempts[len(n.Attempts)-1].StoppedAt == nil {
			count++
		}
		if n.Effect != nil && n.Effect.State == "dispatched" {
			count++
		}
	}
	return count
}
func graphAggregateStateV2(run *GraphRunRecord) {
	if len(run.Scopes) > 0 && run.Scopes[0].State == "succeeded" {
		run.Output = run.Scopes[0].Output
		graphFinish(run, "succeeded", "")
		return
	}
	waiting, blocked, active := false, "", false
	for _, n := range run.Nodes {
		if n.State == "waiting_approval" {
			waiting = true
		}
		if n.State == "blocked" && blocked == "" {
			blocked = n.BlockReason
		}
		if (n.State == "active" || n.State == "validating") && (n.NodeType == "agent" || n.NodeType == "script" || n.NodeType == "http" || n.NodeType == "delay") {
			active = true
		}
	}
	run.BlockReason = ""
	if active {
		run.State = "active"
	} else if blocked != "" {
		run.State = "blocked"
		run.BlockReason = blocked
	} else if waiting {
		run.State = "waiting_approval"
	} else {
		run.State = "active"
	}
}
func graphDrainV2(ctx context.Context, tx pgx.Tx, run *GraphRunRecord) (bool, []string, error) {
	allStopped := true
	var stop []string
	for i := range run.Nodes {
		n := &run.Nodes[i]
		if n.Effect != nil && n.Effect.State == "dispatched" && n.Effect.LeaseUntil.After(time.Now()) {
			allStopped = false
		}
		for j := range n.Attempts {
			a := &n.Attempts[j]
			if a.StoppedAt != nil {
				continue
			}
			stopped, err := graphStopUnclaimedTx(ctx, tx, a)
			if err != nil {
				return false, nil, err
			}
			if !stopped {
				allStopped = false
				stop = append(stop, a.TaskID)
			}
		}
	}
	return allStopped, stop, nil
}

func (s *GraphService) scheduleNodeV2(ctx context.Context, tx pgx.Tx, run *GraphRunRecord, index int, definition graph.Node, scope GraphScopeRecord, version GraphVersionRecord) (*db.AgentTaskQueue, error) {
	n := &run.Nodes[index]
	original := *n
	if s.AuthorizeRun == nil || s.CreateTask == nil {
		graphBlockNodeV2(run, index, "capability_missing", nil)
		return nil, nil
	}
	if err := s.authorizeVersion(ctx, tx, run.WorkspaceID, run.InitiatorID, version, run.RuntimeBindings); err != nil {
		graphBlockNodeV2(run, index, "authorization_revoked", err)
		return nil, nil
	}
	if len(n.Attempts) >= n.attemptBudget() {
		n.State = "failed"
		graphTerminateV2(run, "failed", "budget_exhausted")
		return nil, nil
	}
	if len(n.Attempts) > 0 && n.Attempts[len(n.Attempts)-1].StoppedAt == nil {
		graphBlockNodeV2(run, index, "execution_unknown", nil)
		return nil, nil
	}
	if n.Spec == nil {
		input, err := graph.ResolveScopedInputs(definition.Inputs, graphScopeContextV2(*run, scope))
		if err != nil {
			graphBlockNodeV2(run, index, "input_invalid", err)
			return nil, nil
		}
		input, hash, err := graph.CanonicalJSON(input)
		if err != nil {
			return nil, err
		}
		snapshot := version.Snapshots[n.DefinitionPath]
		repository, err := graphRepositoryForNodeV2(*run, version.Definition, *n)
		if err != nil {
			graphBlockNodeV2(run, index, "ambiguous_repository_input", err)
			return nil, nil
		}
		n.Input = input
		n.InputHash = hash
		n.Spec = &graph.NodeSpec{ProtocolVersion: graph.ProtocolVersion, Node: definition, Input: input, InputHash: hash, AgentConfig: snapshot.AgentConfig, Skills: snapshot.Skills, Repository: repository, InstanceKey: n.InstanceKey}
	}
	number := len(n.Attempts) + 1
	taskID := uuid.NewString()
	identity := graph.AttemptIdentity{RunID: run.ID, NodeRunID: n.ID, TaskID: taskID, AttemptNo: number, ExecutionGeneration: int64(number), InputHash: n.InputHash}
	manifest, err := graph.NewAttemptManifest(*n.Spec, identity, graph.AttemptGrant{Granted: n.GrantedAttempts, Note: n.HumanNote})
	if err != nil {
		graphBlockNodeV2(run, index, "specification_invalid", err)
		return nil, nil
	}
	raw, hash, err := graph.CanonicalManifest(manifest)
	if err != nil {
		return nil, err
	}
	if !graphPayloadFitsV2(*run, len(raw)+2048) {
		*n = original
		n.State = "failed"
		graphTerminateV2(run, "failed", "storage_budget_exhausted")
		return nil, nil
	}
	if _, err = tx.Exec(ctx, `INSERT INTO graph_task_attempt(task_id,workspace_id,run_id,node_run_id,attempt_no,execution_generation,input_hash,manifest,manifest_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, taskID, run.WorkspaceID, run.ID, n.ID, number, number, n.InputHash, raw, hash); err != nil {
		return nil, err
	}
	previous := ""
	if len(n.Attempts) > 0 {
		previous = n.Attempts[len(n.Attempts)-1].TaskID
	}
	task, err := s.CreateTask(ctx, tx, GraphTaskCreateParams{WorkspaceID: run.WorkspaceID, RunID: run.ID, NodeRunID: n.ID, TaskID: taskID, IssueID: n.IssueID, AgentID: definition.AgentID, RuntimeID: run.RuntimeBindings[n.DefinitionPath], InitiatorID: run.InitiatorID, AttemptNo: number, Generation: int64(number), PreviousTaskID: previous, Manifest: manifest, ManifestJSON: raw, ManifestHash: hash})
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	n.Attempts = append(n.Attempts, GraphAttemptRecord{TaskID: taskID, AttemptNo: number, Generation: int64(number), State: "queued", Manifest: manifest, ManifestHash: hash, CreatedAt: now})
	// The note has been delivered: this attempt carries it, and the next one
	// must not inherit an instruction aimed at this one.
	n.HumanNote = nil
	n.State = "active"
	n.BlockReason = ""
	n.NextAttemptAt = nil
	if n.StartedAt == nil {
		n.StartedAt = &now
	}
	graphEvent(run, "attempt.created", run.InitiatorID, taskID)
	return &task, nil
}
func graphAncestorsV2(d graph.Definition, key string) map[string]bool {
	result := map[string]bool{}
	queue := []string{key}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, e := range d.Edges {
			if e.To == current && !result[e.From] {
				result[e.From] = true
				queue = append(queue, e.From)
			}
		}
	}
	return result
}
func (s *GraphService) completeNodeV2(ctx context.Context, tx pgx.Tx, run *GraphRunRecord, index int) ([]string, error) {
	n := &run.Nodes[index]
	a := &n.Attempts[len(n.Attempts)-1]
	if a.State == "queued" || a.State == "dispatched" || a.State == "running" || a.State == "deferred" || a.State == "waiting_local_directory" {
		var started *time.Time
		var created time.Time
		if err := tx.QueryRow(ctx, `SELECT started_at,created_at FROM agent_task_queue WHERE id=$1`, a.TaskID).Scan(&started, &created); err != nil {
			return nil, err
		}
		if started != nil && n.Spec != nil && time.Since(*started) > time.Duration(n.Spec.Node.TimeoutSeconds)*time.Second {
			graphBlockNodeV2(run, index, "execution_unknown", nil)
			return []string{a.TaskID}, nil
		}
		if started == nil && time.Since(created) > 30*time.Minute {
			graphBlockNodeV2(run, index, "queue_timeout", nil)
			stopped, err := graphStopUnclaimedTx(ctx, tx, a)
			if err != nil {
				return nil, err
			}
			if !stopped {
				return []string{a.TaskID}, nil
			}
		}
		return nil, nil
	}
	var sub *GraphSubmissionRecord
	if a.Receipt != nil {
		for j := range n.Submissions {
			if n.Submissions[j].ID == a.Receipt.SubmissionID {
				sub = &n.Submissions[j]
				break
			}
		}
	}
	if sub == nil && a.Receipt != nil && a.Receipt.SubmissionID != "" {
		var stored GraphSubmissionRecord
		if err := graphScan(tx.QueryRow(ctx, `SELECT document FROM graph_submission WHERE workspace_id=$1 AND run_id=$2 AND node_run_id=$3 AND task_id=$4 AND id=$5`, run.WorkspaceID, run.ID, n.ID, a.TaskID, a.Receipt.SubmissionID), &stored); err == nil {
			sub = &stored
		}
	}
	var result *graph.Result
	valid, artifacts := false, false
	delivery := ""
	retryable := false
	if sub != nil && n.Spec != nil {
		result = &sub.Result
		valid = graph.ValidateResult(n.Spec.Node, *result) == nil
		artifacts = true
		if result.Outcome == graph.OutcomePass && n.Spec.Node.ExecutionMode == graph.ExecutionVerifyCommit {
			artifacts = n.Spec.Repository != nil && result.TestedCommitSHA == n.Spec.Repository.CommitSHA
		}
		if len(result.Artifacts) > 0 {
			artifacts = artifacts && s.ValidateArtifacts != nil && a.Receipt != nil && s.ValidateArtifacts(ctx, tx, *run, *n, *result, *a.Receipt) == nil
		}
	}
	if a.Receipt != nil {
		delivery = a.Receipt.DeliveryStatus
		retryable = a.Receipt.Retryable
	}
	decision := graph.DecideCompletion(graph.Completion{RunState: graph.RunActive, ControlState: graph.ControlState(run.ControlState), Expected: a.Manifest.Identity, Result: result, TaskState: a.State, ExecutionStopped: a.StoppedAt != nil, DeliveryStatus: delivery, OutputValid: valid, ArtifactsValid: artifacts, Retryable: retryable, Attempts: len(n.Attempts), MaxAttempts: n.MaxAttempts, Granted: n.GrantedAttempts})
	n.State = string(decision.NodeState)
	n.BlockReason = decision.Reason
	if decision.Retry && a.Receipt != nil && a.StoppedAt != nil {
		if delay := retryDelayForAttempt(a.Receipt.FailureReason, int32(a.AttemptNo)); delay > 0 {
			at := a.StoppedAt.Add(delay)
			n.NextAttemptAt = &at
		}
	}
	if n.State == "succeeded" && sub != nil {
		n.AcceptedSubmissionID = sub.ID
		n.Output = sub.Result.Output
		n.Submissions = []GraphSubmissionRecord{*sub}
		graphSucceedNodeV2(n)
		if err := graphRetainArtifacts(ctx, tx, run, *n, sub.Result); err != nil {
			return nil, err
		}
		graphEvent(run, "node.accepted", a.TaskID, n.InstanceKey)
	}
	if decision.RunState == graph.RunFailed || decision.RunState == graph.RunRejected {
		now := time.Now().UTC()
		n.FinishedAt = &now
		graphTerminateV2(run, string(decision.RunState), decision.Reason)
	}
	return nil, nil
}
