package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

type graphReceiptContextKey struct{}
type graphExecutorContextKey struct{}

// WithGraphReceipt carries the authenticated daemon's terminal facts through the
// existing TaskService API. User/task-token handlers must never set this value.
func WithGraphReceipt(ctx context.Context, receipt GraphTaskReceipt) context.Context {
	return context.WithValue(ctx, graphReceiptContextKey{}, receipt)
}
func WithGraphExecutor(ctx context.Context, executorID string) context.Context {
	return context.WithValue(ctx, graphExecutorContextKey{}, executorID)
}

func (s *GraphService) IsTask(ctx context.Context, taskID string) (bool, error) {
	if err := graphUUIDs(taskID); err != nil {
		return false, err
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var managed bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM graph_task_attempt WHERE task_id=$1)`, taskID).Scan(&managed)
	return managed, err
}
func (s *GraphService) IsIssue(ctx context.Context, workspaceID, issueID string) (bool, error) {
	if err := graphUUIDs(workspaceID, issueID); err != nil {
		return false, err
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var managed bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM graph_issue_owner WHERE workspace_id=$1 AND issue_id=$2)`, workspaceID, issueID).Scan(&managed)
	return managed, err
}

func (s *TaskService) CreateGraphTaskTx(ctx context.Context, tx pgx.Tx, p GraphTaskCreateParams) (db.AgentTaskQueue, error) {
	var task db.AgentTaskQueue
	if err := graphUUIDs(p.WorkspaceID, p.RunID, p.NodeRunID, p.TaskID, p.IssueID, p.AgentID, p.RuntimeID, p.InitiatorID); err != nil {
		return task, err
	}
	if p.AttemptNo < 1 || p.AttemptNo > p.Manifest.Ceiling() {
		return task, graphError("budget_exhausted", "invalid Graph attempt budget")
	}
	var parent any
	if p.PreviousTaskID != "" {
		if err := graphUUIDs(p.PreviousTaskID); err != nil {
			return task, err
		}
		parent = p.PreviousTaskID
	}
	contextJSON, err := json.Marshal(map[string]any{"graph": p.Manifest})
	if err != nil {
		return task, err
	}
	result, err := tx.Exec(ctx, `INSERT INTO agent_task_queue(id,agent_id,runtime_id,issue_id,status,priority,attempt,max_attempts,parent_task_id,retry_of_task_id,force_fresh_session,is_leader_task,originator_user_id,accountable_user_id,initiator_user_id,originator_source,trigger_evidence_kind,trigger_evidence_ref_id,context,quick_actions_disabled)
SELECT $1,$2,$3,$4,'queued',0,$5,$6,$7,$7,true,false,$8,$8,$8,'direct_human','graph_node',$9,$10,true
WHERE lock_task_owner_rows($2,$4,$3) AND EXISTS(SELECT 1 FROM graph_task_attempt WHERE task_id=$1 AND workspace_id=$11 AND node_run_id=$9 AND run_id=$12)
AND EXISTS(SELECT 1 FROM issue WHERE id=$4 AND workspace_id=$11)
AND EXISTS(SELECT 1 FROM agent WHERE id=$2 AND workspace_id=$11)
AND EXISTS(SELECT 1 FROM agent_runtime WHERE id=$3 AND workspace_id=$11)`, p.TaskID, p.AgentID, p.RuntimeID, p.IssueID, p.AttemptNo, p.Manifest.Ceiling(), parent, p.InitiatorID, p.NodeRunID, contextJSON, p.WorkspaceID, p.RunID)
	if err != nil {
		return task, err
	}
	if result.RowsAffected() != 1 {
		return task, graphError("graph_managed", "task binding or workspace ownership invalid")
	}
	taskUUID, err := util.ParseUUID(p.TaskID)
	if err != nil {
		return task, err
	}
	return s.Queries.WithTx(tx).GetAgentTask(ctx, taskUUID)
}

// graphTerminalTask keeps Graph completion atomic and separate from ordinary
// comment synthesis, resume-pointer mutation and automatic retry side effects.
func (s *TaskService) graphTerminalTask(ctx context.Context, taskID pgtype.UUID, state string, result []byte, errMsg, failureReason, sessionID, workDir, branchName string) (*db.AgentTaskQueue, bool, error) {
	if s.Graph == nil {
		return nil, false, nil
	}
	managed, err := s.Graph.IsTask(ctx, util.UUIDToString(taskID))
	if err != nil || !managed {
		return nil, managed, err
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return nil, true, err
	}
	defer tx.Rollback(ctx)
	if _, err = s.Graph.LockGraphTaskRunTx(ctx, tx, util.UUIDToString(taskID)); err != nil {
		return nil, true, err
	}
	qtx := s.Queries.WithTx(tx)
	var task db.AgentTaskQueue
	if state == "completed" {
		task, err = qtx.CompleteAgentTask(ctx, db.CompleteAgentTaskParams{ID: taskID, Result: result, SessionID: pgtype.Text{String: sessionID, Valid: sessionID != ""}, WorkDir: pgtype.Text{String: workDir, Valid: workDir != ""}, BranchName: pgtype.Text{String: branchName, Valid: branchName != ""}})
	} else {
		failureReason = taskfailure.NormalizeDaemonReason(failureReason, errMsg).String()
		if failureReason == "" {
			failureReason = taskfailure.Classify(errMsg).String()
		}
		task, err = qtx.FailAgentTask(ctx, db.FailAgentTaskParams{ID: taskID, Error: pgtype.Text{String: errMsg, Valid: true}, FailureReason: pgtype.Text{String: failureReason, Valid: failureReason != ""}, SessionID: pgtype.Text{String: sessionID, Valid: sessionID != ""}, WorkDir: pgtype.Text{String: workDir, Valid: workDir != ""}, BranchName: pgtype.Text{String: branchName, Valid: branchName != ""}})
	}
	changed := true
	if errors.Is(err, pgx.ErrNoRows) {
		changed = false
		task, err = qtx.GetAgentTask(ctx, taskID)
	}
	if err != nil {
		return nil, true, err
	}
	receipt, hasReceipt := ctx.Value(graphReceiptContextKey{}).(GraphTaskReceipt)
	if !hasReceipt && len(result) > 0 {
		var body struct {
			Receipt *GraphTaskReceipt `json:"graph_receipt"`
		}
		if err = json.Unmarshal(result, &body); err != nil {
			return nil, true, err
		}
		if body.Receipt != nil {
			receipt = *body.Receipt
			hasReceipt = true
		}
	}
	if hasReceipt {
		if err = s.Graph.RecordTaskTerminalTx(ctx, tx, util.UUIDToString(taskID), receipt); err != nil {
			return nil, true, err
		}
	} else {
		if _, err = tx.Exec(ctx, `INSERT INTO graph_operation(operation_key,workspace_id,run_id,kind) SELECT 'terminal:'||task_id::text,workspace_id,run_id,'reconcile' FROM graph_task_attempt WHERE task_id=$1 ON CONFLICT(operation_key) DO UPDATE SET consumed_at=NULL`, taskID); err != nil {
			return nil, true, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, true, err
	}
	if changed {
		s.ReconcileAgentStatus(ctx, task.AgentID)
		if task.Status == "completed" {
			s.captureTaskCompleted(ctx, task)
			s.broadcastTaskEvent(ctx, protocol.EventTaskCompleted, task)
		} else {
			s.captureTaskFailed(ctx, task)
			s.broadcastTaskFailedEvent(ctx, task, errMsg, failureReason, false)
		}
	}
	return &task, true, nil
}

func (s *TaskService) startGraphTask(ctx context.Context, taskID pgtype.UUID) (*db.AgentTaskQueue, bool, error) {
	if s.Graph == nil {
		return nil, false, nil
	}
	managed, err := s.Graph.IsTask(ctx, util.UUIDToString(taskID))
	if err != nil || !managed {
		return nil, managed, err
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return nil, true, err
	}
	defer tx.Rollback(ctx)
	_, err = s.Graph.TaskAdmissionTx(ctx, tx, util.UUIDToString(taskID))
	if err != nil {
		var ge *GraphError
		if errors.As(err, &ge) && (ge.Code == "run_held" || ge.Code == "run_paused") {
			if _, e := tx.Exec(ctx, `UPDATE agent_task_queue SET status='queued',dispatched_at=NULL,prepare_lease_expires_at=NULL WHERE id=$1 AND status='dispatched' AND started_at IS NULL`, taskID); e != nil {
				return nil, true, e
			}
			if e := tx.Commit(ctx); e != nil {
				return nil, true, e
			}
			return nil, true, graphError("run_paused", "Graph execution is held; retry this same task after resume")
		}
		return nil, true, err
	}
	executorID, _ := ctx.Value(graphExecutorContextKey{}).(string)
	if executorID != "" {
		if err = s.Graph.BindTaskExecutorTx(ctx, tx, util.UUIDToString(taskID), executorID); err != nil {
			return nil, true, err
		}
	} else {
		var bound *string
		if err = tx.QueryRow(ctx, `SELECT executor_id FROM graph_task_attempt WHERE task_id=$1`, taskID).Scan(&bound); err != nil {
			return nil, true, err
		}
		if bound == nil {
			return nil, true, graphError("executor_required", "Graph start requires a bound daemon instance")
		}
	}
	task, err := s.Queries.WithTx(tx).StartAgentTask(ctx, taskID)
	if err != nil {
		return nil, true, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, true, err
	}
	s.captureTaskStarted(ctx, task)
	s.ReconcileAgentStatus(ctx, task.AgentID)
	s.broadcastTaskEvent(ctx, protocol.EventTaskRunning, task)
	return &task, true, nil
}

func (s *TaskService) graphClaimTransaction(ctx context.Context, taskID pgtype.UUID, fn func(*db.Queries) error) error {
	if s.Graph == nil {
		return s.runInTx(ctx, fn)
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = s.Graph.TaskAdmissionTx(ctx, tx, util.UUIDToString(taskID)); err != nil {
		return err
	}
	if err = fn(s.Queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *TaskService) graphTaskManaged(ctx context.Context, taskID pgtype.UUID) bool {
	if s.Graph == nil {
		return false
	}
	managed, err := s.Graph.IsTask(ctx, util.UUIDToString(taskID))
	return managed || err != nil
}
func (s *TaskService) rejectGraphIssue(ctx context.Context, issue db.Issue) error {
	if s.Graph == nil {
		return nil
	}
	managed, err := s.Graph.IsIssue(ctx, util.UUIDToString(issue.WorkspaceID), util.UUIDToString(issue.ID))
	if err != nil {
		return fmt.Errorf("check Graph ownership: %w", err)
	}
	if managed {
		return graphError("graph_managed", "Graph controls execution for this issue")
	}
	return nil
}
