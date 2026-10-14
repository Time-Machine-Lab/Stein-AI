package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
)

const (
	// GraphGenerationMaxSubmissions is how many drafts the authoring agent may
	// submit for one request, counting rejected ones.
	GraphGenerationMaxSubmissions = 4
	// A request that no runtime has started, or that outlives the run budget,
	// fails with a timeout instead of holding its requester's slot forever.
	GraphGenerationQueueTimeout = 3 * time.Minute
	GraphGenerationRunTimeout   = 15 * time.Minute

	graphGenerationRetention    = 30 * 24 * time.Hour
	graphGenerationMessageRunes = 500
)

// Failure codes reported to clients. They deliberately match the codes the
// synchronous endpoint used, so the editor keeps one set of messages.
const (
	GraphGenerationFailureTimeout = "generation_timeout"
	GraphGenerationFailureInvalid = "generation_invalid"
	GraphGenerationFailureFailed  = "generation_failed"
)

var (
	// ErrGraphGenerationBusy means the requester already has a request in flight.
	ErrGraphGenerationBusy = errors.New("a Graph generation is already in progress")
	// ErrGraphGenerationClosed means the request no longer accepts drafts.
	ErrGraphGenerationClosed = errors.New("this Graph generation is no longer accepting drafts")
)

// GraphDraftRejectedError tells the authoring agent why a draft was refused
// and how many submissions it has left; the message is shown to the agent.
type GraphDraftRejectedError struct {
	Message   string
	Remaining int
}

func (e *GraphDraftRejectedError) Error() string { return e.Message }

type GraphGenerationFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// GraphGenerationView is what the requester sees. Definition is present only
// once a draft has been accepted.
type GraphGenerationView struct {
	ID          string                  `json:"id"`
	Status      string                  `json:"status"`
	AgentID     string                  `json:"agent_id"`
	Definition  json.RawMessage         `json:"definition,omitempty"`
	Summary     string                  `json:"summary,omitempty"`
	Error       *GraphGenerationFailure `json:"error,omitempty"`
	Submissions int                     `json:"submissions"`
	CreatedAt   time.Time               `json:"created_at"`
	UpdatedAt   time.Time               `json:"updated_at"`
}

// GraphGenerationService coordinates AI authoring requests served by runtime
// agents: one task per request, one draft stored with it, and a state the
// requester reads back.
type GraphGenerationService struct {
	Queries   *db.Queries
	TxStarter TxStarter
	DB        GraphGenerationQuerier
	Tasks     *TaskService
	Bus       *events.Bus
	Now       func() time.Time
}

func NewGraphGenerationService(queries *db.Queries, tx TxStarter, database GraphGenerationQuerier, tasks *TaskService, bus *events.Bus) *GraphGenerationService {
	return &GraphGenerationService{Queries: queries, TxStarter: tx, DB: database, Tasks: tasks, Bus: bus, Now: time.Now}
}

func (s *GraphGenerationService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// GraphGenerationCreate carries an already authorized request. Request is the
// alias-only context the authoring agent reads; Actors maps those aliases back
// to workspace identities and never leaves the server.
type GraphGenerationCreate struct {
	WorkspaceID string
	RequesterID string
	AgentID     string
	Request     json.RawMessage
	Actors      json.RawMessage
}

// GraphGenerationViewOf derives the client-visible state. An accepted draft
// wins over everything that happens to the task afterwards, then a recorded
// failure, then the task's own state.
func GraphGenerationViewOf(r GraphGenerationRecord) GraphGenerationView {
	view := GraphGenerationView{ID: r.ID, AgentID: r.AgentID, Summary: r.Summary, Submissions: r.Submissions, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
	switch {
	case len(r.Draft) > 0:
		view.Status, view.Definition = GraphGenerationSucceeded, r.Draft
	case r.FailureCode != "":
		view.Status = GraphGenerationFailed
		view.Error = &GraphGenerationFailure{Code: r.FailureCode, Message: r.FailureMessage}
	default:
		switch r.TaskStatus {
		case "queued", "deferred":
			view.Status = GraphGenerationQueued
		case "dispatched", "running", "waiting_local_directory":
			view.Status = GraphGenerationRunning
		case "cancelled":
			view.Status = GraphGenerationCancelled
		case "completed":
			view.Status = GraphGenerationFailed
			view.Error = &GraphGenerationFailure{Code: GraphGenerationFailureInvalid, Message: "The agent finished without submitting a valid workflow draft."}
		case "failed":
			view.Status = GraphGenerationFailed
			code := GraphGenerationFailureFailed
			if r.TaskFailureReason == "timeout" || r.TaskFailureReason == "queued_expired" {
				code = GraphGenerationFailureTimeout
			}
			message := truncateGraphGenerationMessage(redact.Text(r.TaskError))
			if message == "" {
				message = "The agent could not finish the workflow draft."
			}
			view.Error = &GraphGenerationFailure{Code: code, Message: message}
		default:
			view.Status = GraphGenerationFailed
			view.Error = &GraphGenerationFailure{Code: GraphGenerationFailureFailed, Message: "The authoring task is no longer available."}
		}
	}
	return view
}

func truncateGraphGenerationMessage(message string) string {
	message = strings.TrimSpace(message)
	if utf8.RuneCountInString(message) <= graphGenerationMessageRunes {
		return message
	}
	return string([]rune(message)[:graphGenerationMessageRunes]) + "..."
}

// Create stores a request and queues its authoring task in one transaction. A
// requester has a single request in flight: stale ones are timed out first,
// and a live one makes this call fail with ErrGraphGenerationBusy.
func (s *GraphGenerationService) Create(ctx context.Context, p GraphGenerationCreate) (GraphGenerationView, error) {
	workspaceID, err := util.ParseUUID(p.WorkspaceID)
	if err != nil {
		return GraphGenerationView{}, graphError("invalid_request", "invalid workspace")
	}
	requesterID, err := util.ParseUUID(p.RequesterID)
	if err != nil {
		return GraphGenerationView{}, graphError("invalid_request", "invalid requester")
	}
	agentID, err := util.ParseUUID(p.AgentID)
	if err != nil {
		return GraphGenerationView{}, graphError("invalid_request", "invalid agent")
	}
	if err = s.settleActive(ctx, p.WorkspaceID, p.RequesterID); err != nil {
		return GraphGenerationView{}, err
	}
	params, err := s.Tasks.prepareGraphAuthoringTask(ctx, workspaceID, requesterID, agentID)
	if err != nil {
		return GraphGenerationView{}, err
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return GraphGenerationView{}, err
	}
	defer tx.Rollback(ctx)
	if err = LockGraphGenerationRequester(ctx, tx, p.WorkspaceID, p.RequesterID); err != nil {
		return GraphGenerationView{}, err
	}
	active, err := ListActiveGraphGenerationIDs(ctx, tx, p.WorkspaceID, p.RequesterID)
	if err != nil {
		return GraphGenerationView{}, err
	}
	if len(active) > 0 {
		return GraphGenerationView{}, ErrGraphGenerationBusy
	}
	task, err := s.Queries.WithTx(tx).CreateQuickCreateTask(ctx, params)
	if err != nil {
		return GraphGenerationView{}, fmt.Errorf("create Graph authoring task: %w", err)
	}
	record := GraphGenerationRecord{ID: util.UUIDToString(task.ID), WorkspaceID: p.WorkspaceID, RequesterID: p.RequesterID, AgentID: p.AgentID, Request: p.Request, Actors: p.Actors}
	if err = InsertGraphGeneration(ctx, tx, record); err != nil {
		return GraphGenerationView{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return GraphGenerationView{}, err
	}
	s.Tasks.NotifyTaskEnqueued(ctx, task)
	s.publish(record.WorkspaceID, record.ID, GraphGenerationQueued)
	if err = PruneGraphGenerations(ctx, s.DB, p.WorkspaceID, p.RequesterID, s.now().Add(-graphGenerationRetention)); err != nil {
		slog.Warn("prune Graph generations failed", "workspace_id", p.WorkspaceID, "error", err)
	}
	stored, found, err := GetGraphGeneration(ctx, s.DB, p.WorkspaceID, record.ID)
	if err != nil {
		return GraphGenerationView{}, err
	}
	if !found {
		return GraphGenerationView{}, graphError("not_found", "Graph generation not found")
	}
	return GraphGenerationViewOf(stored), nil
}

// Get returns the requester's own request, applying the timeouts first so a
// request nobody serves does not look alive forever.
func (s *GraphGenerationService) Get(ctx context.Context, workspaceID, requesterID, id string) (GraphGenerationView, error) {
	record, err := s.load(ctx, workspaceID, id)
	if err != nil {
		return GraphGenerationView{}, err
	}
	if record.RequesterID != requesterID {
		return GraphGenerationView{}, graphError("not_found", "Graph generation not found")
	}
	if record, err = s.settle(ctx, record); err != nil {
		return GraphGenerationView{}, err
	}
	return GraphGenerationViewOf(record), nil
}

// Cancel stops a request that is still queued or running. Cancelling one that
// already has an outcome is not an error and changes nothing.
func (s *GraphGenerationService) Cancel(ctx context.Context, workspaceID, requesterID, id string) (GraphGenerationView, error) {
	record, err := s.load(ctx, workspaceID, id)
	if err != nil {
		return GraphGenerationView{}, err
	}
	if record.RequesterID != requesterID {
		return GraphGenerationView{}, graphError("not_found", "Graph generation not found")
	}
	if status := GraphGenerationViewOf(record).Status; status == GraphGenerationQueued || status == GraphGenerationRunning {
		taskID, parseErr := util.ParseUUID(record.ID)
		if parseErr != nil {
			return GraphGenerationView{}, parseErr
		}
		if _, err = s.Tasks.CancelTask(ctx, taskID); err != nil {
			return GraphGenerationView{}, err
		}
		if record, err = s.load(ctx, workspaceID, id); err != nil {
			return GraphGenerationView{}, err
		}
	}
	return GraphGenerationViewOf(record), nil
}

// AuthoringRequest returns the request for the authoring agent that owns the
// task, while the request is still running.
func (s *GraphGenerationService) AuthoringRequest(ctx context.Context, workspaceID, taskID, agentID string) (GraphGenerationRecord, error) {
	record, err := s.load(ctx, workspaceID, taskID)
	if err != nil {
		return GraphGenerationRecord{}, err
	}
	if record.AgentID != agentID {
		return GraphGenerationRecord{}, graphError("forbidden", "this task does not own the Graph generation")
	}
	if record, err = s.settle(ctx, record); err != nil {
		return GraphGenerationRecord{}, err
	}
	if GraphGenerationViewOf(record).Status != GraphGenerationRunning {
		return GraphGenerationRecord{}, ErrGraphGenerationClosed
	}
	return record, nil
}

// GraphDraftSubmission is one draft from the authoring agent. Resolve checks it
// against the stored request and returns the definition to keep.
type GraphDraftSubmission struct {
	WorkspaceID string
	TaskID      string
	AgentID     string
	Summary     string
	Resolve     func(record GraphGenerationRecord) (json.RawMessage, error)
}

type GraphDraftReceipt struct {
	Submissions int `json:"submissions"`
	Remaining   int `json:"remaining"`
}

// SubmitDraft spends one submission and stores the draft when it resolves. A
// rejected draft returns *GraphDraftRejectedError so the agent can fix it in
// the same task; the last rejection ends the request instead.
func (s *GraphGenerationService) SubmitDraft(ctx context.Context, p GraphDraftSubmission) (GraphDraftReceipt, error) {
	record, err := s.AuthoringRequest(ctx, p.WorkspaceID, p.TaskID, p.AgentID)
	if err != nil {
		return GraphDraftReceipt{}, err
	}
	submissions, ok, err := RecordGraphGenerationSubmission(ctx, s.DB, record.ID, GraphGenerationMaxSubmissions)
	if err != nil {
		return GraphDraftReceipt{}, err
	}
	if !ok {
		return GraphDraftReceipt{}, ErrGraphGenerationClosed
	}
	remaining := GraphGenerationMaxSubmissions - submissions
	resolved, resolveErr := p.Resolve(record)
	if resolveErr != nil {
		message := truncateGraphGenerationMessage(resolveErr.Error())
		if err = RejectGraphGenerationDraft(ctx, s.DB, record.ID, message); err != nil {
			slog.Warn("record rejected Graph draft failed", "generation_id", record.ID, "error", err)
		}
		if remaining <= 0 {
			s.fail(ctx, record, GraphGenerationFailureInvalid, "The agent could not produce a valid workflow draft after several attempts.")
		}
		return GraphDraftReceipt{}, &GraphDraftRejectedError{Message: message, Remaining: remaining}
	}
	accepted, err := AcceptGraphGenerationDraft(ctx, s.DB, record.ID, resolved, truncateGraphGenerationMessage(p.Summary))
	if err != nil {
		return GraphDraftReceipt{}, err
	}
	if !accepted {
		return GraphDraftReceipt{}, ErrGraphGenerationClosed
	}
	s.publish(record.WorkspaceID, record.ID, GraphGenerationSucceeded)
	return GraphDraftReceipt{Submissions: submissions, Remaining: remaining}, nil
}

func (s *GraphGenerationService) load(ctx context.Context, workspaceID, id string) (GraphGenerationRecord, error) {
	if _, err := util.ParseUUID(id); err != nil {
		return GraphGenerationRecord{}, graphError("invalid_id", "invalid Graph generation ID")
	}
	record, found, err := GetGraphGeneration(ctx, s.DB, workspaceID, id)
	if err != nil {
		return GraphGenerationRecord{}, err
	}
	if !found {
		return GraphGenerationRecord{}, graphError("not_found", "Graph generation not found")
	}
	return record, nil
}

// settleActive times out the requester's stale requests so that they stop
// counting as in flight.
func (s *GraphGenerationService) settleActive(ctx context.Context, workspaceID, requesterID string) error {
	ids, err := ListActiveGraphGenerationIDs(ctx, s.DB, workspaceID, requesterID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		record, loadErr := s.load(ctx, workspaceID, id)
		if loadErr != nil {
			return loadErr
		}
		if _, err = s.settle(ctx, record); err != nil {
			return err
		}
	}
	return nil
}

// settle fails a queued or running request that exceeded its time budget and
// stops its task. The failure is recorded before the task is cancelled, so the
// outcome is the timeout whichever write the requester observes first.
func (s *GraphGenerationService) settle(ctx context.Context, record GraphGenerationRecord) (GraphGenerationRecord, error) {
	status := GraphGenerationViewOf(record).Status
	age := s.now().Sub(record.CreatedAt)
	var message string
	switch {
	case status != GraphGenerationQueued && status != GraphGenerationRunning:
		return record, nil
	case age > GraphGenerationRunTimeout:
		message = "The agent did not finish the workflow draft in time. Try again, or choose another agent."
	case status == GraphGenerationQueued && age > GraphGenerationQueueTimeout:
		message = "The selected agent's runtime did not pick up the task in time. Check that its daemon is running, or choose another agent."
	default:
		return record, nil
	}
	s.fail(ctx, record, GraphGenerationFailureTimeout, message)
	return s.load(ctx, record.WorkspaceID, record.ID)
}

// fail records a server-decided failure and stops the authoring task.
func (s *GraphGenerationService) fail(ctx context.Context, record GraphGenerationRecord, code, message string) {
	recorded, err := FailGraphGeneration(ctx, s.DB, record.ID, code, message)
	if err != nil {
		slog.Warn("record Graph generation failure failed", "generation_id", record.ID, "error", err)
		return
	}
	if !recorded {
		return
	}
	if taskID, parseErr := util.ParseUUID(record.ID); parseErr == nil {
		if _, err = s.Tasks.CancelTaskWithReason(ctx, taskID, message, "timeout"); err != nil {
			slog.Warn("stop Graph authoring task failed", "generation_id", record.ID, "error", err)
		}
	}
	s.publish(record.WorkspaceID, record.ID, GraphGenerationFailed)
}

func (s *GraphGenerationService) publish(workspaceID, id, status string) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventGraphGenerationUpdated,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		TaskID:      id,
		Payload:     map[string]any{"generation_id": id, "status": status},
	})
}
