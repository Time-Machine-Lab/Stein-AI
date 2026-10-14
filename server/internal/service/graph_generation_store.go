package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Client-visible states of an AI authoring request. They are derived on read
// from the authoring task, the stored draft and any recorded failure, so no
// writer has to keep a separate status column in step with the task queue.
const (
	GraphGenerationQueued    = "queued"
	GraphGenerationRunning   = "running"
	GraphGenerationSucceeded = "succeeded"
	GraphGenerationFailed    = "failed"
	GraphGenerationCancelled = "cancelled"
)

// GraphGenerationQuerier is the subset of pgx shared by a pool and a
// transaction, so each store function works in either.
type GraphGenerationQuerier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// GraphGenerationRecord is one authoring request. ID is also the authoring
// task's ID. TaskStatus and TaskError come from that task and are empty when
// the task row no longer exists.
type GraphGenerationRecord struct {
	ID             string
	WorkspaceID    string
	RequesterID    string
	AgentID        string
	Request        json.RawMessage
	Actors         json.RawMessage
	Draft          json.RawMessage
	Summary        string
	Submissions    int
	LastError      string
	FailureCode    string
	FailureMessage string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	TaskStatus     string
	TaskError      string

	// TaskFailureReason is the authoring task's failure classifier.
	TaskFailureReason string
}

// graphGenerationActiveTaskSQL lists the task states in which an authoring
// request still occupies its requester's single slot.
const graphGenerationActiveTaskSQL = `('queued','dispatched','running','waiting_local_directory','deferred')`

// LockGraphGenerationRequester serializes creation of authoring requests for
// one requester in a workspace until the transaction ends.
func LockGraphGenerationRequester(ctx context.Context, tx GraphGenerationQuerier, workspaceID, requesterID string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "graph_generation:"+workspaceID+":"+requesterID)
	return err
}

// InsertGraphGeneration stores a new request. The caller creates the
// authoring task in the same transaction and passes its ID as record.ID.
func InsertGraphGeneration(ctx context.Context, q GraphGenerationQuerier, record GraphGenerationRecord) error {
	_, err := q.Exec(ctx, `INSERT INTO graph_generation(id,workspace_id,requester_id,agent_id,request,actors) VALUES($1,$2,$3,$4,$5,$6)`,
		record.ID, record.WorkspaceID, record.RequesterID, record.AgentID, []byte(record.Request), []byte(record.Actors))
	return err
}

// ListActiveGraphGenerationIDs returns the requester's requests whose task has
// not reached a terminal state and that have no outcome yet.
func ListActiveGraphGenerationIDs(ctx context.Context, q GraphGenerationQuerier, workspaceID, requesterID string) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT g.id::text FROM graph_generation g JOIN agent_task_queue t ON t.id=g.id
WHERE g.workspace_id=$1 AND g.requester_id=$2 AND g.draft IS NULL AND g.failure_code='' AND t.status IN `+graphGenerationActiveTaskSQL+`
ORDER BY g.created_at`, workspaceID, requesterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetGraphGeneration loads a request with its authoring task's state. The
// workspace filter keeps a request invisible outside its workspace.
func GetGraphGeneration(ctx context.Context, q GraphGenerationQuerier, workspaceID, id string) (GraphGenerationRecord, bool, error) {
	var record GraphGenerationRecord
	var request, actors, draft []byte
	err := q.QueryRow(ctx, `SELECT g.id::text,g.workspace_id::text,g.requester_id::text,g.agent_id::text,g.request,g.actors,g.draft,
g.summary,g.submissions,g.last_error,g.failure_code,g.failure_message,g.created_at,g.updated_at,COALESCE(t.status,''),COALESCE(t.error,''),COALESCE(t.failure_reason,'')
FROM graph_generation g LEFT JOIN agent_task_queue t ON t.id=g.id WHERE g.workspace_id=$1 AND g.id=$2`, workspaceID, id).Scan(
		&record.ID, &record.WorkspaceID, &record.RequesterID, &record.AgentID, &request, &actors, &draft,
		&record.Summary, &record.Submissions, &record.LastError, &record.FailureCode, &record.FailureMessage,
		&record.CreatedAt, &record.UpdatedAt, &record.TaskStatus, &record.TaskError, &record.TaskFailureReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return GraphGenerationRecord{}, false, nil
	}
	if err != nil {
		return GraphGenerationRecord{}, false, err
	}
	record.Request, record.Actors, record.Draft = request, actors, draft
	return record, true, nil
}

// RecordGraphGenerationSubmission spends one of the request's submissions.
// ok is false when the request is closed or the budget is exhausted.
func RecordGraphGenerationSubmission(ctx context.Context, q GraphGenerationQuerier, id string, limit int) (submissions int, ok bool, err error) {
	err = q.QueryRow(ctx, `UPDATE graph_generation SET submissions=submissions+1,updated_at=now()
WHERE id=$1 AND draft IS NULL AND failure_code='' AND submissions<$2 RETURNING submissions`, id, limit).Scan(&submissions)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	return submissions, err == nil, err
}

// AcceptGraphGenerationDraft stores the validated draft. It reports false
// when the request already has an outcome, so a late or repeated submission
// cannot replace a draft the user may already be reviewing.
func AcceptGraphGenerationDraft(ctx context.Context, q GraphGenerationQuerier, id string, draft json.RawMessage, summary string) (bool, error) {
	tag, err := q.Exec(ctx, `UPDATE graph_generation SET draft=$2,summary=$3,last_error='',updated_at=now()
WHERE id=$1 AND draft IS NULL AND failure_code=''`, id, []byte(draft), summary)
	return tag.RowsAffected() == 1, err
}

// RejectGraphGenerationDraft keeps the latest validation error for diagnosis.
func RejectGraphGenerationDraft(ctx context.Context, q GraphGenerationQuerier, id, message string) error {
	_, err := q.Exec(ctx, `UPDATE graph_generation SET last_error=$2,updated_at=now() WHERE id=$1 AND draft IS NULL`, id, message)
	return err
}

// FailGraphGeneration records a server-decided failure. It reports false when
// the request already has an outcome.
func FailGraphGeneration(ctx context.Context, q GraphGenerationQuerier, id, code, message string) (bool, error) {
	tag, err := q.Exec(ctx, `UPDATE graph_generation SET failure_code=$2,failure_message=$3,updated_at=now()
WHERE id=$1 AND draft IS NULL AND failure_code=''`, id, code, message)
	return tag.RowsAffected() == 1, err
}

// PruneGraphGenerations drops one requester's requests older than the cutoff.
// It is bounded so a single create call never turns into a large delete.
func PruneGraphGenerations(ctx context.Context, q GraphGenerationQuerier, workspaceID, requesterID string, cutoff time.Time) error {
	_, err := q.Exec(ctx, `DELETE FROM graph_generation WHERE id IN (
SELECT id FROM graph_generation WHERE workspace_id=$1 AND requester_id=$2 AND created_at<$3 ORDER BY created_at LIMIT 50)`, workspaceID, requesterID, cutoff)
	return err
}
