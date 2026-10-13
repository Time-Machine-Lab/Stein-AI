package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/graph"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The Graph integration suite requires an explicitly selected test server and
// creates a private schema. It never migrates an ambient application database.
// Legacy table columns are generated from sqlc models; the Graph migrations and
// execution fences are applied verbatim, each outside a transaction. This keeps
// Graph transaction/race tests independent of optional pgvector integrations.
func graphIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("GRAPH_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set GRAPH_TEST_DATABASE_URL to an isolated PostgreSQL test server")
	}
	ctx := context.Background()
	base, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if err = base.Ping(ctx); err != nil {
		base.Close()
		t.Fatal(err)
	}
	schema := "graph_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = base.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
		base.Close()
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = base.Exec(context.Background(), `DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`)
		base.Close()
	})
	for _, entry := range []struct {
		name  string
		model any
	}{{"workspace", db.Workspace{}}, {"member", db.Member{}}, {"issue", db.Issue{}}, {"agent", db.Agent{}}, {"agent_runtime", db.AgentRuntime{}}, {"agent_task_queue", db.AgentTaskQueue{}}, {"attachment", db.Attachment{}}, {"chat_session", db.ChatSession{}}, {"task_token", db.TaskToken{}}} {
		if _, err = pool.Exec(ctx, graphTestTable(entry.name, entry.model)); err != nil {
			t.Fatalf("create test table %s: %v", entry.name, err)
		}
	}
	ownerFence, err := os.ReadFile(filepath.Join("..", "..", "migrations", "284_task_owner_row_fence.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(ownerFence)); err != nil {
		t.Fatalf("install actual task owner fence: %v", err)
	}
	_, err = pool.Exec(ctx, `CREATE TABLE autopilot_run (id uuid,issue_id uuid,status text);
ALTER TABLE agent_task_queue ADD CHECK (status IN ('deferred','queued','dispatched','running','waiting_local_directory','completed','failed','cancelled'));
ALTER TABLE agent_task_queue ADD CHECK (originator_user_id IS NULL OR accountable_user_id IS NOT DISTINCT FROM originator_user_id);
ALTER TABLE member ADD CHECK (role IN ('owner','admin','member'));
CREATE UNIQUE INDEX test_task_pending ON agent_task_queue(issue_id,agent_id) WHERE status IN ('queued','dispatched');`)
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	for _, file := range files {
		number, _ := strconv.Atoi(strings.SplitN(filepath.Base(file), "_", 2)[0])
		if number < 332 || number > 356 {
			continue
		}
		data, e := os.ReadFile(file)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, string(data)); e != nil {
			t.Fatalf("migration %s: %v", file, e)
		}
	}
	return pool
}
func graphTestTable(name string, model any) string {
	typ := reflect.TypeOf(model)
	columns := []string{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		column := strings.Split(field.Tag.Get("json"), ",")[0]
		sqlType := ""
		switch field.Type {
		case reflect.TypeOf(pgtype.UUID{}):
			sqlType = "uuid"
		case reflect.TypeOf(pgtype.Text{}):
			sqlType = "text"
		case reflect.TypeOf(pgtype.Int4{}):
			sqlType = "integer"
		case reflect.TypeOf(pgtype.Int8{}):
			sqlType = "bigint"
		case reflect.TypeOf(pgtype.Bool{}):
			sqlType = "boolean"
		case reflect.TypeOf(pgtype.Float8{}):
			sqlType = "double precision"
		case reflect.TypeOf(pgtype.Numeric{}):
			sqlType = "numeric"
		case reflect.TypeOf(pgtype.Date{}):
			sqlType = "date"
		case reflect.TypeOf(pgtype.Timestamptz{}):
			sqlType = "timestamptz"
			if column == "created_at" || column == "updated_at" {
				sqlType += " DEFAULT now()"
			}
		default:
			switch field.Type.Kind() {
			case reflect.String:
				sqlType = "text NOT NULL DEFAULT ''"
			case reflect.Bool:
				sqlType = "boolean NOT NULL DEFAULT false"
			case reflect.Int32, reflect.Int64, reflect.Int:
				sqlType = "bigint NOT NULL DEFAULT 0"
			case reflect.Float32, reflect.Float64:
				sqlType = "double precision NOT NULL DEFAULT 0"
			case reflect.Slice:
				if field.Type.Elem().Kind() == reflect.Uint8 {
					sqlType = "jsonb"
				} else if field.Type.Elem() == reflect.TypeOf(pgtype.UUID{}) {
					sqlType = "uuid[] NOT NULL DEFAULT '{}'"
				} else {
					sqlType = "text[] NOT NULL DEFAULT '{}'"
				}
			default:
				panic("unsupported test model type " + field.Type.String())
			}
		}
		if column == "id" {
			sqlType = "uuid PRIMARY KEY DEFAULT gen_random_uuid()"
		}
		columns = append(columns, pgx.Identifier{column}.Sanitize()+" "+sqlType)
	}
	return `CREATE TABLE ` + pgx.Identifier{name}.Sanitize() + ` (` + strings.Join(columns, ",") + `)`
}

type graphFixture struct {
	pool                                    *pgxpool.Pool
	svc                                     *GraphService
	tasks                                   *TaskService
	ws, user, member, agent, runtime, issue string
	version                                 GraphVersionRecord
}

func newGraphFixture(t *testing.T) graphFixture {
	t.Helper()
	ctx := context.Background()
	pool := graphIntegrationPool(t)
	f := graphFixture{pool: pool, ws: uuid.NewString(), user: uuid.NewString(), member: uuid.NewString(), agent: uuid.NewString(), runtime: uuid.NewString(), issue: uuid.NewString()}
	for _, command := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO workspace(id,name,slug,issue_counter) VALUES($1,'Graph test',$2,1)`, []any{f.ws, uuid.NewString()}},
		{`INSERT INTO member(id,workspace_id,user_id,role) VALUES($1,$2,$3,'owner')`, []any{f.member, f.ws, f.user}},
		{`INSERT INTO agent_runtime(id,workspace_id,status,owner_id) VALUES($1,$2,'online',$3)`, []any{f.runtime, f.ws, f.user}},
		{`INSERT INTO agent(id,workspace_id,name,runtime_id,owner_id,status,max_concurrent_tasks) VALUES($1,$2,'Graph test agent',$3,$4,'idle',3)`, []any{f.agent, f.ws, f.runtime, f.user}},
		{`INSERT INTO issue(id,workspace_id,title,description,status,priority,creator_type,creator_id,number) VALUES($1,$2,'Graph root','Frozen requirement','todo','none','member',$3,1)`, []any{f.issue, f.ws, f.user}},
	} {
		if _, err := pool.Exec(ctx, command.sql, command.args...); err != nil {
			t.Fatal(err)
		}
	}
	f.tasks = NewTaskService(db.New(pool), pool, nil, events.New())
	f.svc = NewGraphService(db.New(pool), pool)
	f.tasks.Graph = f.svc
	f.svc.CreateTask = f.tasks.CreateGraphTaskTx
	f.svc.PrepareNode = func(context.Context, pgx.Tx, string, string, graph.Node) (GraphNodeSnapshot, error) {
		return GraphNodeSnapshot{AgentConfig: json.RawMessage(`{"instructions":"frozen"}`)}, nil
	}
	f.svc.PrepareRun = func(_ context.Context, _ pgx.Tx, _, _ string, snapshot json.RawMessage, _ *graph.RepositoryInput) (json.RawMessage, error) {
		return snapshot, nil
	}
	f.svc.AuthorizeRun = func(ctx context.Context, tx pgx.Tx, ws, user string, _ graph.Definition, _ map[string]string) error {
		_, _, err := graphMember(ctx, tx, ws, user)
		return err
	}
	definition := graph.Definition{SchemaVersion: 1, Name: "Serial verification", InputSchema: json.RawMessage(`{"type":"object","properties":{"goal":{"type":"string"}},"required":["goal"]}`), Nodes: []graph.Node{
		{Key: "start", Type: graph.NodeTypeStart},
		{Key: "implement", Type: graph.NodeTypeAgent, AgentID: f.agent, Instructions: "Implement fixed goal", Inputs: map[string]graph.InputBinding{"goal": {Source: graph.BindingRunInput, Path: "/goal"}}, OutputSchema: json.RawMessage(`{"type":"object","properties":{"summary":{"type":"string"}},"required":["summary"]}`), ExecutionMode: graph.ExecutionNone, MaxAttempts: 3, TimeoutSeconds: 900},
		{Key: "verify", Type: graph.NodeTypeAgent, AgentID: f.agent, Instructions: "Independently verify", Inputs: map[string]graph.InputBinding{"summary": {Source: graph.BindingNodeOutput, NodeKey: "implement", Path: "/summary"}}, OutputSchema: json.RawMessage(`{"type":"object","properties":{"summary":{"type":"string"}},"required":["summary"]}`), ExecutionMode: graph.ExecutionNone, MaxAttempts: 3, TimeoutSeconds: 900},
		{Key: "approval", Type: graph.NodeTypeApproval, ApproverID: f.member}, {Key: "end", Type: graph.NodeTypeEnd},
	}, Edges: []graph.Edge{{From: "start", To: "implement"}, {From: "implement", To: "verify"}, {From: "verify", To: "approval"}, {From: "approval", To: "end"}}}
	draft, err := f.svc.CreateDefinition(ctx, f.ws, f.user, definition, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	f.version, err = f.svc.Publish(ctx, f.ws, f.user, draft.ID, draft.Revision, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func (f graphFixture) start(t *testing.T) GraphRunRecord {
	t.Helper()
	run, err := f.svc.StartRun(context.Background(), f.ws, f.user, GraphStartParams{VersionID: f.version.ID, RootIssueID: f.issue, Input: json.RawMessage(`{"goal":"Implement deterministically"}`), RuntimeBindings: map[string]string{"implement": f.runtime, "verify": f.runtime}, IdempotencyKey: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	return run
}
func (f graphFixture) reconcile(t *testing.T, run GraphRunRecord) GraphRunRecord {
	t.Helper()
	next, err := f.svc.ReconcileRun(context.Background(), f.ws, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	return next
}
func (f graphFixture) complete(t *testing.T, run GraphRunRecord, nodeIndex int) GraphSubmissionRecord {
	t.Helper()
	ctx := context.Background()
	attempt := run.Nodes[nodeIndex].Attempts[len(run.Nodes[nodeIndex].Attempts)-1]
	id := util.MustParseUUID(attempt.TaskID)
	if _, err := f.pool.Exec(ctx, `UPDATE agent_task_queue SET status='dispatched',dispatched_at=now() WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tasks.StartTask(WithGraphExecutor(ctx, "test-executor"), id); err != nil {
		t.Fatal(err)
	}
	sub, err := f.svc.Submit(ctx, f.ws, attempt.TaskID, uuid.NewString(), graph.Result{Identity: attempt.Manifest.Identity, Outcome: graph.OutcomePass, Output: json.RawMessage(`{"summary":"verified"}`)})
	if err != nil {
		t.Fatal(err)
	}
	receipt := GraphTaskReceipt{Identity: attempt.Manifest.Identity, SubmissionID: sub.ID, ProviderExit: "success", DeliveryStatus: "not_required", ExecutorID: "test-executor", StoppedAt: time.Now().UTC()}
	if _, err = f.tasks.CompleteTask(WithGraphReceipt(ctx, receipt), id, []byte(`{"output":"complete"}`), "", "", "", false, ""); err != nil {
		t.Fatal(err)
	}
	return sub
}

func TestGraphPostgresSerialPauseApprovalAndOwnership(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	run := f.reconcile(t, f.start(t))
	if len(run.Nodes[1].Attempts) != 1 || len(run.Nodes[2].Attempts) != 0 {
		t.Fatal("serial first attempt not isolated")
	}
	if _, err := f.pool.Exec(ctx, `UPDATE issue SET status='done' WHERE id=$1`, f.issue); err == nil {
		t.Fatal("legacy status write escaped Graph fence")
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO agent_task_queue(agent_id,runtime_id,issue_id,status) VALUES($1,$2,$3,'queued')`, f.agent, f.runtime, run.Nodes[1].IssueID); err == nil {
		t.Fatal("legacy enqueue escaped Graph fence")
	}
	f.complete(t, run, 1)
	current, err := f.svc.GetRun(ctx, f.ws, f.user, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	run, err = f.svc.Control(ctx, f.ws, f.user, run.ID, "pause", GraphControlParams{ExpectedRevision: current.Revision, IdempotencyKey: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	run = f.reconcile(t, run)
	if run.Nodes[1].State != "succeeded" || len(run.Nodes[2].Attempts) != 0 {
		t.Fatal("pause lost acceptance or released successor")
	}
	run, err = f.svc.Control(ctx, f.ws, f.user, run.ID, "resume", GraphControlParams{ExpectedRevision: run.Revision, IdempotencyKey: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	run = f.reconcile(t, run)
	f.complete(t, run, 2)
	run = f.reconcile(t, run)
	if run.State != "waiting_approval" || run.Approval == nil {
		t.Fatal("missing approval")
	}
	if _, err = f.svc.Approve(ctx, f.ws, f.user, run.ID, GraphApprovalParams{ExpectedRevision: run.Revision, IdempotencyKey: uuid.NewString(), ApprovalID: run.Approval.ID, CandidateDigest: "stale", Decision: "approved"}); err == nil {
		t.Fatal("stale candidate accepted")
	}
	p := GraphApprovalParams{ExpectedRevision: run.Revision, IdempotencyKey: uuid.NewString(), ApprovalID: run.Approval.ID, CandidateDigest: run.Approval.CandidateDigest, Decision: "approved"}
	run, err = f.svc.Approve(ctx, f.ws, f.user, run.ID, p)
	if err != nil {
		t.Fatal(err)
	}
	if run.State != "succeeded" {
		t.Fatal("approval did not finish run")
	}
	if replay, err := f.svc.Approve(ctx, f.ws, f.user, run.ID, p); err != nil || replay.State != "succeeded" {
		t.Fatalf("duplicate approval failed: %v", err)
	}
}

func TestGraphPostgresConcurrentStartAndAttemptRollback(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	var mu sync.Mutex
	started := []GraphRunRecord{}
	failures := 0
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run, err := f.svc.StartRun(ctx, f.ws, f.user, GraphStartParams{VersionID: f.version.ID, RootIssueID: f.issue, Input: json.RawMessage(`{"goal":"same root"}`), RuntimeBindings: map[string]string{"implement": f.runtime, "verify": f.runtime}, IdempotencyKey: uuid.NewString()})
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				started = append(started, run)
			} else {
				failures++
			}
		}()
	}
	wg.Wait()
	if len(started) != 1 || failures != 1 {
		t.Fatalf("start race: %d succeeded, %d failed", len(started), failures)
	}
	create := f.svc.CreateTask
	f.svc.CreateTask = func(ctx context.Context, tx pgx.Tx, p GraphTaskCreateParams) (db.AgentTaskQueue, error) {
		task, err := create(ctx, tx, p)
		if err != nil {
			return task, err
		}
		return task, errors.New("injected crash after queue insert")
	}
	if _, err := f.svc.ReconcileRun(ctx, f.ws, started[0].ID); err == nil {
		t.Fatal("injected task failure ignored")
	}
	var tasks, bindings int
	if err := f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM agent_task_queue),(SELECT count(*) FROM graph_task_attempt)`).Scan(&tasks, &bindings); err != nil {
		t.Fatal(err)
	}
	if tasks != 0 || bindings != 0 {
		t.Fatalf("partial attempt escaped rollback: tasks=%d bindings=%d", tasks, bindings)
	}
	f.svc.CreateTask = create
	run := f.reconcile(t, started[0])
	run = f.reconcile(t, run)
	if len(run.Nodes[1].Attempts) != 1 {
		t.Fatal("replay duplicated attempt")
	}
}

func TestGraphPostgresUnknownExecutionCannotRetryOrDetach(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	run := f.reconcile(t, f.start(t))
	attempt := run.Nodes[1].Attempts[0]
	id := util.MustParseUUID(attempt.TaskID)
	if _, err := f.pool.Exec(ctx, `UPDATE agent_task_queue SET status='dispatched',dispatched_at=now() WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tasks.StartTask(WithGraphExecutor(ctx, "test-executor"), id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tasks.FailTask(ctx, id, "connection lost", "", "", "", "runtime_offline", false, ""); err != nil {
		t.Fatal(err)
	}
	run = f.reconcile(t, run)
	if run.State != "blocked" || run.BlockReason != "execution_unknown" {
		t.Fatalf("unsafe loss handling: %s / %s", run.State, run.BlockReason)
	}
	if _, err := f.svc.Control(ctx, f.ws, f.user, run.ID, "retry", GraphControlParams{ExpectedRevision: run.Revision, IdempotencyKey: uuid.NewString(), NodeRunID: run.Nodes[1].ID}); err == nil {
		t.Fatal("unknown process was retried")
	}
	run, err := f.svc.Control(ctx, f.ws, f.user, run.ID, "cancel", GraphControlParams{ExpectedRevision: run.Revision, IdempotencyKey: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	run = f.reconcile(t, run)
	if run.State == "cancelled" {
		t.Fatal("cancellation inferred process stop from failed task")
	}
	receipt := GraphTaskReceipt{Identity: attempt.Manifest.Identity, ProviderExit: "failed", DeliveryStatus: "not_required", ExecutorID: "wrong-executor", StoppedAt: time.Now().UTC()}
	if err = f.svc.RecordTaskTerminal(ctx, f.ws, attempt.TaskID, receipt); err == nil {
		t.Fatal("wrong executor acknowledged stop")
	}
	receipt.ExecutorID = "test-executor"
	if err = f.svc.RecordTaskTerminal(ctx, f.ws, attempt.TaskID, receipt); err != nil {
		t.Fatal(err)
	}
	run = f.reconcile(t, run)
	if run.State != "cancelled" {
		t.Fatalf("stop receipt did not settle cancellation: %s", run.State)
	}
	if err = f.svc.DetachIssue(ctx, f.ws, f.user, f.issue, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
}

func TestGraphPostgresClaimPauseRaceAndImmutableSubmission(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	run := f.reconcile(t, f.start(t))
	attempt := run.Nodes[1].Attempts[0]
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM graph_run WHERE id=$1 FOR UPDATE`, run.ID); err != nil {
		t.Fatal(err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var allowed bool
	if err = f.pool.QueryRow(queryCtx, `SELECT graph_task_claim_allowed($1)`, attempt.TaskID).Scan(&allowed); err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("claim entered busy run")
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	run, err = f.svc.Control(ctx, f.ws, f.user, run.ID, "pause", GraphControlParams{ExpectedRevision: run.Revision, IdempotencyKey: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.pool.QueryRow(ctx, `SELECT graph_task_claim_allowed($1)`, attempt.TaskID).Scan(&allowed); err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("claim entered paused run")
	}
	if _, err = f.pool.Exec(ctx, `UPDATE agent_task_queue SET context='{}' WHERE id=$1`, attempt.TaskID); err == nil {
		t.Fatal("mutable task input overwrote frozen manifest")
	}
	if _, err = f.svc.GetRun(ctx, uuid.NewString(), f.user, run.ID); err == nil {
		t.Fatal("cross-workspace run read accepted")
	}
}

func TestGraphPostgresRetryBudgetAndReceiptReplay(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	run := f.reconcile(t, f.start(t))
	for number := 1; number <= 3; number++ {
		node := run.Nodes[1]
		if len(node.Attempts) != number {
			t.Fatalf("attempt count=%d, want %d", len(node.Attempts), number)
		}
		attempt := node.Attempts[number-1]
		taskID := util.MustParseUUID(attempt.TaskID)
		if _, err := f.pool.Exec(ctx, `UPDATE agent_task_queue SET status='dispatched',dispatched_at=now() WHERE id=$1`, taskID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.tasks.StartTask(WithGraphExecutor(ctx, "test-executor"), taskID); err != nil {
			t.Fatal(err)
		}
		receipt := GraphTaskReceipt{Identity: attempt.Manifest.Identity, ProviderExit: "failed", DeliveryStatus: "not_required", ExecutorID: "test-executor", StoppedAt: time.Now().UTC()}
		if _, err := f.tasks.FailTask(WithGraphReceipt(ctx, receipt), taskID, "runtime unavailable", "", "", "", "runtime_offline", false, ""); err != nil {
			t.Fatal(err)
		}
		// The raw daemon receipt omits server-owned retry classification. Both
		// endpoints must canonicalize it identically before comparing replays.
		if err := f.svc.RecordTaskTerminal(ctx, f.ws, attempt.TaskID, receipt); err != nil {
			t.Fatalf("raw receipt replay: %v", err)
		}
		if child, err := f.tasks.MaybeRetryFailedTask(ctx, db.AgentTaskQueue{ID: taskID, Status: "failed", FailureReason: pgtype.Text{String: "runtime_offline", Valid: true}, Attempt: 1, MaxAttempts: 3}); err != nil || child != nil {
			t.Fatal("legacy retry escaped Graph policy")
		}
		run = f.reconcile(t, run)
	}
	if run.State != "failed" || run.TerminationReason != "budget_exhausted" || len(run.Nodes[1].Attempts) != 3 {
		t.Fatalf("budget not enforced: %+v", run)
	}
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("hidden retries created %d tasks", count)
	}
}

func TestGraphPostgresSubmissionIdempotencyAndDisabledAdvancement(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	run := f.reconcile(t, f.start(t))
	attempt := run.Nodes[1].Attempts[0]
	taskID := util.MustParseUUID(attempt.TaskID)
	if _, err := f.pool.Exec(ctx, `UPDATE agent_task_queue SET status='dispatched',dispatched_at=now() WHERE id=$1`, taskID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tasks.StartTask(WithGraphExecutor(ctx, "test-executor"), taskID); err != nil {
		t.Fatal(err)
	}
	key := uuid.NewString()
	result := graph.Result{Identity: attempt.Manifest.Identity, Outcome: graph.OutcomePass, Output: json.RawMessage(`{"summary":"same"}`)}
	sub, err := f.svc.Submit(ctx, f.ws, attempt.TaskID, key, result)
	if err != nil {
		t.Fatal(err)
	}
	changed := result
	changed.Output = json.RawMessage(`{"summary":"changed"}`)
	if _, err = f.svc.Submit(ctx, f.ws, attempt.TaskID, key, changed); err == nil {
		t.Fatal("idempotency key changed content")
	}
	receipt := GraphTaskReceipt{Identity: attempt.Manifest.Identity, SubmissionID: sub.ID, ProviderExit: "success", DeliveryStatus: "not_required", ExecutorID: "test-executor", StoppedAt: time.Now().UTC()}
	if _, err = f.tasks.CompleteTask(WithGraphReceipt(ctx, receipt), taskID, []byte(`{}`), "", "", "", false, ""); err != nil {
		t.Fatal(err)
	}
	f.svc.AdvanceEnabled = false
	run = f.reconcile(t, run)
	if run.Nodes[1].State != "succeeded" || len(run.Nodes[2].Attempts) != 0 {
		t.Fatal("disabled advancement did not drain the completed node safely")
	}
	if replay, err := f.svc.Submit(ctx, f.ws, attempt.TaskID, key, result); err != nil || replay.ID != sub.ID {
		t.Fatalf("immutable submission replay after completion failed: %v", err)
	}
	f.svc.AdvanceEnabled = true
	run = f.reconcile(t, run)
	if len(run.Nodes[2].Attempts) != 1 {
		t.Fatal("restored advancement did not resume successor")
	}
}

func TestGraphPostgresArtifactRetentionPreservesProducer(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	run := f.reconcile(t, f.start(t))
	attempt := run.Nodes[1].Attempts[0]
	artifact := graph.Artifact{ID: uuid.NewString(), Type: graph.ArtifactAttachmentRef, ProducerTaskID: attempt.TaskID, ContentHash: strings.Repeat("a", 64), MediaType: "text/plain", Size: 4}
	if _, err := f.pool.Exec(ctx, `INSERT INTO attachment(id,workspace_id) VALUES($1,$2)`, artifact.ID, f.ws); err != nil {
		t.Fatal(err)
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = graphRetainArtifacts(ctx, tx, &run, run.Nodes[1], graph.Result{Artifacts: []graph.Artifact{artifact}}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `DELETE FROM attachment WHERE id=$1`, artifact.ID); err == nil {
		t.Fatal("accepted artifact attachment was deleted")
	}
	copyRun := run
	copyRun.ID = uuid.NewString()
	copyRun.Artifacts = nil
	tx, err = f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = graphRetainArtifacts(ctx, tx, &copyRun, run.Nodes[2], graph.Result{Artifacts: []graph.Artifact{artifact}}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var producer string
	if err = f.pool.QueryRow(ctx, `SELECT task_id::text FROM graph_artifact WHERE id=$1`, artifact.ID).Scan(&producer); err != nil {
		t.Fatal(err)
	}
	if producer != attempt.TaskID {
		t.Fatal("passthrough artifact producer was rewritten")
	}
}

func TestGraphPostgresInvalidSubmissionReferenceRetainsStopEvidence(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	run := f.reconcile(t, f.start(t))
	attempt := run.Nodes[1].Attempts[0]
	taskID := util.MustParseUUID(attempt.TaskID)
	if _, err := f.pool.Exec(ctx, `UPDATE agent_task_queue SET status='dispatched',dispatched_at=now() WHERE id=$1`, taskID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tasks.StartTask(WithGraphExecutor(ctx, "test-executor"), taskID); err != nil {
		t.Fatal(err)
	}
	receipt := GraphTaskReceipt{Identity: attempt.Manifest.Identity, SubmissionID: uuid.NewString(), ProviderExit: "success", DeliveryStatus: "not_required", ExecutorID: "test-executor", StoppedAt: time.Now().UTC()}
	if _, err := f.tasks.CompleteTask(WithGraphReceipt(ctx, receipt), taskID, []byte(`{}`), "", "", "", false, ""); err != nil {
		t.Fatalf("provider completion discarded authenticated physical stop: %v", err)
	}
	run = f.reconcile(t, run)
	if run.State != "blocked" || run.Nodes[1].BlockReason != "output_invalid" || run.Nodes[1].Attempts[0].State != "completed" || run.Nodes[1].Attempts[0].StoppedAt == nil {
		t.Fatalf("invalid submission advanced or lost stop evidence: %+v", run.Nodes[1])
	}
	if len(run.Nodes[2].Attempts) != 0 {
		t.Fatal("invalid submission scheduled successor")
	}
	if err := f.svc.RecordTaskTerminal(ctx, f.ws, attempt.TaskID, receipt); err != nil {
		t.Fatalf("physical receipt replay failed: %v", err)
	}
}

func TestGraphPostgresStartRejectsLegacyChildAndAutopilot(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	child := uuid.NewString()
	if _, err := f.pool.Exec(ctx, `INSERT INTO issue(id,workspace_id,title,status,parent_issue_id) VALUES($1,$2,'Legacy child','todo',$3)`, child, f.ws, f.issue); err != nil {
		t.Fatal(err)
	}
	start := func() error {
		_, err := f.svc.StartRun(ctx, f.ws, f.user, GraphStartParams{VersionID: f.version.ID, RootIssueID: f.issue, Input: json.RawMessage(`{"goal":"safe takeover"}`), RuntimeBindings: map[string]string{"implement": f.runtime, "verify": f.runtime}, IdempotencyKey: uuid.NewString()})
		return err
	}
	if err := start(); err == nil {
		t.Fatal("Graph took over an unfinished ordinary child issue")
	}
	if _, err := f.pool.Exec(ctx, `UPDATE issue SET status='done' WHERE id=$1`, child); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO autopilot_run(id,issue_id,status) VALUES($1,$2,'running')`, uuid.NewString(), f.issue); err != nil {
		t.Fatal(err)
	}
	if err := start(); err == nil {
		t.Fatal("Graph took over an active Autopilot run")
	}
	if _, err := f.pool.Exec(ctx, `UPDATE autopilot_run SET status='completed' WHERE issue_id=$1`, f.issue); err != nil {
		t.Fatal(err)
	}
	if err := start(); err != nil {
		t.Fatalf("completed legacy work prevented takeover: %v", err)
	}
}

func TestGraphPostgresHeldClaimCanRebindOnlyBeforeStart(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	run := f.reconcile(t, f.start(t))
	taskID := run.Nodes[1].Attempts[0].TaskID
	bind := func(executor string, dispatched time.Time) error {
		tx, err := f.pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if err = f.svc.BindTaskExecutorForClaimTx(ctx, tx, taskID, executor, dispatched); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	var first time.Time
	if err := f.pool.QueryRow(ctx, `UPDATE agent_task_queue SET status='dispatched',dispatched_at=now() WHERE id=$1 RETURNING dispatched_at`, taskID).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err := bind("old-daemon", first); err != nil {
		t.Fatal(err)
	}
	f.svc.AdvanceEnabled = false
	if _, err := f.tasks.StartTask(WithGraphExecutor(ctx, "old-daemon"), util.MustParseUUID(taskID)); err == nil {
		t.Fatal("held claim started")
	}
	var state string
	if err := f.pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id=$1`, taskID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "queued" {
		t.Fatalf("held claim not returned to queue: %s", state)
	}
	f.svc.AdvanceEnabled = true
	var second time.Time
	if err := f.pool.QueryRow(ctx, `UPDATE agent_task_queue SET status='dispatched',dispatched_at=now() WHERE id=$1 RETURNING dispatched_at`, taskID).Scan(&second); err != nil {
		t.Fatal(err)
	}
	if err := bind("old-daemon", first); err == nil {
		t.Fatal("stale claim rebound executor")
	}
	if err := bind("new-daemon", second); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tasks.StartTask(WithGraphExecutor(ctx, "old-daemon"), util.MustParseUUID(taskID)); err == nil {
		t.Fatal("old executor started replacement claim")
	}
	if _, err := f.tasks.StartTask(WithGraphExecutor(ctx, "new-daemon"), util.MustParseUUID(taskID)); err != nil {
		t.Fatal(err)
	}
	if err := bind("third-daemon", second); err == nil {
		t.Fatal("started executor was replaced")
	}
	var attempts int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM graph_task_attempt WHERE run_id=$1`, run.ID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatal("held claim consumed another attempt")
	}
}

func TestGraphPostgresHistoricalArchiveCannotProjectRoot(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	run := f.reconcile(t, f.start(t))
	var err error
	run, err = f.svc.Control(ctx, f.ws, f.user, run.ID, "cancel", GraphControlParams{ExpectedRevision: run.Revision, IdempotencyKey: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	run = f.reconcile(t, run)
	if err = f.svc.DetachIssue(ctx, f.ws, f.user, f.issue, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE issue SET status='todo' WHERE id=$1`, f.issue); err != nil {
		t.Fatal(err)
	}
	run, err = f.svc.GetRun(ctx, f.ws, f.user, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.svc.Control(ctx, f.ws, f.user, run.ID, "archive", GraphControlParams{ExpectedRevision: run.Revision, IdempotencyKey: uuid.NewString()}); err != nil {
		t.Fatal(err)
	}
	var state string
	if err = f.pool.QueryRow(ctx, `SELECT status FROM issue WHERE id=$1`, f.issue).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "todo" {
		t.Fatalf("historical run overwrote ordinary root: %s", state)
	}
	current := f.start(t)
	run, err = f.svc.GetRun(ctx, f.ws, f.user, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.svc.Control(ctx, f.ws, f.user, run.ID, "archive", GraphControlParams{ExpectedRevision: run.Revision, IdempotencyKey: uuid.NewString()}); err != nil {
		t.Fatalf("old run archive conflicted with new root owner: %v", err)
	}
	if err = f.pool.QueryRow(ctx, `SELECT status FROM issue WHERE id=$1`, f.issue).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != graphRunIssueStatus(current.State) {
		t.Fatalf("historical run overwrote newer root: %s", state)
	}
}

func TestGraphPostgresQueueTimeoutAndUnclaimedCancellation(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	run := f.reconcile(t, f.start(t))
	taskID := run.Nodes[1].Attempts[0].TaskID
	if _, err := f.pool.Exec(ctx, `UPDATE agent_task_queue SET created_at=now()-interval '31 minutes' WHERE id=$1`, taskID); err != nil {
		t.Fatal(err)
	}
	run = f.reconcile(t, run)
	if run.Nodes[1].BlockReason != "queue_timeout" || run.Nodes[1].Attempts[0].StoppedAt == nil {
		t.Fatal("unclaimed timeout lost non-execution proof")
	}
	var err error
	run, err = f.svc.Control(ctx, f.ws, f.user, run.ID, "retry", GraphControlParams{ExpectedRevision: run.Revision, NodeRunID: run.Nodes[1].ID, IdempotencyKey: uuid.NewString()})
	if err != nil {
		t.Fatalf("unclaimed timeout cannot retry: %v", err)
	}
	run = f.reconcile(t, run)
	if len(run.Nodes[1].Attempts) != 2 {
		t.Fatal("explicit retry did not create one new attempt")
	}
	taskID = run.Nodes[1].Attempts[1].TaskID
	if _, err = f.tasks.CancelTask(ctx, util.MustParseUUID(taskID)); err != nil {
		t.Fatal(err)
	}
	run = f.reconcile(t, run)
	if run.Nodes[1].Attempts[1].StoppedAt == nil {
		t.Fatal("direct cancellation of unclaimed task lost non-execution proof")
	}
	run, err = f.svc.Control(ctx, f.ws, f.user, run.ID, "cancel", GraphControlParams{ExpectedRevision: run.Revision, IdempotencyKey: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	run = f.reconcile(t, run)
	if run.State != "cancelled" {
		t.Fatal("physically stopped attempts blocked cancellation")
	}
}

func TestGraphPostgresExpiredApprovalCannotSucceed(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	run := f.reconcile(t, f.start(t))
	f.complete(t, run, 1)
	run = f.reconcile(t, run)
	f.complete(t, run, 2)
	run = f.reconcile(t, run)
	if run.Approval == nil {
		t.Fatal("approval missing")
	}
	past := time.Now().Add(-time.Minute)
	if _, err := f.pool.Exec(ctx, `UPDATE graph_run SET document=jsonb_set(document,'{deadline}',to_jsonb($2::timestamptz)) WHERE id=$1`, run.ID, past); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Approve(ctx, f.ws, f.user, run.ID, GraphApprovalParams{ExpectedRevision: run.Revision, ApprovalID: run.Approval.ID, CandidateDigest: run.Approval.CandidateDigest, Decision: "approved", IdempotencyKey: uuid.NewString()}); err == nil {
		t.Fatal("expired run accepted approval")
	}
	run = f.reconcile(t, run)
	if run.State != "failed" || run.TerminationReason != "deadline" {
		t.Fatalf("deadline did not settle: %s %s", run.State, run.TerminationReason)
	}
}
