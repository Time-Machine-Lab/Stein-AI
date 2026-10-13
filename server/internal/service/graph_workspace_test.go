package service

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/graph"
)

func TestGraphPostgresWorkspaceTeardownRequiresStoppedRuns(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	run := f.start(t)
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = PrepareGraphWorkspaceDeleteTx(ctx, tx, f.ws); err == nil {
		t.Fatal("active Graph allowed workspace deletion")
	}
	_ = tx.Rollback(ctx)
	run, err = f.svc.Control(ctx, f.ws, f.user, run.ID, "cancel", GraphControlParams{ExpectedRevision: run.Revision, IdempotencyKey: "workspace-cancel"})
	if err != nil {
		t.Fatal(err)
	}
	run = f.reconcile(t, run)
	if run.State != string(graph.RunCancelled) {
		t.Fatalf("queued run did not cancel: %s", run.State)
	}
	tx, err = f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = PrepareGraphWorkspaceDeleteTx(ctx, tx, f.ws); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM graph_run WHERE workspace_id=$1`, f.ws).Scan(&count); err != nil || count != 0 {
		t.Fatalf("Graph cleanup: count=%d err=%v", count, err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM issue WHERE workspace_id=$1`, f.ws); err != nil {
		t.Fatalf("Graph ownership still prevents teardown: %v", err)
	}
}
