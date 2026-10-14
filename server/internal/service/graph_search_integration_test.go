package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/graph"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestGraphSearchPostgresPaginationAndWorkspaceBoundary(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	for _, name := range []string{"Search Alpha", "Search Beta", "Other"} {
		d := f.version.Definition
		d.Name = name
		if _, err := f.svc.CreateDefinition(ctx, f.ws, f.user, d, uuid.NewString()); err != nil {
			t.Fatal(err)
		}
	}
	first, err := f.svc.SearchDefinitions(ctx, f.ws, f.user, GraphSearchParams{Query: "Search", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 2 || len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatalf("bad first page %+v", first)
	}
	second, err := f.svc.SearchDefinitions(ctx, f.ws, f.user, GraphSearchParams{Query: "Search", Limit: 1, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if second.Total != 2 || len(second.Items) != 1 || second.Items[0].ID == first.Items[0].ID || second.NextCursor != "" {
		t.Fatalf("bad second page %+v", second)
	}
	if _, err = f.svc.SearchDefinitions(ctx, f.ws, uuid.NewString(), GraphSearchParams{}); err == nil {
		t.Fatal("nonmember read definitions")
	}
	if _, err = f.svc.SearchDefinitions(ctx, f.ws, f.user, GraphSearchParams{Cursor: "malformed"}); err == nil {
		t.Fatal("malformed cursor accepted")
	}
}

func TestGraphSearchPostgresRunStatsIncludeAllAttempts(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, graphTestTable("task_usage", db.TaskUsage{})); err != nil {
		t.Fatal(err)
	}
	run, err := f.svc.StartRun(ctx, f.ws, f.user, GraphStartParams{VersionID: f.version.ID, RootIssueID: f.issue, Input: []byte(`{"goal":"Search test"}`), RuntimeBindings: map[string]string{"implement": f.runtime, "verify": f.runtime}, IdempotencyKey: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	run = f.reconcile(t, run)
	var taskID string
	for _, n := range run.Nodes {
		if len(n.Attempts) > 0 {
			taskID = n.Attempts[0].TaskID
			break
		}
	}
	if taskID == "" {
		t.Fatal("no attempt queued")
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO task_usage(task_id,provider,model,input_tokens,output_tokens,cost_usd_ticks) VALUES($1,'test','model',100,20,2500000000)`, taskID); err != nil {
		t.Fatal(err)
	}
	result, err := f.svc.SearchRuns(ctx, f.ws, f.user, GraphSearchParams{GraphID: f.version.GraphID, State: string(graph.RunActive)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || len(result.Items) != 1 || result.Stats.Active != 1 || result.Stats.TotalTokens != 120 || result.Stats.TotalCostUSD != 0.25 {
		t.Fatalf("bad stats %+v", result)
	}
	if result.Items[0].GraphID != f.version.GraphID || result.Items[0].GraphName != f.version.Definition.Name {
		t.Fatal("V1 list did not resolve frozen graph identity")
	}
	if len(result.Items[0].Nodes) > 0 || len(result.Items[0].Input) > 0 || result.Items[0].Definition != nil || len(result.Items[0].RuntimeBindings) > 0 {
		t.Fatal("run search transmitted execution snapshots instead of list summaries")
	}
}
