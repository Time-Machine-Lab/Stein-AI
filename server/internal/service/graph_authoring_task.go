package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// GraphAuthoringContextType marks a task that asks a runtime agent to draft a
// Graph definition. Such a task has no issue, chat or autopilot link, like a
// quick-create task, and is never retried automatically.
const GraphAuthoringContextType = "graph_authoring"

// GraphAuthoringContext is the task's context column. It only identifies the
// request: the request itself lives in graph_generation, so the payload that
// travels in claim queries and realtime events carries nothing the requester
// wrote.
type GraphAuthoringContext struct {
	Type        string `json:"type"`
	WorkspaceID string `json:"workspace_id"`
	RequesterID string `json:"requester_id"`
}

// ParseGraphAuthoringContext returns the authoring payload of a task, or false
// for every other kind of task.
func ParseGraphAuthoringContext(task db.AgentTaskQueue) (GraphAuthoringContext, bool) {
	if task.IssueID.Valid || task.ChatSessionID.Valid || task.AutopilotRunID.Valid || len(task.Context) == 0 {
		return GraphAuthoringContext{}, false
	}
	var parsed GraphAuthoringContext
	if err := json.Unmarshal(task.Context, &parsed); err != nil || parsed.Type != GraphAuthoringContextType || parsed.WorkspaceID == "" {
		return GraphAuthoringContext{}, false
	}
	return parsed, true
}

// IsGraphAuthoringTask reports whether the task is an AI authoring request.
func IsGraphAuthoringTask(task db.AgentTaskQueue) bool {
	_, ok := ParseGraphAuthoringContext(task)
	return ok
}

// prepareGraphAuthoringTask validates the authoring agent and resolves the
// attribution without writing anything, so the slow lookups stay outside the
// creating transaction. The task carries no MCP overlay: an authoring run needs
// the multica CLI and nothing the requester has connected elsewhere.
func (s *TaskService) prepareGraphAuthoringTask(ctx context.Context, workspaceID, requesterID, agentID pgtype.UUID) (db.CreateQuickCreateTaskParams, error) {
	agent, err := s.Queries.GetAgent(ctx, agentID)
	if err != nil {
		return db.CreateQuickCreateTaskParams{}, fmt.Errorf("load agent: %w", err)
	}
	if agent.WorkspaceID != workspaceID {
		return db.CreateQuickCreateTaskParams{}, errors.New("agent belongs to another workspace")
	}
	if agent.ArchivedAt.Valid {
		return db.CreateQuickCreateTaskParams{}, errors.New("agent is archived")
	}
	if !agent.RuntimeID.Valid {
		return db.CreateQuickCreateTaskParams{}, errors.New("agent has no runtime")
	}
	contextJSON, err := json.Marshal(GraphAuthoringContext{
		Type:        GraphAuthoringContextType,
		WorkspaceID: util.UUIDToString(workspaceID),
		RequesterID: util.UUIDToString(requesterID),
	})
	if err != nil {
		return db.CreateQuickCreateTaskParams{}, fmt.Errorf("marshal Graph authoring context: %w", err)
	}
	attr, err := s.applyAttributionFallback(ctx, attribution.DirectHumanRun(requesterID, "", pgtype.UUID{}), agent)
	if err != nil {
		return db.CreateQuickCreateTaskParams{}, err
	}
	source, _, evidenceKind, evidenceRef := attributionCreateParams(attr)
	return db.CreateQuickCreateTaskParams{
		AgentID:              agentID,
		RuntimeID:            agent.RuntimeID,
		Priority:             priorityToInt("high"),
		Context:              contextJSON,
		OriginatorUserID:     requesterID,
		AccountableUserID:    attr.AccountableUserID,
		OriginatorSource:     source,
		TriggerEvidenceKind:  evidenceKind,
		TriggerEvidenceRefID: evidenceRef,
	}, nil
}

// publishGraphGenerationUpdate tells the requester's clients to re-read an
// authoring request after any lifecycle change of its task. The payload names
// the request and its coarse state, never its content.
func (s *TaskService) publishGraphGenerationUpdate(workspaceID string, task db.AgentTaskQueue) {
	if workspaceID == "" || !IsGraphAuthoringTask(task) {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventGraphGenerationUpdated,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		TaskID:      util.UUIDToString(task.ID),
		Payload: map[string]any{
			"generation_id": util.UUIDToString(task.ID),
			"status":        task.Status,
		},
	})
}
