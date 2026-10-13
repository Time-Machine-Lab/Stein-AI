package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/graph"
)

func TestGraphPostgresAttachmentAcceptanceSerializesDeletion(t *testing.T) {
	f := newGraphFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	run := f.reconcile(t, f.start(t))
	attempt := run.Nodes[1].Attempts[0]
	artifact := graph.Artifact{ID: uuid.NewString(), Type: graph.ArtifactAttachmentRef, ProducerTaskID: attempt.TaskID, ContentHash: strings.Repeat("a", 64), MediaType: "text/plain", Size: 4}
	if _, err := f.pool.Exec(ctx, `INSERT INTO attachment(id,workspace_id,task_id) VALUES($1,$2,$3)`, artifact.ID, f.ws, attempt.TaskID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO graph_attachment_upload(attachment_id,workspace_id,task_id,content_hash,size_bytes,media_type) VALUES($1,$2,$3,$4,$5,$6)`, artifact.ID, f.ws, attempt.TaskID, artifact.ContentHash, artifact.Size, artifact.MediaType); err != nil {
		t.Fatal(err)
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err = ValidateGraphAttachmentTx(ctx, tx, f.ws, attempt.TaskID, artifact); err != nil {
		t.Fatal(err)
	}
	deleteConn, err := f.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer deleteConn.Release()
	deleted := make(chan error, 1)
	go func() { _, e := deleteConn.Exec(ctx, `DELETE FROM attachment WHERE id=$1`, artifact.ID); deleted <- e }()
	// Wait for the actual PostgreSQL lock conflict, not scheduler timing.
	for {
		var blocked bool
		if err = f.pool.QueryRow(ctx, `SELECT $1::int = ANY(pg_blocking_pids($2))`, int32(tx.Conn().PgConn().PID()), int32(deleteConn.Conn().PgConn().PID())).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case e := <-deleted:
			t.Fatalf("delete completed before artifact retention: %v", e)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err = graphRetainArtifacts(ctx, tx, &run, run.Nodes[1], graph.Result{Artifacts: []graph.Artifact{artifact}}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-deleted; err == nil {
		t.Fatal("delete removed the accepted artifact after waiting")
	}
	var exists bool
	if err = f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM attachment WHERE id=$1)`, artifact.ID).Scan(&exists); err != nil || !exists {
		t.Fatalf("accepted bytes missing: %v", err)
	}
}

func TestGraphPostgresDraftUpdateReplaysCommittedResponse(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	draft, err := f.svc.GetDefinition(ctx, f.ws, f.user, f.version.GraphID)
	if err != nil {
		t.Fatal(err)
	}
	updated := draft.Definition
	updated.Name = "Saved once"
	first, err := f.svc.UpdateDefinition(ctx, f.ws, f.user, draft.ID, updated, draft.Revision, "save-once")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := f.svc.UpdateDefinition(ctx, f.ws, f.user, draft.ID, updated, draft.Revision, "save-once")
	if err != nil || replay.Revision != first.Revision {
		t.Fatalf("committed save could not replay: %v", err)
	}
	updated.Name = "Different request"
	if _, err = f.svc.UpdateDefinition(ctx, f.ws, f.user, draft.ID, updated, draft.Revision, "save-once"); err == nil {
		t.Fatal("reused key accepted a different update")
	}
}
