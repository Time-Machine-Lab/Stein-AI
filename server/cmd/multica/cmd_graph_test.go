package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/graph"
)

func graphCLIManifest() graph.AttemptManifest {
	return graph.AttemptManifest{ProtocolVersion: 1, Identity: graph.AttemptIdentity{
		RunID: "run-1", NodeRunID: "node-1", TaskID: "task-1", AttemptNo: 1,
		ExecutionGeneration: 1, InputHash: strings.Repeat("a", 64),
	}, NodeSpec: graph.NodeSpec{Node: graph.Node{
		Key: "implement", Type: graph.NodeTypeAgent, AgentID: "agent-1", Instructions: "Work",
		ExecutionMode: graph.ExecutionNone, MaxAttempts: 2, TimeoutSeconds: 60,
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"summary":{"type":"string"}},"required":["summary"]}`),
	}}}
}

func TestGraphSubmitBindsCurrentAttemptAndStableKey(t *testing.T) {
	manifest := graphCLIManifest()
	var submissions []graphNodeSubmitBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("X-Client-Capabilities"), "graph-node-submit-v1") {
			t.Error("missing Graph CLI capability")
		}
		switch r.URL.Path {
		case "/api/graph/node/context":
			_ = json.NewEncoder(w).Encode(map[string]any{"manifest": manifest})
		case "/api/graph/node/submissions":
			var body graphNodeSubmitBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			submissions = append(submissions, body)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "submission-1"})
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := cli.NewAPIClient(server.URL, "ws-1", "mat_test")
	result := []byte(`{"outcome":"pass","output":{"summary":"done"}}`)
	for range 2 {
		if _, err := submitGraphNodeResult(context.Background(), client, result, false, ""); err != nil {
			t.Fatal(err)
		}
	}
	if len(submissions) != 2 || submissions[0].Result.Identity != manifest.Identity {
		t.Fatalf("unexpected submissions: %+v", submissions)
	}
	if submissions[0].IdempotencyKey == "" || submissions[0].IdempotencyKey != submissions[1].IdempotencyKey {
		t.Fatal("identical result retries must use a stable idempotency key")
	}
}

func TestGraphSubmitRejectsStaleIdentityBeforeWrite(t *testing.T) {
	manifest := graphCLIManifest()
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			writes++
			t.Error("stale result must not be posted")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"manifest": manifest})
	}))
	defer server.Close()
	stale := manifest.Identity
	stale.TaskID = "older-task"
	raw, _ := json.Marshal(graph.Result{Identity: stale, Outcome: graph.OutcomePass, Output: json.RawMessage(`{"summary":"old"}`)})
	_, err := submitGraphNodeResult(context.Background(), cli.NewAPIClient(server.URL, "ws-1", "mat_test"), raw, false, "")
	if err == nil || !strings.Contains(err.Error(), "stale_attempt") || writes != 0 {
		t.Fatalf("got writes=%d, error=%v", writes, err)
	}
}

func TestGraphBlockUsesDiagnosticsAndCurrentIdentity(t *testing.T) {
	manifest := graphCLIManifest()
	var submission graphNodeSubmitBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"manifest": manifest})
			return
		}
		if r.URL.Path != "/api/graph/node/block" {
			t.Errorf("wrong path %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&submission)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "blocked-1"})
	}))
	defer server.Close()
	_, err := submitGraphNodeResult(context.Background(), cli.NewAPIClient(server.URL, "ws-1", "mat_test"), []byte(`{"reason":"database is unavailable"}`), true, "block-1")
	if err != nil {
		t.Fatal(err)
	}
	if submission.Result.Outcome != graph.OutcomeEnvironmentBlocked || submission.Result.Identity != manifest.Identity || submission.IdempotencyKey != "block-1" {
		t.Fatalf("wrong blocker: %+v", submission)
	}
}

func TestGraphHumanControlUsesRevisionAndIdempotency(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/graph-runs/run-1/pause" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "run-1"})
	}))
	defer server.Close()
	t.Setenv("MULTICA_SERVER_URL", server.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "human-token")
	t.Setenv("MULTICA_TASK_ID", "")
	t.Setenv("MULTICA_AGENT_ID", "")
	t.Setenv("MULTICA_DAEMON_PORT", "")
	t.Setenv("MULTICA_TASK_CONFIG_ROOT", "")
	cmd := newGraphCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"pause", "run-1", "--expected-revision", "7", "--idempotency-key", "pause-7"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if body["expected_revision"] != float64(7) || body["idempotency_key"] != "pause-7" {
		t.Fatalf("body=%v", body)
	}
}

func TestGraphControlRequiresExplicitReplayKey(t *testing.T) {
	cmd := newGraphCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"pause", "run-1", "--expected-revision", "7"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "idempotency-key") {
		t.Fatalf("error=%v", err)
	}
}

func TestGraphV2ApproverReplacementTargetsExactMemberAndGate(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/graph-runs/run-1/approver" {
			t.Errorf("wrong path %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"id":"run-1"}`))
	}))
	defer server.Close()
	t.Setenv("MULTICA_SERVER_URL", server.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "human-token")
	for _, key := range []string{"MULTICA_TASK_ID", "MULTICA_AGENT_ID", "MULTICA_DAEMON_PORT", "MULTICA_TASK_CONFIG_ROOT"} {
		t.Setenv(key, "")
	}
	cmd := newGraphCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"approver", "run-1", "--expected-revision", "8", "--idempotency-key", "replace-8", "--node-run-id", "gate-1", "--member-id", "new-member", "--previous-member-id", "old-member", "--reason", "Reviewer changed"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if body["node_run_id"] != "gate-1" || body["previous_approver_id"] != "old-member" || body["approver_id"] != "new-member" {
		t.Fatalf("body=%v", body)
	}
}

func TestGraphListSearchEncodesPaginationAndFilters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/graph-runs/search" || r.URL.Query().Get("q") != "A & B" || r.URL.Query().Get("cursor") != "opaque" || r.URL.Query().Get("state") != "blocked" || r.URL.Query().Get("limit") != "10" {
			t.Errorf("wrong search %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"items":[],"total":0}`))
	}))
	defer server.Close()
	t.Setenv("MULTICA_SERVER_URL", server.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "human-token")
	for _, key := range []string{"MULTICA_TASK_ID", "MULTICA_AGENT_ID", "MULTICA_DAEMON_PORT", "MULTICA_TASK_CONFIG_ROOT"} {
		t.Setenv(key, "")
	}
	cmd := newGraphCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"list", "--runs", "--search", "A & B", "--state", "blocked", "--cursor", "opaque", "--limit", "10"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestGraphRuntimeBindingsRejectDuplicateNode(t *testing.T) {
	if _, err := parseGraphRuntimeBindings([]string{"implement=runtime-1", "implement=runtime-2"}); err == nil {
		t.Fatal("duplicate node runtime must be rejected")
	}
}

func TestGraphNodeCommentUsesScopedContextWithoutLiveIssueRead(t *testing.T) {
	manifest := graphCLIManifest()
	const content = "Verification progress\n日志保留，不改变固定输入。"
	var requests []string
	var posted map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/graph/node/context":
			_ = json.NewEncoder(w).Encode(map[string]any{"issue_id": "node-issue-1", "manifest": manifest})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/node-issue-1/comments":
			_ = json.NewDecoder(r.Body).Decode(&posted)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "comment-1", "content": content})
		default:
			t.Errorf("Graph comment must not read live issues: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	setCLITestServerEnv(t, server.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
	t.Setenv("MULTICA_TASK_ID", manifest.Identity.TaskID)
	cmd := newGraphCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"node", "comment", "--body", content})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || posted["content"] != content || len(posted) != 1 {
		t.Fatalf("unexpected scoped comment requests=%v body=%v", requests, posted)
	}
	if !strings.Contains(output.String(), `"id": "comment-1"`) {
		t.Fatalf("missing comment result: %s", output.String())
	}
}

func TestGraphNodeCommentFailsClosedWithoutScopedIssue(t *testing.T) {
	manifest := graphCLIManifest()
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/graph/node/context" {
			writes++
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"manifest": manifest})
	}))
	defer server.Close()
	setCLITestServerEnv(t, server.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
	t.Setenv("MULTICA_TASK_ID", manifest.Identity.TaskID)
	cmd := newGraphCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"node", "comment", "--body", "diagnostic"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "issue") {
		t.Fatalf("error=%v", err)
	}
	if writes != 0 {
		t.Fatal("missing issue context must not fall back to another endpoint")
	}
}
