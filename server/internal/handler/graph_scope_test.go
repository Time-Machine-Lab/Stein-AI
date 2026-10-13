package handler

import (
	"net/http"
	"testing"
)

func TestGraphTaskRouteScope(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		allowed      bool
	}{
		{http.MethodGet, "/api/graph/node/context", true},
		{http.MethodPost, "/api/graph/node/submissions", true},
		{http.MethodPost, "/api/graph/node/block", true},
		{http.MethodPost, "/api/upload-file", true},
		{http.MethodGet, "/api/attachments/file/download", true},
		{http.MethodDelete, "/api/attachments/file", false},
		{http.MethodPost, "/api/issues/child/comments", true},
		{http.MethodPost, "/api/issues/other/comments", false},
		{http.MethodPut, "/api/issues/child", false},
		{http.MethodPost, "/api/issues", false},
		{http.MethodPost, "/api/graphs/graph/publish", false},
		{http.MethodPost, "/api/graph-runs/run/approve", false},
		{http.MethodPost, "/api/chat-sessions", false},
		{http.MethodPost, "/api/autopilots", false},
		{http.MethodGet, "/api/issues/other", false},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			if got := graphTaskRouteAllowed(tc.method, tc.path, "child"); got != tc.allowed {
				t.Fatalf("allowed = %v, want %v", got, tc.allowed)
			}
		})
	}
}

func TestGraphTaskScopeRequiresCurrentLiveExecution(t *testing.T) {
	valid := graphTaskScope{TaskID: "task", CurrentTaskID: "task", TaskState: "running", RunState: "active", ControlState: "run"}
	if !valid.acceptsWrites() {
		t.Fatal("running task rejected")
	}
	valid.ControlState = "pause"
	if !valid.acceptsWrites() {
		t.Fatal("pause must drain running output")
	}
	valid.ControlState = "cancel_requested"
	if valid.acceptsWrites() {
		t.Fatal("cancelled execution accepted output")
	}
	valid.ControlState = "run"
	valid.CurrentTaskID = "next"
	if valid.acceptsWrites() {
		t.Fatal("old attempt accepted output")
	}
	valid.CurrentTaskID = "task"
	valid.TaskState = "completed"
	if valid.acceptsWrites() {
		t.Fatal("completed task token remained writable")
	}
}
