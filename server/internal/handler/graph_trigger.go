package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/service"
)

func (h *Handler) ListGraphTriggers(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	items, err := h.GraphService.ListTriggers(r.Context(), ws, user, chi.URLParam(r, "graphId"))
	if err != nil {
		writeGraphError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (h *Handler) SaveGraphTrigger(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	var body service.GraphTriggerWrite
	if !readGraphBody(w, r, &body) {
		return
	}
	id, graphID := chi.URLParam(r, "triggerId"), chi.URLParam(r, "graphId")
	if id != "" {
		if _, ok = parseUUIDOrBadRequest(w, id, "triggerId"); !ok {
			return
		}
		tx, err := h.TxStarter.Begin(r.Context())
		if err != nil {
			writeGraphError(w, err)
			return
		}
		defer tx.Rollback(r.Context())
		err = tx.QueryRow(r.Context(), `SELECT graph_id FROM graph_trigger WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL`, ws, id).Scan(&graphID)
		if err != nil {
			writeErrorCode(w, 404, "not_found", "Graph trigger not found")
			return
		}
	}
	result, err := h.GraphService.SaveTrigger(r.Context(), ws, user, graphID, id, body)
	if err != nil {
		writeGraphError(w, err)
		return
	}
	status := 200
	if id == "" {
		status = 201
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, result)
}
func (h *Handler) DeleteGraphTrigger(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	var body service.GraphControlParams
	if !readGraphBody(w, r, &body) {
		return
	}
	if err := h.GraphService.DeleteTrigger(r.Context(), ws, user, chi.URLParam(r, "triggerId"), body); err != nil {
		writeGraphError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) RotateGraphTriggerSecret(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	var body service.GraphControlParams
	if !readGraphBody(w, r, &body) {
		return
	}
	result, err := h.GraphService.RotateTriggerSecret(r.Context(), ws, user, chi.URLParam(r, "triggerId"), body)
	if err != nil {
		writeGraphError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, result)
}
func (h *Handler) ListGraphTriggerEvents(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	items, err := h.GraphService.ListTriggerEvents(r.Context(), ws, user, chi.URLParam(r, "triggerId"))
	if err != nil {
		writeGraphError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

// This public ingress authenticates only the dedicated, revocable trigger secret.
// It never accepts a member/task token as authority to invoke another trigger.
func (h *Handler) InvokeGraphTrigger(w http.ResponseWriter, r *http.Request) {
	if h.GraphService == nil {
		writeErrorCode(w, 503, "unavailable", "Graph triggers unavailable")
		return
	}
	ip := h.clientIPForRateLimit(r)
	if ip != "" && h.WebhookAbsoluteIPRateLimiter != nil && !h.WebhookAbsoluteIPRateLimiter.Allow(r.Context(), ip) {
		writeWebhookRateLimit(w, r, h.WebhookAbsoluteIPRateLimiter, ip, "absolute_ip", h.Metrics)
		return
	}
	if ip != "" && h.WebhookIPRateLimiter != nil && !slidingWindowLimiterCheck(r.Context(), h.WebhookIPRateLimiter, ip) {
		writeWebhookRateLimit(w, r, h.WebhookIPRateLimiter, ip, "bad_credential_ip", h.Metrics)
		return
	}
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		if ip != "" && h.WebhookIPRateLimiter != nil {
			h.WebhookIPRateLimiter.Allow(r.Context(), ip)
		}
		writeErrorCode(w, 403, "forbidden", "invalid webhook credentials")
		return
	}
	var body struct {
		Input json.RawMessage `json:"input"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 70000)
	if !readGraphBody(w, r, &body) {
		return
	}
	if len(body.Input) == 0 {
		body.Input = json.RawMessage(`{}`)
	}
	result, err := h.GraphService.InvokeTrigger(r.Context(), chi.URLParam(r, "triggerId"), strings.TrimPrefix(auth, "Bearer "), r.Header.Get("Idempotency-Key"), body.Input)
	if err != nil {
		var ge *service.GraphError
		if errors.As(err, &ge) && ge.Code == "forbidden" && ip != "" && h.WebhookIPRateLimiter != nil {
			h.WebhookIPRateLimiter.Allow(r.Context(), ip)
		}
		writeGraphError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"event_id": result.ID, "state": result.State, "run_id": result.RunID})
}
