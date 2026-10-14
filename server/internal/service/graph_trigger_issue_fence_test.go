package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestGraphTriggerPostgresIssueEventCannotCrossWorkspaceDeletion(t *testing.T) {
	f := graphTriggerFixture(t)
	ctx := context.Background()
	project := uuid.NewString()
	if _, err := f.pool.Exec(ctx, `INSERT INTO project(id,workspace_id,title) VALUES($1,$2,'Events')`, project, f.ws); err != nil {
		t.Fatal(err)
	}
	p := graphTestTrigger(f)
	p.Kind = "task_event"
	p.Config = GraphTriggerConfig{ProjectID: project, Events: []string{"status_changed"}}
	if _, err := f.svc.SaveTrigger(ctx, f.ws, f.user, f.version.GraphID, "", p); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE issue SET project_id=$2 WHERE id=$1`, f.issue, project); err != nil {
		t.Fatal(err)
	}
	deleting, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer deleting.Rollback(ctx)
	if _, err = deleting.Exec(ctx, `SELECT id FROM workspace WHERE id=$1 FOR UPDATE`, f.ws); err != nil {
		t.Fatal(err)
	}
	if _, err = deleting.Exec(ctx, `DELETE FROM graph_trigger_event WHERE workspace_id=$1`, f.ws); err != nil {
		t.Fatal(err)
	}
	writeCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	_, err = f.pool.Exec(writeCtx, `UPDATE issue SET status='in_progress' WHERE id=$1`, f.issue)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("issue update must fail promptly rather than insert after deletion sweep or deadlock: %v", err)
	}
	if err = deleting.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE issue SET status='in_progress' WHERE id=$1`, f.issue); err != nil {
		t.Fatalf("retry after deletion releases fence: %v", err)
	}
	var count int
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM graph_trigger_event WHERE workspace_id=$1`, f.ws).Scan(&count); err != nil || count != 1 {
		t.Fatalf("retry event count=%d error=%v", count, err)
	}
}
