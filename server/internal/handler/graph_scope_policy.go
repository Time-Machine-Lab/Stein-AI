package handler

import (
	"net/http"
	"strings"
)

type graphTaskScope struct {
	TaskID, RunID, IssueID, CurrentTaskID string
	TaskState, RunState, ControlState     string
}

func (s graphTaskScope) acceptsWrites() bool {
	return s.TaskID != "" && s.TaskID == s.CurrentTaskID && s.TaskState == "running" &&
		(s.RunState == "active" || s.RunState == "blocked") && s.ControlState != "cancel_requested"
}

// Graph credentials expose a small capability surface. Ordinary issue writes,
// delegation, chat creation and human run controls must never inherit the
// initiating human's broader workspace permissions.
func graphTaskRouteAllowed(method, path, issueID string) bool {
	path = strings.TrimSuffix(path, "/")
	if method == http.MethodGet && path == "/api/graph/node/context" {
		return true
	}
	if method == http.MethodPost && (path == "/api/graph/node/submissions" || path == "/api/graph/node/block" || path == "/api/upload-file") {
		return true
	}
	if issueID != "" && method == http.MethodPost && path == "/api/issues/"+issueID+"/comments" {
		return true
	}
	if method == http.MethodGet && strings.HasPrefix(path, "/api/attachments/") {
		parts := strings.Split(strings.TrimPrefix(path, "/api/attachments/"), "/")
		return len(parts) == 1 && parts[0] != "" || len(parts) == 2 && parts[0] != "" && (parts[1] == "download" || parts[1] == "content")
	}
	return false
}
