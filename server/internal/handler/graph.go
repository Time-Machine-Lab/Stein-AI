package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/graph"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func readGraphBody(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, "invalid Graph request: "+err.Error())
		return false
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "Graph request must contain one JSON object")
		return false
	}
	return true
}

func writeGraphError(w http.ResponseWriter, err error) {
	var ge *service.GraphError
	var ve *graph.ValidationError
	var ce *graph.Error
	if errors.As(err, &ve) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"code": "invalid_definition", "error": ve.Error(), "diagnostics": []any{ve}})
		return
	}
	code, message := "", ""
	if errors.As(err, &ge) {
		code, message = ge.Code, ge.Message
	} else if errors.As(err, &ce) {
		code, message = ce.Code, ce.Message
	}
	if code != "" {
		status := http.StatusConflict
		switch code {
		case "forbidden":
			status = http.StatusForbidden
		case "not_found":
			status = http.StatusNotFound
		case "invalid_id", "invalid_idempotency_key", "invalid_decision", "invalid_request":
			status = http.StatusBadRequest
		case "disabled", "feature_disabled", "unavailable":
			status = http.StatusServiceUnavailable
		}
		if ge != nil && ge.NodePath != "" {
			writeJSON(w, status, map[string]any{"code": code, "error": message, "node_path": ge.NodePath, "node_label": ge.NodeLabel, "agent_name": ge.AgentName})
		} else {
			writeErrorCode(w, status, code, message)
		}
		return
	}
	slog.Error("Graph operation failed", "error", err)
	writeError(w, http.StatusInternalServerError, "Graph operation failed")
}

func (h *Handler) graphHuman(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "Graph management requires a human actor")
		return "", "", false
	}
	user, ok := requireUserID(w, r)
	if !ok {
		return "", "", false
	}
	ws := h.resolveWorkspaceID(r)
	if ws == "" {
		writeError(w, http.StatusBadRequest, "workspace is required")
		return "", "", false
	}
	if h.GraphService == nil {
		writeError(w, http.StatusServiceUnavailable, "Graph service unavailable")
		return "", "", false
	}
	return ws, user, true
}

type graphDefinitionRequest struct {
	Definition       graph.Definition `json:"definition"`
	ExpectedRevision int64            `json:"expected_revision"`
	IdempotencyKey   string           `json:"idempotency_key"`
}

func (h *Handler) ListGraphs(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	v, err := h.GraphService.ListDefinitions(r.Context(), ws, user)
	if err != nil {
		writeGraphError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (h *Handler) GetGraph(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	v, err := h.GraphService.GetDefinition(r.Context(), ws, user, chi.URLParam(r, "graphId"))
	if err != nil {
		writeGraphError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (h *Handler) CreateGraph(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	var req graphDefinitionRequest
	if !readGraphBody(w, r, &req) {
		return
	}
	v, err := h.GraphService.CreateDefinition(r.Context(), ws, user, req.Definition, req.IdempotencyKey)
	if err != nil {
		writeGraphError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}
func (h *Handler) UpdateGraph(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	var req graphDefinitionRequest
	if !readGraphBody(w, r, &req) {
		return
	}
	v, err := h.GraphService.UpdateDefinition(r.Context(), ws, user, chi.URLParam(r, "graphId"), req.Definition, req.ExpectedRevision, req.IdempotencyKey)
	if err != nil {
		writeGraphError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (h *Handler) ValidateGraph(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	v, err := h.GraphService.GetDefinition(r.Context(), ws, user, chi.URLParam(r, "graphId"))
	if err != nil {
		writeGraphError(w, err)
		return
	}
	diagnostics := []any{}
	if err = graph.ValidateDefinition(v.Definition); err != nil {
		diagnostics = append(diagnostics, map[string]string{"code": "invalid_definition", "message": err.Error()})
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": err == nil, "diagnostics": diagnostics})
}
func (h *Handler) PublishGraph(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	var req service.GraphControlParams
	if !readGraphBody(w, r, &req) {
		return
	}
	v, err := h.GraphService.Publish(r.Context(), ws, user, chi.URLParam(r, "graphId"), req.ExpectedRevision, req.IdempotencyKey)
	if err != nil {
		writeGraphError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}
func (h *Handler) ArchiveGraph(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	var req service.GraphControlParams
	if !readGraphBody(w, r, &req) {
		return
	}
	v, err := h.GraphService.ArchiveDefinition(r.Context(), ws, user, chi.URLParam(r, "graphId"), req.ExpectedRevision, req.IdempotencyKey)
	if err != nil {
		writeGraphError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (h *Handler) DetachGraphIssue(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	var req struct {
		IdempotencyKey string `json:"idempotency_key"`
	}
	if !readGraphBody(w, r, &req) {
		return
	}
	if err := h.GraphService.DetachIssue(r.Context(), ws, user, chi.URLParam(r, "id"), req.IdempotencyKey); err != nil {
		writeGraphError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) ListGraphRuns(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	v, err := h.GraphService.ListRuns(r.Context(), ws, user)
	if err != nil {
		writeGraphError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (h *Handler) GetGraphRun(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	v, err := h.GraphService.GetRun(r.Context(), ws, user, chi.URLParam(r, "runId"))
	if err != nil {
		writeGraphError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (h *Handler) StartGraphRun(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	var req service.GraphStartParams
	if !readGraphBody(w, r, &req) {
		return
	}
	v, err := h.GraphService.StartRun(r.Context(), ws, user, req)
	if err != nil {
		writeGraphError(w, err)
		return
	}
	h.notifyGraphRun(r.Context(), v)
	writeJSON(w, http.StatusAccepted, v)
}
func (h *Handler) ControlGraphRun(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	var req service.GraphControlParams
	if !readGraphBody(w, r, &req) {
		return
	}
	v, err := h.GraphService.Control(r.Context(), ws, user, chi.URLParam(r, "runId"), chi.URLParam(r, "action"), req)
	if err != nil {
		writeGraphError(w, err)
		return
	}
	h.notifyGraphRun(r.Context(), v)
	writeJSON(w, http.StatusOK, v)
}
func (h *Handler) ApproveGraphRun(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	var req service.GraphApprovalParams
	if !readGraphBody(w, r, &req) {
		return
	}
	v, err := h.GraphService.Approve(r.Context(), ws, user, chi.URLParam(r, "runId"), req)
	if err != nil {
		writeGraphError(w, err)
		return
	}
	h.notifyGraphRun(r.Context(), v)
	writeJSON(w, http.StatusOK, v)
}

func (h *Handler) graphNodeIdentity(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	if r.Header.Get("X-Actor-Source") != "task_token" {
		writeError(w, http.StatusForbidden, "Graph node operations require a task token")
		return "", "", false
	}
	if !requestHasClientCapability(r, protocol.CLICapabilityGraphNodeSubmitV1) {
		writeError(w, http.StatusUpgradeRequired, "update the Multica CLI for Graph node submission")
		return "", "", false
	}
	ws, task := r.Header.Get("X-Workspace-ID"), r.Header.Get("X-Task-ID")
	if ws == "" || task == "" {
		writeError(w, http.StatusForbidden, "task identity is missing")
		return "", "", false
	}
	if h.GraphService == nil {
		writeError(w, http.StatusServiceUnavailable, "Graph service unavailable")
		return "", "", false
	}
	return ws, task, true
}
func (h *Handler) GraphNodeContext(w http.ResponseWriter, r *http.Request) {
	ws, task, ok := h.graphNodeIdentity(w, r)
	if !ok {
		return
	}
	v, err := h.GraphService.TaskContext(r.Context(), ws, task)
	if err != nil {
		writeGraphError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

type graphSubmitRequest struct {
	IdempotencyKey string       `json:"idempotency_key"`
	Result         graph.Result `json:"result"`
}

func (h *Handler) SubmitGraphNode(w http.ResponseWriter, r *http.Request) {
	h.submitGraphNode(w, r, false)
}
func (h *Handler) BlockGraphNode(w http.ResponseWriter, r *http.Request) {
	h.submitGraphNode(w, r, true)
}
func (h *Handler) submitGraphNode(w http.ResponseWriter, r *http.Request, block bool) {
	ws, task, ok := h.graphNodeIdentity(w, r)
	if !ok {
		return
	}
	var req graphSubmitRequest
	if !readGraphBody(w, r, &req) {
		return
	}
	if block && req.Result.Outcome != graph.OutcomeEnvironmentBlocked {
		writeError(w, http.StatusBadRequest, "block requires environment_blocked outcome")
		return
	}
	v, err := h.GraphService.Submit(r.Context(), ws, task, req.IdempotencyKey, req.Result)
	if err != nil {
		writeGraphError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

// GraphDaemonSubmission is only reachable with the owning daemon credential.
// The daemon chooses this immutable submission before finalizing artifacts.
func (h *Handler) GraphDaemonSubmission(w http.ResponseWriter, r *http.Request) {
	task := chi.URLParam(r, "taskId")
	_, ws, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, task)
	if !ok {
		return
	}
	v, err := h.GraphService.TaskContext(r.Context(), ws, task)
	if err != nil {
		writeGraphError(w, err)
		return
	}
	if len(v.Submissions) == 0 {
		writeError(w, http.StatusNotFound, "Graph task has no staged submission")
		return
	}
	writeJSON(w, http.StatusOK, v.Submissions[len(v.Submissions)-1])
}
func (h *Handler) GraphDaemonStopped(w http.ResponseWriter, r *http.Request) {
	task := chi.URLParam(r, "taskId")
	_, ws, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, task)
	if !ok {
		return
	}
	var receipt service.GraphTaskReceipt
	if !readGraphBody(w, r, &receipt) {
		return
	}
	if !h.validateGraphReceiptRequest(w, r, task, &receipt) {
		return
	}
	if err := h.GraphService.RecordTaskTerminal(r.Context(), ws, task, receipt); err != nil {
		writeGraphError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
