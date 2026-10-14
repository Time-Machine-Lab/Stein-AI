package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/graph"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// AI authoring through runtime agents. A request becomes a task for the chosen
// agent; the agent reads the request and submits the draft through the CLI
// with its task token. The server only keeps a draft that passes the same
// validation as a saved Graph, and nothing here saves, publishes or runs one.

// graphAgentEligible applies the publication checks to one agent without
// keeping anything: the same ones a published node would face.
func (h *Handler) graphAgentEligible(ctx context.Context, ws, user string, agent db.Agent) error {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	_, err = h.prepareGraphNode(ctx, tx, ws, user, graph.Node{Type: graph.NodeTypeAgent, AgentID: uuidToString(agent.ID)})
	if rollbackErr := tx.Rollback(ctx); err == nil {
		err = rollbackErr
	}
	return err
}

func (h *Handler) CreateGraphGeneration(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	if _, ok = h.workspaceMember(w, r, ws); !ok {
		return
	}
	if h.GraphGeneration == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "generation_unavailable", "AI Graph creation is not available on this server.")
		return
	}
	var req graphGenerationCreateRequest
	r.Body = http.MaxBytesReader(w, r.Body, 512<<10)
	if !readGraphBody(w, r, &req) {
		return
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" || len(req.Prompt) > graphGenerationPromptBytes {
		writeErrorCode(w, http.StatusBadRequest, "invalid_request", "Describe the workflow in 1 to 16000 bytes.")
		return
	}
	if err := validateGraphAuthoringHistory(req.Messages); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, req.DefaultAgentID, "default_agent_id")
	if !ok {
		return
	}
	approverID, ok := parseUUIDOrBadRequest(w, req.ApproverID, "approver_id")
	if !ok {
		return
	}
	if len(req.AgentScope) > graphAuthoringDirectoryLimit {
		writeErrorCode(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("Choose at most %d agents.", graphAuthoringDirectoryLimit))
		return
	}
	scope := make([]pgtype.UUID, 0, len(req.AgentScope))
	for index, raw := range req.AgentScope {
		id, ok := parseUUIDOrBadRequest(w, raw, fmt.Sprintf("agent_scope[%d]", index))
		if !ok {
			return
		}
		scope = append(scope, id)
	}
	ctx := r.Context()
	agent, err := h.Queries.GetAgent(ctx, agentID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (uuidToString(agent.WorkspaceID) != ws || agent.ArchivedAt.Valid) {
		writeErrorCode(w, http.StatusNotFound, "not_found", "The selected agent is unavailable in this workspace.")
		return
	}
	if err != nil {
		writeGraphError(w, err)
		return
	}
	if !h.canInvokeAgent(ctx, agent, "member", user, user, ws) {
		writeErrorCode(w, http.StatusForbidden, "forbidden", "You do not have permission to use the selected agent.")
		return
	}
	approver, err := h.Queries.GetMember(ctx, approverID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && uuidToString(approver.WorkspaceID) != ws {
		writeErrorCode(w, http.StatusNotFound, "not_found", "The selected approver is unavailable in this workspace.")
		return
	}
	if err != nil {
		writeGraphError(w, err)
		return
	}
	// The authoring agent must be able to run right now: waiting on an offline
	// machine would only end in a timeout.
	verdict, err := service.AgentReadiness(ctx, h.Queries, agent)
	if err != nil {
		writeGraphError(w, err)
		return
	}
	if !verdict.Ready() {
		writeAgentUnavailable(w, verdict.Detail, verdict.Reason)
		return
	}
	runtime, err := h.Queries.GetAgentRuntime(ctx, agent.RuntimeID)
	if err != nil {
		writeGraphError(w, err)
		return
	}
	if uuidToString(runtime.WorkspaceID) != ws || !runtimeHasCapability(runtime.Metadata, protocol.DaemonCapabilityGraphAuthoringV1) {
		writeErrorCode(w, http.StatusConflict, "capability_missing", "The selected agent's runtime cannot draft Graphs yet. Update the Multica CLI on that machine, or choose another agent.")
		return
	}
	if err = h.graphAgentEligible(ctx, ws, user, agent); err != nil {
		writeGraphError(w, err)
		return
	}
	team, truncated, err := h.graphAuthoringTeam(ctx, ws, user, agent, scope)
	if err != nil {
		writeGraphError(w, err)
		return
	}
	request, actors, err := h.buildGraphAuthoringRequest(ctx, ws, user, req, team, truncated)
	if err != nil {
		writeGraphError(w, err)
		return
	}
	view, err := h.GraphGeneration.Create(ctx, service.GraphGenerationCreate{
		WorkspaceID: ws, RequesterID: user, AgentID: uuidToString(agent.ID), Request: request, Actors: actors,
	})
	if errors.Is(err, service.ErrGraphGenerationBusy) {
		w.Header().Set("Retry-After", "10")
		writeErrorCode(w, http.StatusTooManyRequests, "generation_busy", "A workflow draft is already being generated. Wait for it to finish or cancel it.")
		return
	}
	if err != nil {
		writeGraphError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, view)
}

func (h *Handler) GetGraphGeneration(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	if _, ok = h.workspaceMember(w, r, ws); !ok {
		return
	}
	if h.GraphGeneration == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "generation_unavailable", "AI Graph creation is not available on this server.")
		return
	}
	view, err := h.GraphGeneration.Get(r.Context(), ws, user, chi.URLParam(r, "generationId"))
	if err != nil {
		writeGraphError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *Handler) CancelGraphGeneration(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	if _, ok = h.workspaceMember(w, r, ws); !ok {
		return
	}
	if h.GraphGeneration == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "generation_unavailable", "AI Graph creation is not available on this server.")
		return
	}
	view, err := h.GraphGeneration.Cancel(r.Context(), ws, user, chi.URLParam(r, "generationId"))
	if err != nil {
		writeGraphError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// graphAuthoringIdentity authenticates the authoring agent: only the task
// token of the authoring task, from a CLI that announced the capability.
func (h *Handler) graphAuthoringIdentity(w http.ResponseWriter, r *http.Request) (ws, task, agent string, ok bool) {
	if r.Header.Get("X-Actor-Source") != "task_token" {
		writeError(w, http.StatusForbidden, "Graph authoring requires a task token")
		return "", "", "", false
	}
	if !requestHasClientCapability(r, protocol.CLICapabilityGraphAuthoringV1) {
		writeError(w, http.StatusUpgradeRequired, "update the Multica CLI for Graph authoring")
		return "", "", "", false
	}
	ws, task, agent = r.Header.Get("X-Workspace-ID"), r.Header.Get("X-Task-ID"), r.Header.Get("X-Agent-ID")
	if ws == "" || task == "" || agent == "" {
		writeError(w, http.StatusForbidden, "task identity is missing")
		return "", "", "", false
	}
	if h.GraphGeneration == nil {
		writeError(w, http.StatusServiceUnavailable, "Graph generation unavailable")
		return "", "", "", false
	}
	return ws, task, agent, true
}

func writeGraphAuthoringError(w http.ResponseWriter, err error) {
	if errors.Is(err, service.ErrGraphGenerationClosed) {
		writeErrorCode(w, http.StatusConflict, "generation_closed", "This Graph generation is no longer accepting drafts. Stop work and exit.")
		return
	}
	writeGraphError(w, err)
}

type graphAuthoringContextResponse struct {
	GenerationID  string          `json:"generation_id"`
	Contract      string          `json:"contract"`
	Request       json.RawMessage `json:"request"`
	Limits        graphAuthoringLimits `json:"limits"`
	LastRejection string          `json:"last_rejection,omitempty"`
}

type graphAuthoringLimits struct {
	MaxSubmissions int `json:"max_submissions"`
	Submitted      int `json:"submitted"`
}

// GetGraphAuthoringContext hands the authoring agent its request: the
// contract to follow and the alias-only description of what to draft.
func (h *Handler) GetGraphAuthoringContext(w http.ResponseWriter, r *http.Request) {
	ws, task, agent, ok := h.graphAuthoringIdentity(w, r)
	if !ok {
		return
	}
	record, err := h.GraphGeneration.AuthoringRequest(r.Context(), ws, task, agent)
	if err != nil {
		writeGraphAuthoringError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, graphAuthoringContextResponse{
		GenerationID:  record.ID,
		Contract:      graphAuthoringContract,
		Request:       record.Request,
		Limits:        graphAuthoringLimits{MaxSubmissions: service.GraphGenerationMaxSubmissions, Submitted: record.Submissions},
		LastRejection: record.LastError,
	})
}

type graphAuthoringDraftRequest struct {
	Definition json.RawMessage `json:"definition"`
	Summary    string          `json:"summary"`
}

// SubmitGraphAuthoringDraft validates a draft from the authoring agent. An
// invalid draft is answered with the reason so the agent can correct it in the
// same task, until its submissions run out.
func (h *Handler) SubmitGraphAuthoringDraft(w http.ResponseWriter, r *http.Request) {
	ws, task, agent, ok := h.graphAuthoringIdentity(w, r)
	if !ok {
		return
	}
	var req graphAuthoringDraftRequest
	r.Body = http.MaxBytesReader(w, r.Body, 512<<10)
	if !readGraphBody(w, r, &req) {
		return
	}
	if len(req.Definition) == 0 || len(req.Definition) > graph.MaxDefinitionBytes {
		writeErrorCode(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("definition is required and must not exceed %d bytes", graph.MaxDefinitionBytes))
		return
	}
	receipt, err := h.GraphGeneration.SubmitDraft(r.Context(), service.GraphDraftSubmission{
		WorkspaceID: ws, TaskID: task, AgentID: agent, Summary: req.Summary,
		Resolve: func(record service.GraphGenerationRecord) (json.RawMessage, error) {
			return h.resolveGraphAuthoringDraft(r.Context(), record, req.Definition)
		},
	})
	var rejected *service.GraphDraftRejectedError
	if errors.As(err, &rejected) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"code": "draft_rejected", "error": rejected.Message, "remaining_submissions": rejected.Remaining})
		return
	}
	if err != nil {
		writeGraphAuthoringError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"accepted": true, "submissions": receipt.Submissions, "remaining_submissions": receipt.Remaining})
}

// resolveGraphAuthoringDraft turns the agent's JSON into the definition the
// requester will review: aliases become workspace identities, the result passes
// the complete validator, and every agent is checked again for the requester.
func (h *Handler) resolveGraphAuthoringDraft(ctx context.Context, record service.GraphGenerationRecord, raw json.RawMessage) (json.RawMessage, error) {
	var probe struct {
		SchemaVersion *int `json:"schema_version"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil || probe.SchemaVersion == nil {
		return nil, errors.New("the draft must be one JSON object that contains schema_version: 2")
	}
	actors, err := graphAuthoringActorsOf(record.Actors)
	if err != nil {
		return nil, err
	}
	definition, err := parseGeneratedGraphActors(string(raw), actors)
	if err != nil {
		if strings.Contains(err.Error(), "unknown generated") {
			return nil, fmt.Errorf("%w; valid agent aliases: %s; valid approver aliases: %s", err, strings.Join(sortedKeys(actors.Agents), ", "), strings.Join(sortedKeys(actors.Approvers), ", "))
		}
		return nil, err
	}
	if err = h.verifyGraphAuthoringAgents(ctx, record.WorkspaceID, record.RequesterID, definition); err != nil {
		return nil, err
	}
	return json.Marshal(definition)
}

// verifyGraphAuthoringAgents re-checks, at submission time, that the requester
// may still invoke every agent the draft uses.
func (h *Handler) verifyGraphAuthoringAgents(ctx context.Context, ws, user string, definition graph.Definition) error {
	checked := map[string]bool{}
	return graph.WalkNodes(definition, func(path string, node graph.Node) error {
		if (node.Type != graph.NodeTypeAgent && node.Type != graph.NodeTypeScript) || checked[node.AgentID] {
			return nil
		}
		checked[node.AgentID] = true
		id, err := parseGraphUUID(node.AgentID)
		if err != nil {
			return fmt.Errorf("node %s has an invalid agent", path)
		}
		agent, err := h.Queries.GetAgent(ctx, id)
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				slog.Warn("Graph authoring agent lookup failed", "error", err)
			}
			return fmt.Errorf("node %s uses an agent that is no longer available", path)
		}
		if uuidToString(agent.WorkspaceID) != ws || agent.ArchivedAt.Valid || !h.canInvokeAgent(ctx, agent, "member", user, user, ws) {
			return fmt.Errorf("node %s uses an agent that is no longer available to the requester", path)
		}
		return nil
	})
}
