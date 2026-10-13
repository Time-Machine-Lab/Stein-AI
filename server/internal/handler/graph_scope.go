package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

type graphScopeContextKey struct{}

func graphScopeFromRequest(r *http.Request) *graphTaskScope {
	scope, _ := r.Context().Value(graphScopeContextKey{}).(*graphTaskScope)
	return scope
}

// GraphTaskTokenScope runs after authentication, before any protected route.
// It checks the current binding on every request, including reads, so an old
// attempt cannot continue using its token while a successor is executing.
func (h *Handler) GraphTaskTokenScope(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Actor-Source") != "task_token" {
			next.ServeHTTP(w, r)
			return
		}
		if h.DB == nil {
			writeError(w, http.StatusServiceUnavailable, "task scope unavailable")
			return
		}
		s := graphTaskScope{TaskID: r.Header.Get("X-Task-ID")}
		err := h.DB.QueryRow(r.Context(), `SELECT a.run_id::text, COALESCE(n.issue_id::text,''), COALESCE(n.current_task_id::text,''), COALESCE(t.status,''), COALESCE(r.state,''), COALESCE(r.control_state,'')
FROM graph_task_attempt a LEFT JOIN graph_node_run n ON n.id=a.node_run_id AND n.run_id=a.run_id LEFT JOIN graph_run r ON r.id=a.run_id AND r.workspace_id=a.workspace_id LEFT JOIN agent_task_queue t ON t.id=a.task_id
WHERE a.task_id=$1 AND a.workspace_id=$2`, s.TaskID, r.Header.Get("X-Workspace-ID")).Scan(&s.RunID, &s.IssueID, &s.CurrentTaskID, &s.TaskState, &s.RunState, &s.ControlState)
		if errors.Is(err, pgx.ErrNoRows) {
			next.ServeHTTP(w, r)
			return
		}
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "task scope unavailable")
			return
		}
		if !s.acceptsWrites() {
			writeError(w, http.StatusConflict, "Graph attempt is no longer accepting agent requests")
			return
		}
		if !graphTaskRouteAllowed(r.Method, r.URL.Path, s.IssueID) {
			writeError(w, http.StatusForbidden, "this operation is outside the Graph task scope")
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/attachments/") {
			id := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/attachments/"), "/")[0]
			var allowed bool
			err = h.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM attachment f WHERE f.id=$1 AND f.workspace_id=$2 AND (f.task_id=$3 OR EXISTS(SELECT 1 FROM graph_artifact a WHERE a.run_id=$4 AND a.document->>'type'='attachment_ref' AND a.document->>'id'=f.id::text)))`, id, r.Header.Get("X-Workspace-ID"), s.TaskID, s.RunID).Scan(&allowed)
			if err != nil || !allowed {
				writeError(w, http.StatusNotFound, "attachment not found in Graph inputs")
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), graphScopeContextKey{}, &s)))
	})
}
