package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func graphLegacyTaskParams(f graphFixture, agentID string) db.CreateAgentTaskParams {
	return db.CreateAgentTaskParams{
		AgentID: util.MustParseUUID(agentID), RuntimeID: util.MustParseUUID(f.runtime),
		IssueID: util.MustParseUUID(f.issue), CoalescedCommentIds: []pgtype.UUID{},
	}
}

func TestGraphPostgresLegacyConcurrentEnqueueDoesNotUpgradeKeyShare(t *testing.T) {
	f := newGraphFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	otherAgent := uuid.NewString()
	if _, err := f.pool.Exec(ctx, `INSERT INTO agent(id,workspace_id,runtime_id) VALUES($1,$2,$3)`, otherAgent, f.ws, f.runtime); err != nil {
		t.Fatal(err)
	}
	var transactions []pgx.Tx
	for _, agentID := range []string{f.agent, otherAgent} {
		tx, err := f.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(context.Background())
		transactions = append(transactions, tx)
		// Every production enqueue holds this KEY SHARE fence before its row
		// trigger runs. Acquiring it in both transactions makes the race exact.
		var locked bool
		if err = tx.QueryRow(ctx, `SELECT lock_task_owner_rows($1,$2,$3)`, agentID, f.issue, f.runtime).Scan(&locked); err != nil || !locked {
			t.Fatalf("legacy ownership fence: locked=%v err=%v", locked, err)
		}
	}
	errors := make(chan error, 2)
	for i, agentID := range []string{f.agent, otherAgent} {
		go func(tx pgx.Tx, agentID string) {
			_, err := db.New(tx).CreateAgentTask(ctx, graphLegacyTaskParams(f, agentID))
			if err == nil {
				err = tx.Commit(ctx)
			} else {
				_ = tx.Rollback(context.Background())
			}
			errors <- err
		}(transactions[i], agentID)
	}
	for range transactions {
		if err := <-errors; err != nil {
			t.Errorf("ordinary concurrent enqueue failed: %v", err)
		}
	}
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, f.issue).Scan(&count); err != nil || count != 2 {
		t.Fatalf("ordinary enqueue count=%d, err=%v", count, err)
	}
}

func TestGraphPostgresLegacyTaskLifecycleAndIssueWrites(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	q := db.New(f.pool)
	// Kill switches and published Graph definitions must not gate ordinary work.
	f.svc.NewRunsEnabled = false
	f.svc.AdvanceEnabled = false
	task, err := q.CreateAgentTask(ctx, graphLegacyTaskParams(f, f.agent))
	if err != nil {
		t.Fatal(err)
	}
	if managed, err := f.svc.IsTask(ctx, util.UUIDToString(task.ID)); err != nil || managed {
		t.Fatalf("ordinary task classified as Graph: %v, %v", managed, err)
	}
	commentID := util.MustParseUUID(uuid.NewString())
	if _, err = q.MergeCommentIntoPendingTask(ctx, db.MergeCommentIntoPendingTaskParams{
		IssueID: task.IssueID, AgentID: task.AgentID, NewTriggerCommentID: commentID,
	}); err != nil {
		t.Fatalf("ordinary comment merge: %v", err)
	}
	claimed, err := q.ClaimAgentTask(ctx, db.ClaimAgentTaskParams{AgentID: task.AgentID, PrepareLeaseSecs: 120})
	if err != nil || claimed.ID != task.ID {
		t.Fatalf("ordinary claim: %v", err)
	}
	if err = f.tasks.graphClaimTransaction(ctx, task.ID, func(qtx *db.Queries) error {
		_, e := qtx.GetAgentTask(ctx, task.ID)
		return e
	}); err != nil {
		t.Fatalf("ordinary claim admission: %v", err)
	}
	if _, err = f.tasks.StartTask(ctx, task.ID); err != nil {
		t.Fatalf("ordinary start without Graph executor: %v", err)
	}
	if _, err = q.RegisterPlannedCommentForActiveTask(ctx, db.RegisterPlannedCommentForActiveTaskParams{
		IssueID: task.IssueID, AgentID: task.AgentID, CommentID: util.MustParseUUID(uuid.NewString()),
	}); err != nil {
		t.Fatalf("ordinary active comment registration: %v", err)
	}
	if _, err = f.tasks.FailTask(ctx, task.ID, "connection reset", "ordinary-session", "ordinary-workdir", "", "provider_network", false, ""); err != nil {
		t.Fatalf("ordinary terminal failure: %v", err)
	}
	retry, err := q.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: task.ID})
	if err != nil || retry.Status != "queued" || retry.SessionID.String != "ordinary-session" {
		t.Fatalf("ordinary retry/resume: task=%+v err=%v", retry, err)
	}
	if _, err = q.ClaimAgentTask(ctx, db.ClaimAgentTaskParams{AgentID: retry.AgentID, PrepareLeaseSecs: 120}); err != nil {
		t.Fatalf("ordinary retry claim: %v", err)
	}
	if _, err = f.tasks.StartTask(ctx, retry.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.tasks.CompleteTask(ctx, retry.ID, []byte(`{}`), "resumed-session", "ordinary-workdir", "", false, ""); err != nil {
		t.Fatalf("ordinary completion: %v", err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE issue SET status='done',description='ordinary edit' WHERE id=$1`, f.issue); err != nil {
		t.Fatalf("ordinary Issue edit: %v", err)
	}
	childID, attachmentID := uuid.NewString(), uuid.NewString()
	if _, err = f.pool.Exec(ctx, `INSERT INTO issue(id,workspace_id,parent_issue_id,title,status) VALUES($1,$2,$3,'ordinary child','todo')`, childID, f.ws, f.issue); err != nil {
		t.Fatalf("ordinary child creation: %v", err)
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO attachment(id,workspace_id,issue_id) VALUES($1,$2,$3)`, attachmentID, f.ws, f.issue); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `DELETE FROM attachment WHERE id=$1`, attachmentID); err != nil {
		t.Fatalf("ordinary attachment deletion: %v", err)
	}
	if _, err = f.pool.Exec(ctx, `DELETE FROM issue WHERE id=$1`, childID); err != nil {
		t.Fatalf("ordinary child deletion: %v", err)
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = PrepareGraphWorkspaceDeleteTx(ctx, tx, f.ws); err != nil {
		t.Fatalf("ordinary workspace teardown with definitions but no runs: %v", err)
	}
}
