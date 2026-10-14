package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestGraphTriggerPostgresRevokedCreatorCannotAllocateRoot(t *testing.T) {
	f := graphTriggerFixture(t)
	trigger, err := f.svc.SaveTrigger(context.Background(), f.ws, f.user, f.version.GraphID, "", graphTestTrigger(f))
	if err != nil {
		t.Fatal(err)
	}
	event, err := f.svc.InvokeTrigger(context.Background(), trigger.ID, trigger.Secret, "revoked", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(context.Background(), `DELETE FROM member WHERE id=$1`, f.member); err != nil {
		t.Fatal(err)
	}
	if err = f.svc.ReconcileTriggersOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	var state string
	var rootExists bool
	if err = f.pool.QueryRow(context.Background(), `SELECT state,EXISTS(SELECT 1 FROM issue WHERE id=e.root_issue_id) FROM graph_trigger_event e WHERE id=$1`, event.ID).Scan(&state, &rootExists); err != nil {
		t.Fatal(err)
	}
	if state != "failed" || rootExists {
		t.Fatalf("revoked trigger principal created a root issue: %s %v", state, rootExists)
	}
}

func TestGraphTriggerPostgresIngressWaitsForWorkspaceDeletionFence(t *testing.T) {
	f := graphTriggerFixture(t)
	trigger, err := f.svc.SaveTrigger(context.Background(), f.ws, f.user, f.version.GraphID, "", graphTestTrigger(f))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(context.Background(), `SELECT id FROM workspace WHERE id=$1 FOR UPDATE`, f.ws); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := f.svc.InvokeTrigger(ctx, trigger.ID, trigger.Secret, "fenced", json.RawMessage(`{}`))
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("ingress bypassed workspace deletion fence: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err = tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestGraphTriggerPostgresDisabledPendingEventDoesNotCreateRoot(t *testing.T) {
	f := graphTriggerFixture(t)
	request := graphTestTrigger(f)
	trigger, err := f.svc.SaveTrigger(context.Background(), f.ws, f.user, f.version.GraphID, "", request)
	if err != nil {
		t.Fatal(err)
	}
	event, err := f.svc.InvokeTrigger(context.Background(), trigger.ID, trigger.Secret, "disabled-before-claim", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Enabled = false
	request.ExpectedRevision = trigger.Revision
	request.IdempotencyKey = uuid.NewString()
	if _, err = f.svc.SaveTrigger(context.Background(), f.ws, f.user, f.version.GraphID, trigger.ID, request); err != nil {
		t.Fatal(err)
	}
	if err = f.svc.ReconcileTriggersOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	var exists bool
	var state string
	if err = f.pool.QueryRow(context.Background(), `SELECT e.state,EXISTS(SELECT 1 FROM issue i WHERE i.id=e.root_issue_id) FROM graph_trigger_event e WHERE e.id=$1`, event.ID).Scan(&state, &exists); err != nil {
		t.Fatal(err)
	}
	if state != "skipped" || exists {
		t.Fatalf("disabled event left an orphan root task: state=%s root=%v", state, exists)
	}
}

func TestGraphTriggerPostgresConcurrentCoordinatorsCreateOneRun(t *testing.T) {
	f := graphTriggerFixture(t)
	trigger, err := f.svc.SaveTrigger(context.Background(), f.ws, f.user, f.version.GraphID, "", graphTestTrigger(f))
	if err != nil {
		t.Fatal(err)
	}
	event, err := f.svc.InvokeTrigger(context.Background(), trigger.ID, trigger.Secret, "concurrent", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for range 6 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- f.svc.ReconcileTriggersOnce(ctx) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var state string
	if err = f.pool.QueryRow(ctx, `SELECT e.state,(SELECT count(*) FROM graph_run r WHERE r.root_issue_id=e.root_issue_id) FROM graph_trigger_event e WHERE e.id=$1`, event.ID).Scan(&state, &count); err != nil {
		t.Fatal(err)
	}
	if count != 1 || state != "started" {
		t.Fatalf("concurrent trigger dispatch duplicated or lost run: state=%s count=%d", state, count)
	}
}
