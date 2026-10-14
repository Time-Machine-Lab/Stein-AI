package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/graph"
	"github.com/multica-ai/multica/server/internal/util"
)

func graphV2ParallelFixture(t *testing.T) graphFixture {
	t.Helper()
	f := newGraphFixture(t)
	a := f.version.Definition.Nodes[1]
	a.Key = "left"
	b := a
	b.Key = "right"
	d := graph.Definition{SchemaVersion: 2, Name: "Parallel acceptance", InputSchema: f.version.Definition.InputSchema,
		Nodes: []graph.Node{{Key: "start", Type: "start"}, {Key: "fork", Type: "parallel"}, a, b, {Key: "join", Type: "join"}, {Key: "approval", Type: "approval", ApproverID: f.member}, {Key: "end", Type: "end"}},
		Edges: []graph.Edge{{From: "start", To: "fork"}, {From: "fork", To: "left"}, {From: "fork", To: "right"}, {From: "left", To: "join"}, {From: "right", To: "join"}, {From: "join", To: "approval"}, {From: "approval", To: "end"}}}
	draft, err := f.svc.CreateDefinition(context.Background(), f.ws, f.user, d, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	f.version, err = f.svc.Publish(context.Background(), f.ws, f.user, draft.ID, draft.Revision, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func graphV2Publish(t *testing.T, f graphFixture, d graph.Definition) graphFixture {
	t.Helper()
	draft, err := f.svc.CreateDefinition(context.Background(), f.ws, f.user, d, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	f.version, err = f.svc.Publish(context.Background(), f.ws, f.user, draft.ID, draft.Revision, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func graphV2StartAny(t *testing.T, f graphFixture) GraphRunRecord {
	t.Helper()
	bindings := map[string]string{}
	if err := graph.WalkNodes(f.version.Definition, func(path string, n graph.Node) error {
		if n.Type == graph.NodeTypeAgent || n.Type == graph.NodeTypeScript {
			bindings[path] = f.runtime
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	run, err := f.svc.StartRun(context.Background(), f.ws, f.user, GraphStartParams{VersionID: f.version.ID, RootIssueID: f.issue, Input: json.RawMessage(`{"goal":"test"}`), RuntimeBindings: bindings, IdempotencyKey: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	return f.reconcile(t, run)
}

func TestGraphV2PostgresLoopsHaveDistinctFrozenInstances(t *testing.T) {
	f := newGraphFixture(t)
	agent := f.version.Definition.Nodes[1]
	schema := agent.OutputSchema
	body := graph.Definition{SchemaVersion: 2, Name: "Review iteration", InputSchema: f.version.Definition.InputSchema, OutputSchema: schema, Outputs: map[string]graph.InputBinding{"summary": {Source: graph.BindingNodeOutput, NodeKey: "implement", Path: "/summary"}}, Nodes: []graph.Node{{Key: "start", Type: graph.NodeTypeStart}, agent, {Key: "end", Type: graph.NodeTypeEnd}}, Edges: []graph.Edge{{From: "start", To: "implement"}, {From: "implement", To: "end"}}}
	d := graph.Definition{SchemaVersion: 2, Name: "Bounded review", InputSchema: body.InputSchema, Nodes: []graph.Node{{Key: "start", Type: graph.NodeTypeStart}, {Key: "loop", Type: graph.NodeTypeLoop, Inputs: map[string]graph.InputBinding{"goal": {Source: graph.BindingRunInput, Path: "/goal"}}, OutputSchema: schema, Loop: &graph.LoopConfig{Body: body, MaxIterations: 3, Condition: graph.Condition{Op: "lt", Left: &graph.InputBinding{Source: graph.BindingIterationIndex}, Right: &graph.InputBinding{Source: graph.BindingConstant, Value: json.RawMessage(`2`)}}, Next: map[string]graph.InputBinding{"goal": {Source: graph.BindingNodeOutput, NodeKey: "implement", Path: "/summary"}}}}, {Key: "end", Type: graph.NodeTypeEnd}}, Edges: []graph.Edge{{From: "start", To: "loop"}, {From: "loop", To: "end"}}}
	f = graphV2Publish(t, f, d)
	run := graphV2StartAny(t, f)
	first := graphV2NodeIndex(t, run, "implement")
	if run.Nodes[first].Iteration != 1 {
		t.Fatal("first iteration not isolated")
	}
	firstID := run.Nodes[first].ID
	f.complete(t, run, first)
	run = f.reconcile(t, run)
	second := -1
	for i, n := range run.Nodes {
		if n.NodeKey == "implement" && n.Iteration == 2 {
			second = i
		}
	}
	if second < 0 || run.Nodes[second].ID == firstID || run.Nodes[second].InstanceKey == run.Nodes[first].InstanceKey {
		t.Fatal("second iteration reused execution identity")
	}
	if string(run.Nodes[second].Input) != `{"goal":"verified"}` {
		t.Fatalf("next iteration input not frozen from previous output: %s", run.Nodes[second].Input)
	}
	if run.Nodes[second].Spec.InstanceKey == run.Nodes[first].Spec.InstanceKey {
		t.Fatal("task manifests collide between loop iterations")
	}
	f.complete(t, run, second)
	run = f.reconcile(t, run)
	if run.State != "succeeded" || len(run.Scopes) != 3 {
		t.Fatalf("loop failed to stop: %s scopes=%d", run.State, len(run.Scopes))
	}
}

func TestGraphV2PostgresBranchSkipsUnselectedTasks(t *testing.T) {
	f := graphV2ParallelFixture(t)
	d := f.version.Definition
	for i := range d.Nodes {
		if d.Nodes[i].Key == "fork" {
			d.Nodes[i].Type = graph.NodeTypeBranch
			d.Nodes[i].Condition = &graph.Condition{Op: "eq", Left: &graph.InputBinding{Source: graph.BindingConstant, Value: json.RawMessage(`1`)}, Right: &graph.InputBinding{Source: graph.BindingConstant, Value: json.RawMessage(`1`)}}
		}
	}
	for i := range d.Edges {
		if d.Edges[i].From == "fork" {
			d.Edges[i].Port = "true"
			if d.Edges[i].To == "right" {
				d.Edges[i].Port = "false"
			}
		}
	}
	f = graphV2Publish(t, f, d)
	run := graphV2StartAny(t, f)
	left, right := graphV2NodeIndex(t, run, "left"), graphV2NodeIndex(t, run, "right")
	if len(run.Nodes[left].Attempts) != 1 || run.Nodes[right].State != "skipped" || len(run.Nodes[right].Attempts) != 0 {
		t.Fatalf("wrong selected path: %+v", run.Nodes)
	}
	var skippedIssueStatus string
	if err := f.pool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id=$1`, run.Nodes[right].IssueID).Scan(&skippedIssueStatus); err != nil {
		t.Fatal(err)
	}
	if skippedIssueStatus != "cancelled" {
		t.Fatalf("skipped branch left an actionable task: %s", skippedIssueStatus)
	}
	f.complete(t, run, left)
	run = f.reconcile(t, run)
	if run.State != "waiting_approval" {
		t.Fatalf("join hung on skipped branch: %s", run.State)
	}
}

func TestGraphV2PostgresConcurrentCoordinatorsDoNotDuplicateTasks(t *testing.T) {
	f := graphV2ParallelFixture(t)
	run := graphV2Start(t, f)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := f.svc.ReconcileRun(context.Background(), f.ws, run.ID); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	run = f.reconcile(t, run)
	for _, key := range []string{"left", "right"} {
		if len(run.Nodes[graphV2NodeIndex(t, run, key)].Attempts) != 1 {
			t.Fatal("concurrent reconciliation duplicated attempt")
		}
	}
}

func TestGraphV2PostgresMultipleApprovalGatesAndQuorum(t *testing.T) {
	f := newGraphFixture(t)
	secondMember, secondUser := uuid.NewString(), uuid.NewString()
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO member(id,workspace_id,user_id,role) VALUES($1,$2,$3,'member')`, secondMember, f.ws, secondUser); err != nil {
		t.Fatal(err)
	}
	agent := f.version.Definition.Nodes[1]
	d := graph.Definition{SchemaVersion: 2, Name: "Two gates", InputSchema: f.version.Definition.InputSchema, Nodes: []graph.Node{{Key: "start", Type: graph.NodeTypeStart}, {Key: "fork", Type: graph.NodeTypeParallel}, {Key: "all", Type: graph.NodeTypeApproval, Approval: &graph.ApprovalConfig{ApproverIDs: []string{f.member, secondMember}, Mode: "all"}}, {Key: "any", Type: graph.NodeTypeApproval, Approval: &graph.ApprovalConfig{ApproverIDs: []string{f.member, secondMember}, Mode: "any"}}, {Key: "join", Type: graph.NodeTypeJoin}, agent, {Key: "end", Type: graph.NodeTypeEnd}}, Edges: []graph.Edge{{From: "start", To: "fork"}, {From: "fork", To: "all"}, {From: "fork", To: "any"}, {From: "all", To: "join"}, {From: "any", To: "join"}, {From: "join", To: "implement"}, {From: "implement", To: "end"}}}
	f = graphV2Publish(t, f, d)
	run := graphV2StartAny(t, f)
	if len(run.Approvals) != 2 {
		t.Fatalf("expected two independently durable approval gates: %d", len(run.Approvals))
	}
	vote := func(user, node string, decision string) {
		t.Helper()
		nodeID := run.Nodes[graphV2NodeIndex(t, run, node)].ID
		var a GraphApprovalRecord
		for _, candidate := range run.Approvals {
			if candidate.NodeRunID == nodeID {
				a = candidate
			}
		}
		var err error
		run, err = f.svc.Approve(context.Background(), f.ws, user, run.ID, GraphApprovalParams{ExpectedRevision: run.Revision, IdempotencyKey: uuid.NewString(), ApprovalID: a.ID, CandidateDigest: a.CandidateDigest, Decision: decision})
		if err != nil {
			t.Fatal(err)
		}
		run = f.reconcile(t, run)
	}
	vote(f.user, "any", "rejected")
	if run.State == "rejected" {
		t.Fatal("any-mode first rejection incorrectly ended gate")
	}
	vote(secondUser, "any", "approved")
	vote(f.user, "all", "approved")
	if len(run.Nodes[graphV2NodeIndex(t, run, "implement")].Attempts) != 0 {
		t.Fatal("all-mode gate ignored second approver")
	}
	vote(secondUser, "all", "approved")
	index := graphV2NodeIndex(t, run, "implement")
	if len(run.Nodes[index].Attempts) != 1 {
		t.Fatal("approved intermediate gate failed to schedule successor")
	}
	f.complete(t, run, index)
	run = f.reconcile(t, run)
	if run.State != "succeeded" {
		t.Fatal("multiapproval workflow did not complete")
	}
}

func TestGraphV2PostgresDurableDelayDoesNotRestartClock(t *testing.T) {
	f := newGraphFixture(t)
	d := graph.Definition{SchemaVersion: 2, Name: "Delay", InputSchema: f.version.Definition.InputSchema, Nodes: []graph.Node{{Key: "start", Type: graph.NodeTypeStart}, {Key: "wait", Type: graph.NodeTypeDelay, Delay: &graph.DelayConfig{Seconds: 60}}, {Key: "end", Type: graph.NodeTypeEnd}}, Edges: []graph.Edge{{From: "start", To: "wait"}, {From: "wait", To: "end"}}}
	f = graphV2Publish(t, f, d)
	run := graphV2StartAny(t, f)
	index := graphV2NodeIndex(t, run, "wait")
	wake := *run.Nodes[index].WakeAt
	run = f.reconcile(t, run)
	if !run.Nodes[index].WakeAt.Equal(wake) {
		t.Fatal("reconcile reset durable timer")
	}
	tx, err := f.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Second)
	run.Nodes[index].WakeAt = &past
	if err = graphPersistRun(context.Background(), tx, &run); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	run = f.reconcile(t, run)
	if run.State != "succeeded" {
		t.Fatal("expired delay did not resume")
	}
}

func graphV2HTTPFixture(t *testing.T, method string) graphFixture {
	t.Helper()
	f := newGraphFixture(t)
	d := graph.Definition{SchemaVersion: 2, Name: "HTTP", InputSchema: f.version.Definition.InputSchema, Nodes: []graph.Node{{Key: "start", Type: graph.NodeTypeStart}, {Key: "request", Type: graph.NodeTypeHTTP, OutputSchema: json.RawMessage(`{"type":"object","properties":{"status_code":{"type":"integer"},"body":{"type":"object"}},"required":["status_code","body"]}`), HTTP: &graph.HTTPConfig{Method: method, URL: "https://example.com/work", TimeoutSeconds: 1, MaxResponseBytes: 4096}}, {Key: "end", Type: graph.NodeTypeEnd}}, Edges: []graph.Edge{{From: "start", To: "request"}, {From: "request", To: "end"}}}
	return graphV2Publish(t, f, d)
}

func TestGraphV2PostgresHTTPDispatchCommitsBeforeEffectAndAmbiguousFailureRequiresReason(t *testing.T) {
	f := graphV2HTTPFixture(t, "POST")
	started := make(chan string, 1)
	release := make(chan struct{})
	defer close(release)
	f.svc.ExecuteHTTP = func(_ context.Context, _ graph.HTTPConfig, _ map[string]string, _ json.RawMessage, key string) (json.RawMessage, error) {
		var state string
		err := f.pool.QueryRow(context.Background(), `SELECT document->'effect'->>'state' FROM graph_node_run WHERE id=$1`, key).Scan(&state)
		if err != nil || state != "dispatched" {
			started <- "effect was invoked before durable dispatch commit"
		} else {
			started <- key
		}
		<-release
		return nil, errors.New("connection dropped after remote accepted request")
	}
	run := graphV2StartAny(t, f)
	index := graphV2NodeIndex(t, run, "request")
	select {
	case key := <-started:
		if key != run.Nodes[index].ID {
			t.Fatal(key)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP effect never dispatched")
	}
	// Simulate a coordinator crash after dispatch; replay must not duplicate POST.
	tx, err := f.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	run.Nodes[index].Effect.LeaseUntil = time.Now().Add(-time.Second)
	if err = graphPersistRun(context.Background(), tx, &run); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	run = f.reconcile(t, run)
	if run.State != "blocked" || run.Nodes[index].BlockReason != "effect_outcome_unknown" {
		t.Fatalf("ambiguous POST not blocked: %s %s", run.State, run.Nodes[index].BlockReason)
	}
	if _, err = f.svc.Control(context.Background(), f.ws, f.user, run.ID, "retry", GraphControlParams{ExpectedRevision: run.Revision, NodeRunID: run.Nodes[index].ID, IdempotencyKey: uuid.NewString()}); err == nil {
		t.Fatal("ambiguous HTTP retry accepted without explicit reason")
	}
	_, err = f.svc.Control(context.Background(), f.ws, f.user, run.ID, "retry", GraphControlParams{ExpectedRevision: run.Revision, NodeRunID: run.Nodes[index].ID, IdempotencyKey: uuid.NewString(), Reason: "Verified the remote idempotency key; retry the same invocation"})
	if err != nil {
		t.Fatalf("explicit bounded retry rejected: %v", err)
	}
}

func TestGraphV2PostgresApproverReplacementInvalidatesPriorVotes(t *testing.T) {
	f := newGraphFixture(t)
	m2, u2, m3, u3 := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, pair := range [][2]string{{m2, u2}, {m3, u3}} {
		if _, err := f.pool.Exec(context.Background(), `INSERT INTO member(id,workspace_id,user_id,role) VALUES($1,$2,$3,'member')`, pair[0], f.ws, pair[1]); err != nil {
			t.Fatal(err)
		}
	}
	d := graph.Definition{SchemaVersion: 2, Name: "Approval replacement", InputSchema: f.version.Definition.InputSchema, Nodes: []graph.Node{{Key: "start", Type: graph.NodeTypeStart}, {Key: "gate", Type: graph.NodeTypeApproval, Approval: &graph.ApprovalConfig{ApproverIDs: []string{f.member, m2}, Mode: "all"}}, {Key: "end", Type: graph.NodeTypeEnd}}, Edges: []graph.Edge{{From: "start", To: "gate"}, {From: "gate", To: "end"}}}
	f = graphV2Publish(t, f, d)
	run := graphV2StartAny(t, f)
	oldID := run.Approval.ID
	var err error
	run, err = f.svc.Approve(context.Background(), f.ws, f.user, run.ID, GraphApprovalParams{ExpectedRevision: run.Revision, IdempotencyKey: uuid.NewString(), ApprovalID: oldID, CandidateDigest: run.Approval.CandidateDigest, Decision: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	run, err = f.svc.Control(context.Background(), f.ws, f.user, run.ID, "approver", GraphControlParams{ExpectedRevision: run.Revision, IdempotencyKey: uuid.NewString(), NodeRunID: run.Approval.NodeRunID, PreviousApproverID: m2, ApproverID: m3, Reason: "Member unavailable"})
	if err != nil {
		t.Fatal(err)
	}
	if run.Approval.ID == oldID || len(run.Approval.Votes) != 0 || len(run.Approval.ApproverIDs) != 2 || run.Approval.Mode != "all" {
		t.Fatal("replacement silently preserved votes or changed quorum")
	}
	if len(run.Approvals) != 2 || run.Approvals[0].State != "invalidated" {
		t.Fatal("old candidate approval not retained as invalidated history")
	}
}

func TestGraphV2PostgresStorageBudgetStillPermitsCancellationAndStopReceipt(t *testing.T) {
	f := graphV2ParallelFixture(t)
	run := graphV2Start(t, f)
	index := graphV2NodeIndex(t, run, "left")
	attempt := run.Nodes[index].Attempts[0]
	taskID := util.MustParseUUID(attempt.TaskID)
	if _, err := f.pool.Exec(context.Background(), `UPDATE agent_task_queue SET status='dispatched',dispatched_at=now() WHERE id=$1`, taskID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tasks.StartTask(WithGraphExecutor(context.Background(), "budget-executor"), taskID); err != nil {
		t.Fatal(err)
	}
	raw, _ := graphJSON(run)
	padding := strings.Repeat("x", (15<<20)-len(raw)-32768)
	run.RootSnapshot = json.RawMessage(`{"padding":"` + padding + `"}`)
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
	_, err = f.svc.Submit(context.Background(), f.ws, attempt.TaskID, uuid.NewString(), graph.Result{Identity: attempt.Manifest.Identity, Outcome: graph.OutcomePass, Output: json.RawMessage(`{"summary":"` + strings.Repeat("y", 128<<10) + `"}`)})
	var graphErr *GraphError
	if !errors.As(err, &graphErr) || graphErr.Code != "storage_budget_exhausted" {
		t.Fatalf("expected explicit run storage budget, got %v", err)
	}
	run, err = f.svc.GetRun(context.Background(), f.ws, f.user, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.ControlState != "cancel_requested" {
		t.Fatal("storage budget failed without durable stop intent")
	}
	receipt := GraphTaskReceipt{Identity: attempt.Manifest.Identity, ProviderExit: "cancelled", DeliveryStatus: "not_required", ExecutorID: "budget-executor", StoppedAt: time.Now().UTC()}
	if _, err = f.tasks.CompleteTask(WithGraphReceipt(context.Background(), receipt), taskID, []byte(`{"output":"stopped"}`), "", "", "", false, ""); err != nil {
		t.Fatalf("stop proof rejected after budget exhaustion: %v", err)
	}
	run = f.reconcile(t, run)
	if run.State != "failed" || run.TerminationReason != "storage_budget_exhausted" {
		t.Fatalf("budget failure could not drain to terminal: %s %s", run.State, run.TerminationReason)
	}
}

func TestGraphV2PostgresRemovedApproverCanBeReplaced(t *testing.T) {
	f := newGraphFixture(t)
	otherMember, otherUser := uuid.NewString(), uuid.NewString()
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO member(id,workspace_id,user_id,role) VALUES($1,$2,$3,'member')`, otherMember, f.ws, otherUser); err != nil {
		t.Fatal(err)
	}
	d := graph.Definition{SchemaVersion: 2, Name: "Unavailable approver", InputSchema: f.version.Definition.InputSchema, Nodes: []graph.Node{{Key: "start", Type: graph.NodeTypeStart}, {Key: "gate", Type: graph.NodeTypeApproval, ApproverID: otherMember}, {Key: "end", Type: graph.NodeTypeEnd}}, Edges: []graph.Edge{{From: "start", To: "gate"}, {From: "gate", To: "end"}}}
	f = graphV2Publish(t, f, d)
	if _, err := f.pool.Exec(context.Background(), `DELETE FROM member WHERE id=$1`, otherMember); err != nil {
		t.Fatal(err)
	}
	run := graphV2StartAny(t, f)
	if run.State != "blocked" || run.BlockReason != "approver_unavailable" {
		t.Fatalf("removed approver not visible: %s %s", run.State, run.BlockReason)
	}
	if run.Approval == nil {
		t.Fatal("unavailable approval gate lost its candidate and cannot be reassigned")
	}
	var err error
	run, err = f.svc.Control(context.Background(), f.ws, f.user, run.ID, "approver", GraphControlParams{ExpectedRevision: run.Revision, NodeRunID: run.Approval.NodeRunID, ApproverID: f.member, IdempotencyKey: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	if run.Approval == nil || run.Approval.ApproverID != f.member {
		t.Fatal("failed to repair unavailable approval")
	}
}

func graphV2Start(t *testing.T, f graphFixture) GraphRunRecord {
	t.Helper()
	run, err := f.svc.StartRun(context.Background(), f.ws, f.user, GraphStartParams{VersionID: f.version.ID, RootIssueID: f.issue,
		Input: json.RawMessage(`{"goal":"parallel verification"}`), RuntimeBindings: map[string]string{"left": f.runtime, "right": f.runtime}, IdempotencyKey: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	return f.reconcile(t, run)
}

func graphV2NodeIndex(t *testing.T, run GraphRunRecord, key string) int {
	t.Helper()
	for i, n := range run.Nodes {
		if n.NodeKey == key {
			return i
		}
	}
	t.Fatalf("node %q not found", key)
	return -1
}

func TestGraphV2PostgresParallelJoinAndIntermediateApproval(t *testing.T) {
	f := graphV2ParallelFixture(t)
	run := graphV2Start(t, f)
	left, right := graphV2NodeIndex(t, run, "left"), graphV2NodeIndex(t, run, "right")
	if len(run.Nodes[left].Attempts) != 1 || len(run.Nodes[right].Attempts) != 1 {
		t.Fatalf("both independent branches must be queued together: left=%d right=%d", len(run.Nodes[left].Attempts), len(run.Nodes[right].Attempts))
	}
	f.complete(t, run, right)
	run = f.reconcile(t, run)
	if run.Nodes[graphV2NodeIndex(t, run, "join")].State == "succeeded" || run.Approval != nil {
		t.Fatal("join/approval ran before every selected dependency completed")
	}
	f.complete(t, run, left)
	run = f.reconcile(t, run)
	if run.State != "waiting_approval" || run.Approval == nil {
		t.Fatalf("expected approval after join, got %s", run.State)
	}
	approved, err := f.svc.Approve(context.Background(), f.ws, f.user, run.ID, GraphApprovalParams{ExpectedRevision: run.Revision, IdempotencyKey: uuid.NewString(), ApprovalID: run.Approval.ID, CandidateDigest: run.Approval.CandidateDigest, Decision: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	approved = f.reconcile(t, approved)
	if approved.State != "succeeded" {
		t.Fatalf("graph did not finish: %s", approved.State)
	}
}

func TestGraphV2PostgresRestartDoesNotDuplicateParallelAttempts(t *testing.T) {
	f := graphV2ParallelFixture(t)
	run := graphV2Start(t, f)
	for i := 0; i < 4; i++ {
		run = f.reconcile(t, run)
	}
	for _, key := range []string{"left", "right"} {
		n := run.Nodes[graphV2NodeIndex(t, run, key)]
		if len(n.Attempts) != 1 {
			t.Fatalf("%s got %d attempts while still active", key, len(n.Attempts))
		}
	}
}
