package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/graph"
	"github.com/multica-ai/multica/server/internal/util"
)

func TestGraphV2PostgresScriptUsesExistingRuntimeTaskAndReceipt(t *testing.T) {
	f := newGraphFixture(t)
	node := f.version.Definition.Nodes[1]
	node.Type = graph.NodeTypeScript
	node.Instructions = ""
	node.TimeoutSeconds = 30
	node.ExecutionMode = graph.ExecutionNone
	node.Script = &graph.ScriptConfig{Language: "python", Code: `print('{"summary":"verified"}')`, TimeoutSeconds: 30}
	d := graph.Definition{SchemaVersion: 2, Name: "Script runtime", InputSchema: f.version.Definition.InputSchema, Nodes: []graph.Node{{Key: "start", Type: graph.NodeTypeStart}, node, {Key: "end", Type: graph.NodeTypeEnd}}, Edges: []graph.Edge{{From: "start", To: node.Key}, {From: node.Key, To: "end"}}}
	f = graphV2Publish(t, f, d)
	run := graphV2StartAny(t, f)
	index := graphV2NodeIndex(t, run, node.Key)
	if run.Nodes[index].Spec == nil || run.Nodes[index].Spec.Node.Script == nil {
		t.Fatal("script lost its manifest configuration")
	}
	var runtime string
	if err := f.pool.QueryRow(context.Background(), `SELECT runtime_id::text FROM agent_task_queue WHERE id=$1`, run.Nodes[index].Attempts[0].TaskID).Scan(&runtime); err != nil {
		t.Fatal(err)
	}
	if runtime != f.runtime {
		t.Fatal("script bypassed selected existing runtime")
	}
	f.complete(t, run, index)
	run = f.reconcile(t, run)
	if run.State != "succeeded" || run.Nodes[index].AcceptedSubmissionID == "" {
		t.Fatalf("script failed normal result and stop-receipt acceptance: %s", run.State)
	}
}

func TestGraphV2PostgresHTTPReplacedLeaseIgnoresLateCompletion(t *testing.T) {
	f := newGraphFixture(t)
	d := graph.Definition{SchemaVersion: 2, Name: "HTTP lease", InputSchema: f.version.Definition.InputSchema, Nodes: []graph.Node{{Key: "start", Type: graph.NodeTypeStart}, {Key: "request", Type: graph.NodeTypeHTTP, HTTP: &graph.HTTPConfig{Method: "GET", URL: "https://example.org", TimeoutSeconds: 30, MaxResponseBytes: 1024}, OutputSchema: json.RawMessage(`{"type":"object","properties":{"status_code":{"type":"integer"},"body":{"type":"string"}},"required":["status_code","body"]}`)}, {Key: "end", Type: graph.NodeTypeEnd}}, Edges: []graph.Edge{{From: "start", To: "request"}, {From: "request", To: "end"}}}
	f = graphV2Publish(t, f, d)
	first, second := make(chan struct{}), make(chan struct{})
	var firstOnce, secondOnce sync.Once
	defer firstOnce.Do(func() { close(first) })
	defer secondOnce.Do(func() { close(second) })
	started := make(chan string, 2)
	var calls atomic.Int32
	f.svc.ExecuteHTTP = func(ctx context.Context, _ graph.HTTPConfig, _ map[string]string, _ json.RawMessage, key string) (json.RawMessage, error) {
		n := calls.Add(1)
		started <- key
		if n == 1 {
			select {
			case <-first:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return json.RawMessage(`{"status_code":202,"body":"stale"}`), nil
		}
		select {
		case <-second:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return json.RawMessage(`{"status_code":200,"body":"current"}`), nil
	}
	waitStarted := func() string {
		t.Helper()
		select {
		case id := <-started:
			return id
		case <-time.After(3 * time.Second):
			t.Fatal("HTTP callback not started")
			return ""
		}
	}
	run := graphV2StartAny(t, f)
	index := graphV2NodeIndex(t, run, "request")
	id := waitStarted()
	firstClaim := run.Nodes[index].Effect.ClaimToken
	run.Nodes[index].Effect.LeaseUntil = time.Now().Add(-time.Second)
	tx, err := f.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = graphPersistRun(context.Background(), tx, &run); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	run = f.reconcile(t, run)
	if waitStarted() != id || run.Nodes[index].Effect.ClaimToken == firstClaim {
		t.Fatal("retry changed invocation identity or reused claim")
	}
	waitSlots := func(expected int) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for len(f.svc.effectSlots) != expected && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if len(f.svc.effectSlots) != expected {
			t.Fatal("callback did not persist completion")
		}
	}
	firstOnce.Do(func() { close(first) })
	waitSlots(1)
	run, err = f.svc.GetRun(context.Background(), f.ws, f.user, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Nodes[index].State != "active" || len(run.Nodes[index].Output) > 0 {
		t.Fatal("stale HTTP callback overwrote replaced claim")
	}
	secondOnce.Do(func() { close(second) })
	waitSlots(0)
	run = f.reconcile(t, run)
	if run.State != "succeeded" || !strings.Contains(string(run.Nodes[index].Output), `"current"`) {
		t.Fatalf("current HTTP claim failed: %s %s", run.State, run.Nodes[index].Output)
	}
}

func TestGraphV2PostgresApprovalReplacementRespectsStorageBudget(t *testing.T) {
	f := newGraphFixture(t)
	replacement := uuid.NewString()
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO member(id,workspace_id,user_id,role) VALUES($1,$2,$3,'member')`, replacement, f.ws, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	d := graph.Definition{SchemaVersion: 2, Name: "Approval budget", InputSchema: f.version.Definition.InputSchema, Nodes: []graph.Node{{Key: "start", Type: graph.NodeTypeStart}, {Key: "gate", Type: graph.NodeTypeApproval, ApproverID: f.member}, {Key: "end", Type: graph.NodeTypeEnd}}, Edges: []graph.Edge{{From: "start", To: "gate"}, {From: "gate", To: "end"}}}
	f = graphV2Publish(t, f, d)
	run := graphV2StartAny(t, f)
	run.Approvals[0].Candidate = json.RawMessage(`{"evidence":"` + strings.Repeat("x", 128<<10) + `"}`)
	graphCurrentApprovalV2(&run)
	raw, _ := graphJSON(run)
	run.RootSnapshot = json.RawMessage(`{"padding":"` + strings.Repeat("p", graphRunPayloadLimit-len(raw)-32768) + `"}`)
	tx, err := f.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = graphPersistRun(context.Background(), tx, &run); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.Control(context.Background(), f.ws, f.user, run.ID, "approver", GraphControlParams{ExpectedRevision: run.Revision, IdempotencyKey: uuid.NewString(), NodeRunID: run.Approval.NodeRunID, ApproverID: replacement})
	var ge *GraphError
	if !errors.As(err, &ge) || ge.Code != "storage_budget_exhausted" {
		t.Fatalf("approval replacement exceeded document budget: %v", err)
	}
	current, err := f.svc.GetRun(context.Background(), f.ws, f.user, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Approval.ID != run.Approval.ID || current.Approval.State != "pending" {
		t.Fatal("rejected replacement mutated existing gate")
	}
}

func TestGraphV2PostgresCompactedSubmissionsRetainAttemptBudget(t *testing.T) {
	f := graphV2ParallelFixture(t)
	run := graphV2Start(t, f)
	index := graphV2NodeIndex(t, run, "left")
	a := run.Nodes[index].Attempts[0]
	id := util.MustParseUUID(a.TaskID)
	if _, err := f.pool.Exec(context.Background(), `UPDATE agent_task_queue SET status='dispatched',dispatched_at=now() WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tasks.StartTask(WithGraphExecutor(context.Background(), "test-executor"), id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO graph_submission(id,workspace_id,run_id,node_run_id,task_id,idempotency_key,content_hash,document) SELECT gen_random_uuid(),$1,$2,$3,$4,i::text,'hash','{}'::jsonb FROM generate_series(1,100) i`, f.ws, run.ID, run.Nodes[index].ID, a.TaskID); err != nil {
		t.Fatal(err)
	}
	_, err := f.svc.Submit(context.Background(), f.ws, a.TaskID, uuid.NewString(), graph.Result{Identity: a.Manifest.Identity, Outcome: graph.OutcomePass, Output: json.RawMessage(`{"summary":"ok"}`)})
	var ge *GraphError
	if !errors.As(err, &ge) || ge.Code != "submission_limit" {
		t.Fatalf("compaction bypassed persisted submission limit: %v", err)
	}
}

func TestGraphV2BlockedUnknownExecutionRemainsVisible(t *testing.T) {
	run := GraphRunRecord{Nodes: []GraphNodeRunRecord{{State: "blocked", BlockReason: "execution_unknown", Attempts: []GraphAttemptRecord{{State: "running"}}}}}
	graphAggregateStateV2(&run)
	if run.State != "blocked" || run.BlockReason != "execution_unknown" {
		t.Fatalf("unresolved physical execution hidden as %s: %s", run.State, run.BlockReason)
	}
	if graphActiveExecutionsV2(run) != 1 {
		t.Fatal("unknown execution must still reserve its concurrency slot")
	}
}

func TestGraphV2PostgresCommittedHTTPHasIndependentBoundedContext(t *testing.T) {
	f := newGraphFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.svc.effectOnce.Do(func() { f.svc.effectSlots = make(chan struct{}, 32) })
	f.svc.effectSlots <- struct{}{}
	f.svc.ExecuteHTTP = func(ctx context.Context, _ graph.HTTPConfig, _ map[string]string, _ json.RawMessage, _ string) (json.RawMessage, error) {
		if ctx.Err() != nil {
			t.Errorf("committed effect inherited expired request: %v", ctx.Err())
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 7*time.Second {
			t.Error("effect lacks independent bounded deadline")
		}
		return json.RawMessage(`{}`), nil
	}
	f.svc.executeHTTPV2(ctx, graphHTTPEffectV2{WorkspaceID: f.ws, RunID: uuid.NewString(), Config: graph.HTTPConfig{TimeoutSeconds: 2}})
}

func TestGraphV2PostgresParallelRejectionDrainsRunningSibling(t *testing.T) {
	f := graphV2ParallelFixture(t)
	run := graphV2Start(t, f)
	left, right := graphV2NodeIndex(t, run, "left"), graphV2NodeIndex(t, run, "right")
	for _, index := range []int{left, right} {
		id := util.MustParseUUID(run.Nodes[index].Attempts[0].TaskID)
		if _, err := f.pool.Exec(context.Background(), `UPDATE agent_task_queue SET status='dispatched',dispatched_at=now() WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := f.tasks.StartTask(WithGraphExecutor(context.Background(), "parallel-executor"), id); err != nil {
			t.Fatal(err)
		}
	}
	a := run.Nodes[left].Attempts[0]
	sub, err := f.svc.Submit(context.Background(), f.ws, a.TaskID, uuid.NewString(), graph.Result{Identity: a.Manifest.Identity, Outcome: graph.OutcomeNeedsChanges, Diagnostics: json.RawMessage(`{"reason":"acceptance failed"}`)})
	if err != nil {
		t.Fatal(err)
	}
	receipt := GraphTaskReceipt{Identity: a.Manifest.Identity, SubmissionID: sub.ID, ProviderExit: "success", DeliveryStatus: "not_required", ExecutorID: "parallel-executor", StoppedAt: time.Now().UTC()}
	if _, err = f.tasks.CompleteTask(WithGraphReceipt(context.Background(), receipt), util.MustParseUUID(a.TaskID), []byte(`{}`), "", "", "", false, ""); err != nil {
		t.Fatal(err)
	}
	stop := ""
	f.svc.RequestStop = func(_ context.Context, id string) error { stop = id; return nil }
	run = f.reconcile(t, run)
	if graphTerminal(run.State) || run.ControlState != "cancel_requested" || stop != run.Nodes[right].Attempts[0].TaskID {
		t.Fatalf("parallel failure did not durably drain sibling: %s %s stop=%s", run.State, run.ControlState, stop)
	}
	a = run.Nodes[right].Attempts[0]
	receipt = GraphTaskReceipt{Identity: a.Manifest.Identity, ProviderExit: "cancelled", DeliveryStatus: "not_required", ExecutorID: "parallel-executor", StoppedAt: time.Now().UTC()}
	if _, err = f.tasks.CompleteTask(WithGraphReceipt(context.Background(), receipt), util.MustParseUUID(a.TaskID), []byte(`{}`), "", "", "", false, ""); err != nil {
		t.Fatal(err)
	}
	run = f.reconcile(t, run)
	if run.State != "rejected" || run.Nodes[right].Attempts[0].StoppedAt == nil {
		t.Fatalf("drained rejection lost terminal outcome: %s", run.State)
	}
	for _, scope := range run.Scopes {
		if scope.State == "active" {
			t.Fatal("terminal run retained an active execution scope")
		}
	}
}

func TestGraphV2PostgresLoopLimitAndSubgraphOutputs(t *testing.T) {
	for _, container := range []string{"loop", "subgraph"} {
		t.Run(container, func(t *testing.T) {
			f := newGraphFixture(t)
			agent := f.version.Definition.Nodes[1]
			body := graph.Definition{SchemaVersion: 2, Name: "Body", InputSchema: f.version.Definition.InputSchema, OutputSchema: agent.OutputSchema, Outputs: map[string]graph.InputBinding{"summary": {Source: graph.BindingNodeOutput, NodeKey: agent.Key, Path: "/summary"}}, Nodes: []graph.Node{{Key: "start", Type: graph.NodeTypeStart}, agent, {Key: "end", Type: graph.NodeTypeEnd}}, Edges: []graph.Edge{{From: "start", To: agent.Key}, {From: agent.Key, To: "end"}}}
			node := graph.Node{Key: "container", Type: graph.NodeTypeSubgraph, Inputs: map[string]graph.InputBinding{"goal": {Source: graph.BindingRunInput, Path: "/goal"}}, OutputSchema: agent.OutputSchema, Subgraph: &graph.SubgraphConfig{Definition: body}}
			if container == "loop" {
				node.Type = graph.NodeTypeLoop
				node.Subgraph = nil
				node.Loop = &graph.LoopConfig{Body: body, MaxIterations: 1, Condition: graph.Condition{Op: "eq", Left: &graph.InputBinding{Source: graph.BindingConstant, Value: json.RawMessage(`true`)}, Right: &graph.InputBinding{Source: graph.BindingConstant, Value: json.RawMessage(`true`)}}, Next: map[string]graph.InputBinding{"goal": {Source: graph.BindingRunInput, Path: "/goal"}}}
			}
			d := graph.Definition{SchemaVersion: 2, Name: "Container", InputSchema: body.InputSchema, OutputSchema: agent.OutputSchema, Outputs: map[string]graph.InputBinding{"summary": {Source: graph.BindingNodeOutput, NodeKey: "container", Path: "/summary"}}, Nodes: []graph.Node{{Key: "start", Type: graph.NodeTypeStart}, node, {Key: "end", Type: graph.NodeTypeEnd}}, Edges: []graph.Edge{{From: "start", To: "container"}, {From: "container", To: "end"}}}
			f = graphV2Publish(t, f, d)
			run := graphV2StartAny(t, f)
			f.complete(t, run, graphV2NodeIndex(t, run, agent.Key))
			run = f.reconcile(t, run)
			if container == "loop" {
				if run.State != "failed" || run.TerminationReason != "iteration_budget_exhausted" || len(run.Scopes) != 2 {
					t.Fatalf("loop budget silently succeeded or exceeded bound: %s %s scopes=%d", run.State, run.TerminationReason, len(run.Scopes))
				}
			} else if run.State != "succeeded" || string(run.Output) != `{"summary":"verified"}` {
				t.Fatalf("subgraph output not exported: %s %s", run.State, run.Output)
			}
		})
	}
}

func TestGraphV2PostgresQuorumRequiresTwoOfThreeVotes(t *testing.T) {
	f := newGraphFixture(t)
	members, users := []string{f.member}, []string{f.user}
	for range 2 {
		member, user := uuid.NewString(), uuid.NewString()
		members = append(members, member)
		users = append(users, user)
		if _, err := f.pool.Exec(context.Background(), `INSERT INTO member(id,workspace_id,user_id,role) VALUES($1,$2,$3,'member')`, member, f.ws, user); err != nil {
			t.Fatal(err)
		}
	}
	d := graph.Definition{SchemaVersion: 2, Name: "Quorum", InputSchema: f.version.Definition.InputSchema, Nodes: []graph.Node{{Key: "start", Type: graph.NodeTypeStart}, {Key: "gate", Type: graph.NodeTypeApproval, Approval: &graph.ApprovalConfig{ApproverIDs: members, Mode: "quorum", Quorum: 2}}, {Key: "end", Type: graph.NodeTypeEnd}}, Edges: []graph.Edge{{From: "start", To: "gate"}, {From: "gate", To: "end"}}}
	f = graphV2Publish(t, f, d)
	run := graphV2StartAny(t, f)
	for i, decision := range []string{"rejected", "approved", "approved"} {
		var err error
		run, err = f.svc.Approve(context.Background(), f.ws, users[i], run.ID, GraphApprovalParams{ExpectedRevision: run.Revision, IdempotencyKey: uuid.NewString(), ApprovalID: run.Approval.ID, CandidateDigest: run.Approval.CandidateDigest, Decision: decision})
		if err != nil {
			t.Fatal(err)
		}
		run = f.reconcile(t, run)
		if i < 2 && run.State != "waiting_approval" {
			t.Fatalf("quorum ended early after %d votes: %s", i+1, run.State)
		}
	}
	if run.State != "succeeded" {
		t.Fatalf("quorum did not accept two approvals: %s", run.State)
	}
}
