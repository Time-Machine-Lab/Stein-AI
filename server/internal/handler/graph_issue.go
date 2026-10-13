package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Graph persistence is authoritative; these ordinary Issue events only refresh
// the existing Issue lists and Graph query caches after a committed transition.
func (h *Handler) notifyGraphRun(ctx context.Context, run service.GraphRunRecord) {
	ids := []string{run.RootIssueID}
	for _, node := range run.Nodes {
		if node.IssueID != "" {
			ids = append(ids, node.IssueID)
		}
	}
	prefix := h.getIssuePrefix(ctx, parseUUID(run.WorkspaceID))
	for _, id := range ids {
		issue, err := h.Queries.GetIssue(ctx, parseUUID(id))
		if err != nil || uuidToString(issue.WorkspaceID) != run.WorkspaceID {
			continue
		}
		response := issueToResponse(issue, prefix)
		h.hydrateGraphIssues(ctx, []*IssueResponse{&response})
		h.publish(protocol.EventIssueUpdated, run.WorkspaceID, "system", "", map[string]any{
			"issue": response, "status_changed": true,
		})
	}
}

func graphProtectedIssueFields(fields map[string]json.RawMessage) bool {
	for _, field := range []string{"description", "status", "assignee_type", "assignee_id", "parent_issue_id", "project_id", "stage", "start_date"} {
		if _, ok := fields[field]; ok {
			return true
		}
	}
	return false
}

func (h *Handler) graphOwnsIssue(ctx context.Context, issue db.Issue) bool {
	if h.GraphService == nil {
		return false
	}
	managed, err := h.GraphService.IsIssue(ctx, uuidToString(issue.WorkspaceID), uuidToString(issue.ID))
	if err != nil {
		slog.Warn("Graph issue scope unavailable", "issue_id", uuidToString(issue.ID), "error", err)
		return true
	}
	return managed
}

func (h *Handler) rejectGraphIssueMutation(w http.ResponseWriter, r *http.Request, issue db.Issue) bool {
	if h.GraphService == nil {
		return false
	}
	managed, err := h.GraphService.IsIssue(r.Context(), uuidToString(issue.WorkspaceID), uuidToString(issue.ID))
	if err != nil {
		writeGraphError(w, err)
		return true
	}
	if managed {
		writeErrorCode(w, http.StatusConflict, "graph_managed", "use the Graph run controls for this issue")
		return true
	}
	return false
}

func (h *Handler) rejectGraphBatchMutation(w http.ResponseWriter, r *http.Request, ids []string) bool {
	if h.GraphService == nil || h.DB == nil {
		return false
	}
	uids := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		parsed, ok := parseUUIDOrBadRequest(w, id, "issue_id")
		if !ok {
			return true
		}
		uids = append(uids, parsed)
	}
	var owned bool
	err := h.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM graph_issue_owner WHERE workspace_id=$1 AND issue_id=ANY($2::uuid[]))`, h.resolveWorkspaceID(r), uids).Scan(&owned)
	if err != nil {
		writeGraphError(w, err)
		return true
	}
	if owned {
		writeErrorCode(w, http.StatusConflict, "graph_managed", "batch contains an issue controlled by Graph")
		return true
	}
	return false
}

// Hydrate a whole response window with one ownership lookup. The relation is
// authoritative even for terminal runs, until the parent is explicitly detached.
func (h *Handler) hydrateGraphIssues(ctx context.Context, issues []*IssueResponse) {
	if h.GraphService == nil || h.DB == nil || len(issues) == 0 {
		return
	}
	ids := make([]pgtype.UUID, 0, len(issues))
	byID := map[string][]*IssueResponse{}
	for _, issue := range issues {
		if issue == nil {
			continue
		}
		id, err := parseGraphUUID(issue.ID)
		if err != nil {
			continue
		}
		ids = append(ids, id)
		byID[issue.ID] = append(byID[issue.ID], issue)
	}
	rows, err := h.DB.Query(ctx, `SELECT issue_id::text,run_id::text,COALESCE(node_run_id::text,'') FROM graph_issue_owner WHERE issue_id=ANY($1::uuid[])`, ids)
	if err != nil {
		slog.Warn("Graph issue hydration failed", "error", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, run, node string
		if err = rows.Scan(&id, &run, &node); err != nil {
			return
		}
		for _, issue := range byID[id] {
			issue.ExecutionMode = "graph"
			issue.GraphRunID = run
			issue.GraphNodeRunID = node
		}
	}
}
func (h *Handler) hydrateGraphIssueSlice(ctx context.Context, issues []IssueResponse) {
	refs := make([]*IssueResponse, len(issues))
	for i := range issues {
		refs[i] = &issues[i]
	}
	h.hydrateGraphIssues(ctx, refs)
}
