package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/graph"
)

func graphRefreshAttempts(ctx context.Context, tx pgx.Tx, run *GraphRunRecord) error {
	for i := range run.Nodes {
		n := &run.Nodes[i]
		for j := range n.Attempts {
			a := &n.Attempts[j]
			var receipt []byte
			err := tx.QueryRow(ctx, `SELECT t.status,b.final_receipt,b.stopped_at FROM graph_task_attempt b JOIN agent_task_queue t ON t.id=b.task_id WHERE b.workspace_id=$1 AND b.task_id=$2`, run.WorkspaceID, a.TaskID).Scan(&a.State, &receipt, &a.StoppedAt)
			if err != nil {
				return err
			}
			if len(receipt) > 0 {
				var r GraphTaskReceipt
				if err = json.Unmarshal(receipt, &r); err != nil {
					return err
				}
				a.Receipt = &r
			}
		}
		n.CanResolveBlock = run.ControlState == "run" && !graphTerminal(run.State) && graphRetryAllowed(*n) == nil
		n.CanRetry = n.CanResolveBlock && len(n.Attempts) > 0
	}
	return nil
}

func graphTaskBinding(ctx context.Context, tx pgx.Tx, ws, taskID string) (string, string, error) {
	if err := graphUUIDs(ws, taskID); err != nil {
		return "", "", err
	}
	var runID, nodeID string
	err := tx.QueryRow(ctx, `SELECT run_id::text,node_run_id::text FROM graph_task_attempt WHERE workspace_id=$1 AND task_id=$2`, ws, taskID).Scan(&runID, &nodeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", graphError("not_found", "Graph task not found")
	}
	return runID, nodeID, err
}
func graphCurrentAttempt(run *GraphRunRecord, nodeID, taskID string) (*GraphNodeRunRecord, *GraphAttemptRecord, error) {
	for i := range run.Nodes {
		n := &run.Nodes[i]
		if n.ID != nodeID {
			continue
		}
		if len(n.Attempts) == 0 {
			return nil, nil, graphError("stale_attempt", "node has no active attempt")
		}
		a := &n.Attempts[len(n.Attempts)-1]
		if a.TaskID != taskID {
			return nil, nil, graphError("stale_attempt", "task is no longer the current attempt")
		}
		return n, a, nil
	}
	return nil, nil, graphError("not_found", "node not found")
}
func (s *GraphService) TaskContext(ctx context.Context, ws, taskID string) (GraphTaskContext, error) {
	var result GraphTaskContext
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	runID, nodeID, err := graphTaskBinding(ctx, tx, ws, taskID)
	if err != nil {
		return result, err
	}
	run, err := graphLoadRun(ctx, tx, ws, runID, false)
	if err != nil {
		return result, err
	}
	node, attempt, err := graphCurrentAttempt(&run, nodeID, taskID)
	if err != nil {
		return result, err
	}
	result = GraphTaskContext{WorkspaceID: ws, IssueID: node.IssueID, RunID: runID, NodeRunID: nodeID, State: run.State, ControlState: run.ControlState, Manifest: attempt.Manifest, ManifestHash: attempt.ManifestHash, RootSnapshot: run.RootSnapshot, Submissions: node.Submissions}
	return result, nil
}

func (s *GraphService) Submit(ctx context.Context, ws, taskID, key string, result graph.Result) (GraphSubmissionRecord, error) {
	var record GraphSubmissionRecord
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return record, err
	}
	defer tx.Rollback(ctx)
	runID, nodeID, err := graphTaskBinding(ctx, tx, ws, taskID)
	if err != nil {
		return record, err
	}
	run, err := graphLoadRun(ctx, tx, ws, runID, true)
	if err != nil {
		return record, err
	}
	if key == "" || len(key) > 200 {
		return record, graphError("invalid_idempotency_key", "submission requires an idempotency key")
	}
	contentHash, err := graphHash(result)
	if err != nil {
		return record, err
	}
	var previousHash string
	var previous []byte
	err = tx.QueryRow(ctx, `SELECT content_hash,document FROM graph_submission WHERE workspace_id=$1 AND task_id=$2 AND idempotency_key=$3`, ws, taskID, key).Scan(&previousHash, &previous)
	if err == nil {
		if previousHash != contentHash {
			return record, graphError("idempotency_conflict", "submission key already contains different content")
		}
		if err = json.Unmarshal(previous, &record); err != nil {
			return record, err
		}
		return record, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return record, err
	}
	node, attempt, err := graphCurrentAttempt(&run, nodeID, taskID)
	if err != nil {
		return record, err
	}
	if err = graph.CanAcceptResult(graph.RunState(run.State), graph.ControlState(run.ControlState), attempt.Manifest.Identity, result.Identity); err != nil {
		return record, graphError("stale_attempt", err.Error())
	}
	var status string
	var receipt []byte
	if err = tx.QueryRow(ctx, `SELECT t.status,a.final_receipt FROM agent_task_queue t JOIN graph_task_attempt a ON a.task_id=t.id WHERE t.id=$1 AND a.workspace_id=$2`, taskID, ws).Scan(&status, &receipt); err != nil {
		return record, err
	}
	if status != "running" || len(receipt) > 0 {
		return record, graphError("stale_attempt", "only the running current task can submit")
	}
	if node.Spec == nil {
		return record, graphError("input_mismatch", "node input is unavailable")
	}
	if err = graph.ValidateResult(node.Spec.Node, result); err != nil {
		return record, graphError("output_invalid", err.Error())
	}
	b, err := json.Marshal(result)
	if err != nil {
		return record, err
	}
	if len(b) > 1024*1024 {
		return record, graphError("output_invalid", "submission exceeds 1 MiB")
	}
	if len(node.Submissions) >= 100 {
		return record, graphError("submission_limit", "node submission limit exceeded")
	}
	record = GraphSubmissionRecord{ID: uuid.NewString(), TaskID: taskID, ContentHash: contentHash, Result: result, CreatedAt: time.Now().UTC()}
	doc, err := graphJSON(record)
	if err != nil {
		return record, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO graph_submission(id,workspace_id,run_id,node_run_id,task_id,idempotency_key,content_hash,document) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, record.ID, ws, runID, nodeID, taskID, key, contentHash, doc)
	if err != nil {
		return record, err
	}
	node.Submissions = append(node.Submissions, record)
	graphEvent(&run, "node.submitted", taskID, record.ID)
	if err = graphPersistRun(ctx, tx, &run); err != nil {
		return record, err
	}
	return record, tx.Commit(ctx)
}

// LockGraphTaskRunTx must precede task-row mutations. All Graph mutations take
// the run lock before node/task locks so terminal callbacks and cancellation do
// not deadlock with the coordinator. Non-Graph tasks return false.
func (s *GraphService) LockGraphTaskRunTx(ctx context.Context, tx pgx.Tx, taskID string) (bool, error) {
	var workspaceID, runID string
	err := tx.QueryRow(ctx, `SELECT workspace_id::text,run_id::text FROM graph_task_attempt WHERE task_id=$1`, taskID).Scan(&workspaceID, &runID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err = graphLockWorkspace(ctx, tx, workspaceID); err != nil {
		return true, err
	}
	var locked string
	err = tx.QueryRow(ctx, `SELECT id::text FROM graph_run WHERE id=$1 FOR UPDATE`, runID).Scan(&locked)
	return true, err
}

// RecordTaskTerminalTx is called in the SAME transaction as the queue terminal
// write. It accepts late physical receipts, without reopening business acceptance.
func (s *GraphService) RecordTaskTerminalTx(ctx context.Context, tx pgx.Tx, taskID string, receipt GraphTaskReceipt) error {
	var ws, runID string
	err := tx.QueryRow(ctx, `SELECT workspace_id::text,run_id::text FROM graph_task_attempt WHERE task_id=$1`, taskID).Scan(&ws, &runID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	run, err := graphLoadRun(ctx, tx, ws, runID, true)
	if err != nil {
		return err
	}
	var manifestData, previousData []byte
	var executorID *string
	err = tx.QueryRow(ctx, `SELECT manifest,final_receipt,executor_id FROM graph_task_attempt WHERE workspace_id=$1 AND task_id=$2 FOR UPDATE`, ws, taskID).Scan(&manifestData, &previousData, &executorID)
	if err != nil {
		return err
	}
	var manifest graph.AttemptManifest
	if err = json.Unmarshal(manifestData, &manifest); err != nil {
		return err
	}
	var previous *GraphTaskReceipt
	if len(previousData) > 0 {
		previous = &GraphTaskReceipt{}
		if err = json.Unmarshal(previousData, previous); err != nil {
			return err
		}
	}
	if executorID == nil || *executorID != receipt.ExecutorID {
		return graphError("stale_executor", "stop receipt does not match the executor bound at start")
	}
	var taskState, failureReason string
	if err = tx.QueryRow(ctx, `SELECT status,COALESCE(failure_reason,'') FROM agent_task_queue WHERE id=$1`, taskID).Scan(&taskState, &failureReason); err != nil {
		return err
	}
	if taskState != "completed" && taskState != "failed" && taskState != "cancelled" {
		return graphError("invalid_state", "terminal receipt requires a terminal task")
	}
	receipt.Retryable = taskState == "failed" && retryableReasons[failureReason]
	receipt.FailureReason = failureReason
	if err = graphValidateReceipt(manifest.Identity, previous, receipt); err != nil {
		return err
	}
	// Submission validity is a business acceptance decision. A missing or foreign
	// reference must not discard an authenticated process-stop receipt or roll
	// back provider completion. The coordinator only resolves this node's own
	// submissions and validates their full attempt identity before accepting one.
	b, err := graphJSON(receipt)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE graph_task_attempt SET final_receipt=$3,stopped_at=$4 WHERE workspace_id=$1 AND task_id=$2`, ws, taskID, b, receipt.StoppedAt); err != nil {
		return err
	}
	return graphOperation(ctx, tx, run, "terminal:"+taskID, "reconcile")
}
func (s *GraphService) RecordTaskTerminal(ctx context.Context, ws, taskID string, receipt GraphTaskReceipt) error {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, _, err = graphTaskBinding(ctx, tx, ws, taskID); err != nil {
		return err
	}
	if err = s.RecordTaskTerminalTx(ctx, tx, taskID, receipt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// TaskAdmissionTx is a fail-closed, transaction-local second check for claim and
// start. The caller holds the run lock until its queue state transition commits.
func (s *GraphService) TaskAdmissionTx(ctx context.Context, tx pgx.Tx, taskID string) (bool, error) {
	managed, err := s.LockGraphTaskRunTx(ctx, tx, taskID)
	if err != nil || !managed {
		return managed, err
	}
	if !s.AdvanceEnabled {
		return true, graphError("run_paused", "Graph advancement is disabled")
	}
	var ws, runID, nodeID string
	err = tx.QueryRow(ctx, `SELECT workspace_id::text,run_id::text,node_run_id::text FROM graph_task_attempt WHERE task_id=$1`, taskID).Scan(&ws, &runID, &nodeID)
	if err != nil {
		return true, err
	}
	run, err := graphLoadRun(ctx, tx, ws, runID, false)
	if err != nil {
		return true, err
	}
	node, attempt, err := graphCurrentAttempt(&run, nodeID, taskID)
	if err != nil {
		return true, err
	}
	if run.State != "active" || run.ControlState != "run" || node.State != "active" {
		return true, graphError("run_held", "Graph run is paused, blocked, cancelled or terminal")
	}
	if run.Deadline != nil && !run.Deadline.After(time.Now()) {
		return true, graphError("deadline_exceeded", "run deadline reached")
	}
	var receipt []byte
	if err = tx.QueryRow(ctx, `SELECT final_receipt FROM graph_task_attempt WHERE task_id=$1`, taskID).Scan(&receipt); err != nil {
		return true, err
	}
	if len(receipt) > 0 || attempt.StoppedAt != nil {
		return true, graphError("stale_attempt", "attempt already stopped")
	}
	if s.AuthorizeRun == nil {
		return true, graphError("capability_missing", "execution authorization unavailable")
	}
	version, err := graphLoadVersion(ctx, tx, ws, run.VersionID)
	if err != nil {
		return true, err
	}
	return true, s.authorizeVersion(ctx, tx, ws, run.InitiatorID, version, run.RuntimeBindings)
}

// BindTaskExecutorTx is called at the successful start transition under the run
// lock. Once execution starts, a different daemon instance cannot acknowledge it.
func (s *GraphService) BindTaskExecutorTx(ctx context.Context, tx pgx.Tx, taskID, executorID string) error {
	if executorID == "" {
		return graphError("executor_required", "Graph start requires the daemon instance identity")
	}
	managed, err := s.LockGraphTaskRunTx(ctx, tx, taskID)
	if err != nil || !managed {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE graph_task_attempt SET executor_id=$2 WHERE task_id=$1 AND (executor_id IS NULL OR executor_id=$2) AND final_receipt IS NULL`, taskID, executorID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return graphError("stale_executor", "attempt was started by a different executor")
	}
	return nil
}

// BindTaskExecutorForClaimTx can replace an executor only while the exact queue
// claim is still preparing. A later claim has a different dispatched_at value;
// once start commits, executor identity is immutable even across daemon restarts.
func (s *GraphService) BindTaskExecutorForClaimTx(ctx context.Context, tx pgx.Tx, taskID, executorID string, dispatchedAt time.Time) error {
	if executorID == "" || dispatchedAt.IsZero() {
		return graphError("executor_required", "Graph claim requires executor identity and dispatch time")
	}
	managed, err := s.TaskAdmissionTx(ctx, tx, taskID)
	if err != nil {
		return err
	}
	if !managed {
		return graphError("not_found", "Graph task not found")
	}
	result, err := tx.Exec(ctx, `UPDATE graph_task_attempt a SET executor_id=$2 FROM agent_task_queue t WHERE a.task_id=$1 AND t.id=a.task_id AND t.status='dispatched' AND t.started_at IS NULL AND t.dispatched_at=$3 AND a.final_receipt IS NULL AND a.stopped_at IS NULL`, taskID, executorID, dispatchedAt)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return graphError("stale_executor", "task claim was replaced or execution already started")
	}
	return nil
}

func (s *GraphService) AdmitTaskTx(ctx context.Context, tx pgx.Tx, taskID, phase string) (bool, error) {
	return s.TaskAdmissionTx(ctx, tx, taskID)
}

// graphStopUnclaimedTx proves non-execution only for an attempt that never had
// an executor and whose task has never started. Preparing/dispatched attempts
// require the bound daemon's physical receipt even if a timeout cancels them.
func graphStopUnclaimedTx(ctx context.Context, tx pgx.Tx, attempt *GraphAttemptRecord) (bool, error) {
	var state string
	err := tx.QueryRow(ctx, `UPDATE agent_task_queue t SET status='cancelled',completed_at=COALESCE(completed_at,now()) WHERE t.id=$1 AND t.status IN ('queued','deferred','cancelled') AND t.started_at IS NULL AND EXISTS(SELECT 1 FROM graph_task_attempt a WHERE a.task_id=t.id AND a.executor_id IS NULL AND a.final_receipt IS NULL) RETURNING status`, attempt.TaskID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	attempt.State = state
	attempt.StoppedAt = &now
	attempt.Receipt = &GraphTaskReceipt{Identity: attempt.Manifest.Identity, ProviderExit: "not_started", DeliveryStatus: "not_required", ExecutorID: "server:never-started", StoppedAt: now}
	receipt, err := graphJSON(attempt.Receipt)
	if err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx, `UPDATE graph_task_attempt SET stopped_at=$2,final_receipt=$3 WHERE task_id=$1`, attempt.TaskID, now, receipt)
	return err == nil, err
}
