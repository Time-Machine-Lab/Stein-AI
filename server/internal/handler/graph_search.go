package handler

import (
	"net/http"
	"strconv"

	"github.com/multica-ai/multica/server/internal/service"
)

func graphSearchRequest(w http.ResponseWriter, r *http.Request) (service.GraphSearchParams, bool) {
	q := r.URL.Query()
	p := service.GraphSearchParams{Query: q.Get("q"), State: q.Get("state"), GraphID: q.Get("graph_id"), Cursor: q.Get("cursor")}
	if q.Get("limit") != "" {
		limit, err := strconv.Atoi(q.Get("limit"))
		if err != nil || limit < 1 || limit > 100 {
			writeErrorCode(w, 400, "invalid_request", "limit must be between 1 and 100")
			return p, false
		}
		p.Limit = limit
	}
	return p, true
}
func (h *Handler) SearchGraphs(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	p, ok := graphSearchRequest(w, r)
	if !ok {
		return
	}
	result, err := h.GraphService.SearchDefinitions(r.Context(), ws, user, p)
	if err != nil {
		writeGraphError(w, err)
		return
	}
	writeJSON(w, 200, result)
}
func (h *Handler) SearchGraphRuns(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	p, ok := graphSearchRequest(w, r)
	if !ok {
		return
	}
	result, err := h.GraphService.SearchRuns(r.Context(), ws, user, p)
	if err != nil {
		writeGraphError(w, err)
		return
	}
	writeJSON(w, 200, result)
}
