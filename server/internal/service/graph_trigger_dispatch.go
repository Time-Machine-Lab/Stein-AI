package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *GraphService) RunTriggerCoordinator(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.ReconcileTriggersOnce(ctx); err != nil && ctx.Err() == nil {
			slog.Error("Graph trigger dispatch failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ReconcileTriggersOnce uses database time, durable event keys and bounded batches.
// A restart catches up one missed schedule slot, then resumes its future cadence.
// It never replays a day's worth of missed external/model work in a burst.
func (s *GraphService) ReconcileTriggersOnce(ctx context.Context) error {
	if !s.NewRunsEnabled {
		return nil
	}
	if err := s.enqueueGraphSchedules(ctx); err != nil {
		return err
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id FROM graph_trigger_event WHERE (state='pending' OR (state='starting' AND lease_until<now())) AND available_at<=now() ORDER BY available_at,id LIMIT 20`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	var joined error
	for _, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		claim, err := s.claimGraphTriggerEvent(ctx, id)
		if err != nil {
			joined = errors.Join(joined, err)
			continue
		}
		if !claim {
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = s.dispatchGraphTriggerEvent(callCtx, id)
		cancel()
		joined = errors.Join(joined, err)
	}
	return joined
}

func (s *GraphService) enqueueGraphSchedules(ctx context.Context) error {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id FROM graph_trigger WHERE enabled AND kind='schedule' AND deleted_at IS NULL AND next_fire_at<=now() ORDER BY next_fire_at,id LIMIT 20`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	var joined error
	for _, id := range ids {
		joined = errors.Join(joined, s.enqueueGraphSchedule(ctx, id))
	}
	return joined
}

func (s *GraphService) enqueueGraphSchedule(ctx context.Context, id string) error {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = graphLockTriggerWorkspace(ctx, tx, id); errors.Is(err, pgx.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	var raw []byte
	var due, now time.Time
	err = tx.QueryRow(ctx, `SELECT document,next_fire_at,now() FROM graph_trigger WHERE id=$1 AND enabled AND kind='schedule' AND deleted_at IS NULL AND next_fire_at<=now() FOR UPDATE SKIP LOCKED`, id).Scan(&raw, &due, &now)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var trigger GraphTrigger
	if err = json.Unmarshal(raw, &trigger); err != nil {
		return err
	}
	next, err := NextOccurrenceAfterUTC(trigger.Config.Cron, trigger.Config.Timezone, now)
	if err != nil || next.IsZero() {
		if _, err = tx.Exec(ctx, `UPDATE graph_trigger SET enabled=false,document=jsonb_set(document,'{enabled}','false'),last_error='Schedule has no future occurrence',updated_at=now() WHERE id=$1`, id); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if _, err = graphEnqueueTrigger(ctx, tx, trigger, "schedule:"+due.UTC().Format(time.RFC3339Nano), trigger.Input, ""); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE graph_trigger SET next_fire_at=$2,updated_at=now() WHERE id=$1`, id, next); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *GraphService) claimGraphTriggerEvent(ctx context.Context, id string) (bool, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var triggerID string
	if err = tx.QueryRow(ctx, `SELECT trigger_id FROM graph_trigger_event WHERE id=$1`, id).Scan(&triggerID); errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if err = graphLockTriggerWorkspace(ctx, tx, triggerID); errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	var enabled bool
	var deleted *time.Time
	if err = tx.QueryRow(ctx, `SELECT enabled,deleted_at FROM graph_trigger WHERE id=$1 FOR UPDATE`, triggerID).Scan(&enabled, &deleted); errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	var raw []byte
	var rootID, state string
	var lease *time.Time
	var now time.Time
	var attempts int
	err = tx.QueryRow(ctx, `SELECT snapshot,root_issue_id,state,lease_until,now(),attempts FROM graph_trigger_event WHERE id=$1 AND available_at<=now() FOR UPDATE SKIP LOCKED`, id).Scan(&raw, &rootID, &state, &lease, &now, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if state != "pending" && (state != "starting" || lease == nil || lease.After(now)) {
		return false, nil
	}
	var trigger GraphTrigger
	if err = json.Unmarshal(raw, &trigger); err != nil {
		return false, err
	}
	// Recover a committed start before applying disablement or the retry budget.
	// A crash between StartRun and event acknowledgement never loses the run link.
	committedRun, err := graphCommittedTriggerRun(ctx, tx, trigger, id, rootID)
	if err != nil {
		return false, err
	}
	if committedRun != "" {
		if _, err = tx.Exec(ctx, `UPDATE graph_trigger_event SET state='started',run_id=$2,error='',lease_until=NULL,updated_at=now() WHERE id=$1`, id, committedRun); err != nil {
			return false, err
		}
		return false, tx.Commit(ctx)
	}
	if !enabled || deleted != nil || attempts >= 10 {
		state, message := "skipped", "Trigger disabled before dispatch"
		if enabled && deleted == nil {
			state, message = "failed", "Trigger dispatch attempt budget exhausted"
		}
		if _, err = tx.Exec(ctx, `UPDATE graph_trigger_event SET state=$2,error=$3,lease_until=NULL,updated_at=now() WHERE id=$1`, id, state, message); err != nil {
			return false, err
		}
		return false, tx.Commit(ctx)
	}
	if _, _, err = graphMember(ctx, tx, trigger.WorkspaceID, trigger.CreatorID); err != nil {
		var ge *GraphError
		if !errors.As(err, &ge) || ge.Code != "forbidden" {
			return false, err
		}
		if _, err = tx.Exec(ctx, `UPDATE graph_trigger_event SET state='failed',error='Trigger creator no longer belongs to this workspace',lease_until=NULL,updated_at=now() WHERE id=$1`, id); err != nil {
			return false, err
		}
		return false, tx.Commit(ctx)
	}
	if trigger.Kind != "task_event" {
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM issue WHERE id=$1 AND workspace_id=$2)`, rootID, trigger.WorkspaceID).Scan(&exists); err != nil {
			return false, err
		}
		if !exists {
			// Commit the root reservation before calling StartRun: both operations
			// allocate workspace issue numbers and must not hold nested row locks.
			if _, err = tx.Exec(ctx, `SELECT set_config('multica.graph_trigger_event_id',$1,true)`, id); err != nil {
				return false, err
			}
			var number int64
			if err = tx.QueryRow(ctx, `UPDATE workspace SET issue_counter=issue_counter+1 WHERE id=$1 RETURNING issue_counter`, trigger.WorkspaceID).Scan(&number); err != nil {
				return false, err
			}
			var project any
			if trigger.Config.ProjectID != "" {
				project = trigger.Config.ProjectID
			}
			if _, err = tx.Exec(ctx, `INSERT INTO issue(id,workspace_id,title,description,status,priority,creator_type,creator_id,project_id,number,position) VALUES($1,$2,$3,$4,'todo','none','member',$5,$6,$7,0)`, rootID, trigger.WorkspaceID, trigger.Config.RootTitle, trigger.Config.RootDescription, trigger.CreatorID, project, number); err != nil {
				return false, err
			}
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE graph_trigger_event SET state='starting',attempts=attempts+1,lease_until=now()+interval '2 minutes',updated_at=now() WHERE id=$1`, id); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func graphCommittedTriggerRun(ctx context.Context, tx pgx.Tx, trigger GraphTrigger, eventID, rootID string) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT r.id FROM graph_command c JOIN graph_run r ON r.id::text=c.response->>'id' AND r.workspace_id=c.workspace_id WHERE c.workspace_id=$1 AND c.command_key=$2 AND r.root_issue_id=$3 AND r.version_id=$4`, trigger.WorkspaceID, "trigger-event:"+eventID, rootID, trigger.VersionID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (s *GraphService) dispatchGraphTriggerEvent(ctx context.Context, id string) error {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var triggerID string
	if err = tx.QueryRow(ctx, `SELECT trigger_id FROM graph_trigger_event WHERE id=$1`, id).Scan(&triggerID); err != nil {
		return err
	}
	if err = graphLockTriggerWorkspace(ctx, tx, triggerID); err != nil {
		return err
	}
	// Serialize with disable/delete while StartRun commits its idempotent start.
	// No issue/workspace write locks are held in this transaction.
	var enabled bool
	var deleted *time.Time
	if err = tx.QueryRow(ctx, `SELECT enabled,deleted_at FROM graph_trigger WHERE id=$1 FOR UPDATE`, triggerID).Scan(&enabled, &deleted); err != nil {
		return err
	}
	var snapshot, input []byte
	var rootID, state string
	var attempts int
	if err = tx.QueryRow(ctx, `SELECT snapshot,input,root_issue_id,state,attempts FROM graph_trigger_event WHERE id=$1 FOR UPDATE`, id).Scan(&snapshot, &input, &rootID, &state, &attempts); err != nil {
		return err
	}
	if state != "starting" {
		return nil
	}
	var trigger GraphTrigger
	if err = json.Unmarshal(snapshot, &trigger); err != nil {
		return err
	}
	// A previous worker may have committed StartRun and died before this event
	// acknowledgement. Recover that fact even if the trigger was since disabled.
	committedRun, err := graphCommittedTriggerRun(ctx, tx, trigger, id, rootID)
	if err != nil {
		return err
	}
	if committedRun != "" {
		if _, err = tx.Exec(ctx, `UPDATE graph_trigger_event SET state='started',run_id=$2,error='',lease_until=NULL,updated_at=now() WHERE id=$1`, id, committedRun); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if !enabled || deleted != nil {
		if _, err = tx.Exec(ctx, `UPDATE graph_trigger_event SET state='skipped',error='Trigger disabled before dispatch',lease_until=NULL,updated_at=now() WHERE id=$1`, id); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	run, startErr := s.StartRun(ctx, trigger.WorkspaceID, trigger.CreatorID, GraphStartParams{VersionID: trigger.VersionID, RootIssueID: rootID, Input: input, RuntimeBindings: trigger.RuntimeBindings, Repository: trigger.Repository, IdempotencyKey: "trigger-event:" + id})
	if startErr != nil {
		message := "Graph trigger could not start the run"
		state := "failed"
		var ge *GraphError
		if errors.As(startErr, &ge) {
			message = ge.Error()
			if (ge.Code == "capability_missing" || ge.Code == "unavailable" || ge.Code == "feature_disabled") && attempts < 10 {
				state = "pending"
			}
		} else if attempts < 10 {
			state = "pending"
		}
		if _, err = tx.Exec(ctx, `UPDATE graph_trigger_event SET state=$2,error=$3,lease_until=NULL,available_at=now()+($4*interval '30 seconds'),updated_at=now() WHERE id=$1`, id, state, message, min(attempts, 20)); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE graph_trigger SET last_error=$2,updated_at=now() WHERE id=$1`, triggerID, message); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `UPDATE graph_trigger_event SET state='started',run_id=$2,error='',lease_until=NULL,updated_at=now() WHERE id=$1`, id, run.ID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE graph_trigger SET last_error='',updated_at=now() WHERE id=$1`, triggerID); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if s.NotifyRun != nil {
		s.NotifyRun(ctx, run)
	}
	return nil
}
