package handler

import (
	"encoding/json"
	"github.com/multica-ai/multica/server/internal/service"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGraphRuntimeErrorIdentifiesAffectedNodeAndAgent(t *testing.T) {
	w := httptest.NewRecorder()
	writeGraphError(w, &service.GraphError{Code: "runtime_mismatch", Message: "binding changed", NodePath: "review/body/check", NodeLabel: "Review design", AgentName: "UI reviewer"})
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusConflict || body["node_path"] != "review/body/check" || body["node_label"] != "Review design" || body["agent_name"] != "UI reviewer" {
		t.Fatalf("Graph error lost the affected node and agent: %s", w.Body.String())
	}
}

func TestGraphHumanEndpointsRejectMachineCredentials(t *testing.T) {
	h := &Handler{}
	for _, source := range []string{"task_token", "cloud_pat"} {
		for _, endpoint := range []http.HandlerFunc{h.CreateGraph, h.UpdateGraph, h.PublishGraph, h.StartGraphRun, h.ControlGraphRun, h.ApproveGraphRun} {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
			r.Header.Set("X-Actor-Source", source)
			w := httptest.NewRecorder()
			endpoint(w, r)
			if w.Code != http.StatusForbidden {
				t.Fatalf("%s: got %d, want 403", source, w.Code)
			}
		}
	}
}

func TestGraphNodeEndpointsRequireTaskToken(t *testing.T) {
	h := &Handler{}
	for _, endpoint := range []http.HandlerFunc{h.GraphNodeContext, h.SubmitGraphNode, h.BlockGraphNode} {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
		r.Header.Set("X-Task-ID", "forged")
		r.Header.Set("X-Agent-ID", "forged")
		w := httptest.NewRecorder()
		endpoint(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("got %d, want 403", w.Code)
		}
	}
}

func TestGraphRequestRejectsUnknownAndTrailingFields(t *testing.T) {
	for _, body := range []string{`{"expected_revision":1,"typo":true}`, `{"expected_revision":1}{}`} {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		w := httptest.NewRecorder()
		var request struct {
			ExpectedRevision int64 `json:"expected_revision"`
		}
		if readGraphBody(w, r, &request) {
			t.Fatalf("accepted %s", body)
		}
	}
}
