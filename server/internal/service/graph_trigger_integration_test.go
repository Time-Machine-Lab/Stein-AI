package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func graphTriggerFixture(t *testing.T) graphFixture {
	t.Helper()
	f := newGraphFixture(t)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, graphTestTable("project", db.Project{})); err != nil {
		t.Fatal(err)
	}
	return f
}
func graphTestTrigger(f graphFixture) GraphTriggerWrite {
	return GraphTriggerWrite{Name: "Webhook acceptance", VersionID: f.version.ID, Kind: "webhook", Enabled: true, Config: GraphTriggerConfig{RootTitle: "Triggered work"}, Input: json.RawMessage(`{"goal":"Verify trigger execution"}`), RuntimeBindings: map[string]string{"implement": f.runtime, "verify": f.runtime}, IdempotencyKey: uuid.NewString()}
}

func TestGraphTriggerPostgresWebhookDedupAndSecretRotation(t *testing.T) {
	f := graphTriggerFixture(t)
	ctx := context.Background()
	trigger, err := f.svc.SaveTrigger(ctx, f.ws, f.user, f.version.GraphID, "", graphTestTrigger(f))
	if err != nil {
		t.Fatal(err)
	}
	if trigger.Secret == "" {
		t.Fatal("creation did not reveal secret")
	}
	event, err := f.svc.InvokeTrigger(ctx, trigger.ID, trigger.Secret, "external-event", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := f.svc.InvokeTrigger(ctx, trigger.ID, trigger.Secret, "external-event", json.RawMessage(`{}`))
	if err != nil || event.ID != replayed.ID {
		t.Fatalf("dedup failed %+v %v", replayed, err)
	}
	if _, err = f.svc.InvokeTrigger(ctx, trigger.ID, trigger.Secret, "external-event", json.RawMessage(`{"goal":"different"}`)); err == nil {
		t.Fatal("idempotency key reuse accepted")
	}
	rotated, err := f.svc.RotateTriggerSecret(ctx, f.ws, f.user, trigger.ID, GraphControlParams{ExpectedRevision: trigger.Revision, IdempotencyKey: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.svc.InvokeTrigger(ctx, trigger.ID, trigger.Secret, "old-secret", json.RawMessage(`{}`)); err == nil {
		t.Fatal("rotated secret remained valid")
	}
	if _, err = f.svc.InvokeTrigger(ctx, trigger.ID, rotated.Secret, "new-secret", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err = f.svc.ReconcileTriggersOnce(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM graph_run WHERE workspace_id=$1`, f.ws).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("wanted2 independent runs, got%d", count)
	}
	if err = f.svc.ReconcileTriggersOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM graph_run WHERE workspace_id=$1`, f.ws).Scan(&count); err != nil || count != 2 {
		t.Fatalf("restart duplicated runs %d %v", count, err)
	}
}

func TestGraphTriggerPostgresTaskEventsCommitAndIsolation(t *testing.T) {
	f := graphTriggerFixture(t)
	ctx := context.Background()
	projectID := uuid.NewString()
	if _, err := f.pool.Exec(ctx, `INSERT INTO project(id,workspace_id,title) VALUES($1,$2,'Trigger project')`, projectID, f.ws); err != nil {
		t.Fatal(err)
	}
	p := graphTestTrigger(f)
	p.Kind = "task_event"
	p.Config = GraphTriggerConfig{ProjectID: projectID, Events: []string{"created"}, Statuses: []string{"todo"}}
	_, err := f.svc.SaveTrigger(ctx, f.ws, f.user, f.version.GraphID, "", p)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO issue(id,workspace_id,project_id,title,status) VALUES($1,$2,$3,'Rolled back','todo')`, uuid.NewString(), f.ws, projectID); err != nil {
		t.Fatal(err)
	}
	tx.Rollback(ctx)
	var count int
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM graph_trigger_event`).Scan(&count); err != nil || count != 0 {
		t.Fatal("rolled-back event escaped transaction")
	}
	issueID := uuid.NewString()
	if _, err = f.pool.Exec(ctx, `INSERT INTO issue(id,workspace_id,project_id,title,status,priority,creator_type,creator_id,number) VALUES($1,$2,$3,'Actual event','todo','none','member',$4,10)`, issueID, f.ws, projectID, f.user); err != nil {
		t.Fatal(err)
	}
	if err = f.svc.ReconcileTriggersOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM graph_trigger_event`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("Graph children recursively triggered: %d %v", count, err)
	}
}

func TestGraphTriggerPostgresScheduleRecoversOneMissedSlot(t *testing.T) {
	f := graphTriggerFixture(t)
	ctx := context.Background()
	p := graphTestTrigger(f)
	p.Kind = "schedule"
	p.Config.Cron = "*/5 * * * *"
	p.Config.Timezone = "Asia/Singapore"
	trigger, err := f.svc.SaveTrigger(ctx, f.ws, f.user, f.version.GraphID, "", p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE graph_trigger SET next_fire_at=now()-interval '1 day' WHERE id=$1`, trigger.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.svc.ReconcileTriggersOnce(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	var next time.Time
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM graph_trigger_event`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("unbounded catchup %d %v", count, err)
	}
	if err = f.pool.QueryRow(ctx, `SELECT next_fire_at FROM graph_trigger WHERE id=$1`, trigger.ID).Scan(&next); err != nil || !next.After(time.Now()) {
		t.Fatalf("schedule not advanced %v %v", next, err)
	}
}

func TestGraphTriggerPostgresCrashAfterStartRemainsRecordedAfterDisable(t *testing.T) {
	f := graphTriggerFixture(t)
	ctx := context.Background()
	p := graphTestTrigger(f)
	trigger, err := f.svc.SaveTrigger(ctx, f.ws, f.user, f.version.GraphID, "", p)
	if err != nil {
		t.Fatal(err)
	}
	event, err := f.svc.InvokeTrigger(ctx, trigger.ID, trigger.Secret, "crash-test", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := f.svc.claimGraphTriggerEvent(ctx, event.ID)
	if err != nil || !claimed {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	var rootID string
	if err = f.pool.QueryRow(ctx, `SELECT root_issue_id FROM graph_trigger_event WHERE id=$1`, event.ID).Scan(&rootID); err != nil {
		t.Fatal(err)
	}
	run, err := f.svc.StartRun(ctx, f.ws, f.user, GraphStartParams{VersionID: trigger.VersionID, RootIssueID: rootID, Input: trigger.Input, RuntimeBindings: trigger.RuntimeBindings, IdempotencyKey: "trigger-event:" + event.ID})
	if err != nil {
		t.Fatal(err)
	}
	// The process dies after StartRun commits, before recording the event result.
	p.Enabled = false
	p.ExpectedRevision = trigger.Revision
	p.IdempotencyKey = uuid.NewString()
	if _, err = f.svc.SaveTrigger(ctx, f.ws, f.user, f.version.GraphID, trigger.ID, p); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE graph_trigger_event SET lease_until=now()-interval '1 second' WHERE id=$1`, event.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.svc.ReconcileTriggersOnce(ctx); err != nil {
		t.Fatal(err)
	}
	var state, recordedRun string
	if err = f.pool.QueryRow(ctx, `SELECT state,COALESCE(run_id::text,'') FROM graph_trigger_event WHERE id=$1`, event.ID).Scan(&state, &recordedRun); err != nil {
		t.Fatal(err)
	}
	if state != "started" || recordedRun != run.ID {
		t.Fatalf("committed run lost after disable: %s %s", state, recordedRun)
	}
}
