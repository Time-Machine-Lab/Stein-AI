package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/graph"
	"github.com/multica-ai/multica/server/pkg/llm"
)

type graphGenerationEndpoints interface {
	GenerateGraph(http.ResponseWriter, *http.Request)
	GetGraphGenerationOptions(http.ResponseWriter, *http.Request)
}

func graphGenerationAPI(t *testing.T, h *Handler) graphGenerationEndpoints {
	t.Helper()
	api, ok := any(h).(graphGenerationEndpoints)
	if !ok {
		t.Fatal("Graph must expose AI draft generation and availability endpoints")
	}
	return api
}

func newGraphGenerationTestHandler(t *testing.T, content string) *Handler {
	t.Helper()
	h := &Handler{Queries: testHandler.Queries, TxStarter: testPool, GraphService: testHandler.GraphService, PluginService: testHandler.PluginService, graphGeneration: &graphGenerationGate{}}
	if content != "" {
		upstream := stubLLMCompletion(t, http.StatusOK, content)
		h.LLM = llm.New(llm.Config{BaseURL: upstream.URL, APIKey: "fake-test-key", MaxRetries: -1})
	}
	return h
}

func graphGenerationTestIdentity(t *testing.T) (string, string) {
	t.Helper()
	ctx := context.Background()
	var agentID, approverID string
	if err := testPool.QueryRow(ctx, `INSERT INTO agent (workspace_id, name, owner_id, model, permission_mode, runtime_mode, runtime_config) VALUES ($1, 'Graph draft author test', $2, 'codex-test-model', 'private', 'cloud', '{}') RETURNING id`, testWorkspaceID, testUserID).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id=$1`, agentID) })
	if err := testPool.QueryRow(ctx, `SELECT id FROM member WHERE workspace_id=$1 AND user_id=$2`, testWorkspaceID, testUserID).Scan(&approverID); err != nil {
		t.Fatal(err)
	}
	return agentID, approverID
}

const graphGenerationValidPlan = `{"name":"Research and summarize","input_schema":{"type":"object","additionalProperties":true},"steps":[{"node_key":"research","instructions":"Research the requested topic and provide evidence.","inputs":{},"output_schema":{"type":"object","properties":{"summary":{"type":"string"}},"required":["summary"],"additionalProperties":false},"execution_mode":"none","max_attempts":2,"timeout_seconds":3600},{"node_key":"summarize","instructions":"Summarize the research with citations.","inputs":{"research":{"source":"node_output","node_key":"research","path":"/summary"}},"output_schema":{"type":"object","properties":{"summary":{"type":"string"}},"required":["summary"],"additionalProperties":false},"execution_mode":"none","max_attempts":2,"timeout_seconds":3600}]}`

func TestGraphGenerationRejectsMachineActors(t *testing.T) {
	api := graphGenerationAPI(t, &Handler{})
	for _, source := range []string{"task_token", "cloud_pat"} {
		for _, endpoint := range []http.HandlerFunc{api.GenerateGraph, api.GetGraphGenerationOptions} {
			req := httptest.NewRequest(http.MethodPost, "/api/graphs/generate", strings.NewReader(`{}`))
			req.Header.Set("X-Actor-Source", source)
			w := httptest.NewRecorder()
			endpoint(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("%s: got %d, want 403", source, w.Code)
			}
		}
	}
}

func TestGraphGenerationUnavailableAndMembership(t *testing.T) {
	api := graphGenerationAPI(t, newGraphGenerationTestHandler(t, ""))
	w := httptest.NewRecorder()
	api.GetGraphGenerationOptions(w, newRequest(http.MethodGet, "/api/graphs/generation-options", nil))
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"enabled":false}` {
		t.Fatalf("availability: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	api.GenerateGraph(w, newRequest(http.MethodPost, "/api/graphs/generate", map[string]string{"prompt": "Research a topic"}))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "generation_unavailable") {
		t.Fatalf("unconfigured: %d %s", w.Code, w.Body.String())
	}
	req := newRequest(http.MethodGet, "/api/graphs/generation-options", nil)
	req.Header.Set("X-User-ID", uuid.NewString())
	w = httptest.NewRecorder()
	api.GetGraphGenerationOptions(w, req)
	if w.Code != http.StatusForbidden && w.Code != http.StatusNotFound {
		t.Fatalf("nonmember: %d %s", w.Code, w.Body.String())
	}
}

func TestGraphGenerationProducesValidatedUnsavedDraft(t *testing.T) {
	agentID, approverID := graphGenerationTestIdentity(t)
	api := graphGenerationAPI(t, newGraphGenerationTestHandler(t, graphGenerationValidPlan))
	var before, after int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM graph_definition WHERE workspace_id=$1`, testWorkspaceID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	api.GenerateGraph(w, newRequest(http.MethodPost, "/api/graphs/generate", map[string]string{"prompt": "Research a topic, then summarize the findings", "agent_id": agentID, "approver_id": approverID}))
	if w.Code != http.StatusOK {
		t.Fatalf("generate: %d %s", w.Code, w.Body.String())
	}
	var result struct {
		Definition graph.Definition `json:"definition"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if err := graph.ValidateDefinition(result.Definition); err != nil {
		t.Fatal(err)
	}
	if len(result.Definition.Nodes) != 5 {
		t.Fatalf("expected two steps with start/approval/end, got %+v", result.Definition.Nodes)
	}
	for _, node := range result.Definition.Nodes {
		if node.Type == graph.NodeTypeAgent && node.AgentID != agentID {
			t.Fatal("model changed agent identity")
		}
		if node.Type == graph.NodeTypeApproval && node.ApproverID != approverID {
			t.Fatal("model changed approver identity")
		}
	}
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM graph_definition WHERE workspace_id=$1`, testWorkspaceID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("draft generation persisted a Graph without user saving")
	}
	// A repeated request must not spend on another concurrent/immediate generation.
	w = httptest.NewRecorder()
	api.GenerateGraph(w, newRequest(http.MethodPost, "/api/graphs/generate", map[string]string{"prompt": "Again", "agent_id": agentID, "approver_id": approverID}))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("repeat: %d %s", w.Code, w.Body.String())
	}
}

func TestGraphGenerationRejectsMalformedOrUnsafePlans(t *testing.T) {
	agentID, approverID := graphGenerationTestIdentity(t)
	for name, content := range map[string]string{
		"invalid JSON":         `not JSON`,
		"identity injection":   strings.Replace(graphGenerationValidPlan, `"node_key":"research"`, `"node_key":"research","agent_id":"attacker"`, 1),
		"invalid reference":    strings.Replace(graphGenerationValidPlan, `"node_key":"research","path"`, `"node_key":"future","path"`, 1),
		"unknown topology":     strings.Replace(graphGenerationValidPlan, `"steps":`, `"branches":true,"steps":`, 1),
		"no steps":             `{"name":"Empty","input_schema":{"type":"object"},"steps":[]}`,
		"duplicate key":        strings.Replace(graphGenerationValidPlan, `"node_key":"summarize"`, `"node_key":"research"`, 1),
		"duplicate JSON field": strings.Replace(graphGenerationValidPlan, `"name":`, `"name":"Ignored","name":`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			api := graphGenerationAPI(t, newGraphGenerationTestHandler(t, content))
			w := httptest.NewRecorder()
			api.GenerateGraph(w, newRequest(http.MethodPost, "/api/graphs/generate", map[string]string{"prompt": "Plan my work", "agent_id": agentID, "approver_id": approverID}))
			if w.Code != http.StatusBadGateway {
				t.Fatalf("got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestGraphGenerationBoundRuntimeMustSupportGraphButMayBeOffline(t *testing.T) {
	agentID, approverID := graphGenerationTestIdentity(t)
	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, owner_id) VALUES ($1, 'Graph authoring runtime', 'cloud', 'claude', 'offline', $2) RETURNING id`, testWorkspaceID, testUserID).Scan(&runtimeID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id=$1`, runtimeID) })
	if _, err := testPool.Exec(context.Background(), `UPDATE agent SET runtime_id=$2 WHERE id=$1`, agentID, runtimeID); err != nil {
		t.Fatal(err)
	}
	request := map[string]string{"prompt": "Plan", "agent_id": agentID, "approver_id": approverID}
	w := httptest.NewRecorder()
	graphGenerationAPI(t, newGraphGenerationTestHandler(t, graphGenerationValidPlan)).GenerateGraph(w, newRequest(http.MethodPost, "/api/graphs/generate", request))
	if w.Code != http.StatusOK {
		t.Fatalf("offline Claude must allow drafting: %d %s", w.Code, w.Body.String())
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_runtime SET provider='codex' WHERE id=$1`, runtimeID); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	graphGenerationAPI(t, newGraphGenerationTestHandler(t, graphGenerationValidPlan)).GenerateGraph(w, newRequest(http.MethodPost, "/api/graphs/generate", request))
	if w.Code != http.StatusOK {
		t.Fatalf("offline Codex must allow drafting: %d %s", w.Code, w.Body.String())
	}
}

func TestGraphGenerationGateCapsConcurrentRequestsAndExpiresCooldowns(t *testing.T) {
	gate := graphGenerationGate{}
	var releases []func()
	for i := 0; i < graphGenerationMaxConcurrent; i++ {
		release, ok := gate.acquire(uuid.NewString())
		if !ok {
			t.Fatal("request within concurrency budget rejected")
		}
		releases = append(releases, release)
	}
	if _, ok := gate.acquire("over-budget"); ok {
		t.Fatal("concurrency ceiling not enforced")
	}
	for _, release := range releases {
		release()
	}
	release, ok := gate.acquire("same-actor")
	if !ok {
		t.Fatal("released concurrency slot was not restored")
	}
	if _, ok := gate.acquire("same-actor"); ok {
		t.Fatal("duplicate actor allowed during request")
	}
	release()
	if _, ok := gate.acquire("same-actor"); ok {
		t.Fatal("duplicate actor allowed during cooldown")
	}
	gate.slots["same-actor"] = graphGenerationSlot{expiresAt: time.Now().Add(-time.Second)}
	release, ok = gate.acquire("same-actor")
	if !ok {
		t.Fatal("expired actor cooldown not removed")
	}
	release()
}

func TestGraphGenerationRejectsInvalidRequestsAndResources(t *testing.T) {
	agentID, approverID := graphGenerationTestIdentity(t)
	for name, request := range map[string]map[string]string{
		"empty prompt":     {"prompt": " ", "agent_id": agentID, "approver_id": approverID},
		"oversized prompt": {"prompt": strings.Repeat("a", 16001), "agent_id": agentID, "approver_id": approverID},
		"bad agent ID":     {"prompt": "Plan", "agent_id": "bad", "approver_id": approverID},
		"missing approver": {"prompt": "Plan", "agent_id": agentID, "approver_id": uuid.NewString()},
		"missing agent":    {"prompt": "Plan", "agent_id": uuid.NewString(), "approver_id": approverID},
		"arbitrary model":  {"prompt": "Plan", "agent_id": agentID, "approver_id": approverID, "model": "expensive-model"},
	} {
		t.Run(name, func(t *testing.T) {
			api := graphGenerationAPI(t, newGraphGenerationTestHandler(t, graphGenerationValidPlan))
			w := httptest.NewRecorder()
			api.GenerateGraph(w, newRequest(http.MethodPost, "/api/graphs/generate", request))
			if w.Code < 400 || w.Code >= 500 {
				t.Fatalf("got %d: %s", w.Code, w.Body.String())
			}
		})
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE agent SET model=NULL WHERE id=$1`, agentID); err != nil {
		t.Fatal(err)
	}
	api := graphGenerationAPI(t, newGraphGenerationTestHandler(t, graphGenerationValidPlan))
	w := httptest.NewRecorder()
	api.GenerateGraph(w, newRequest(http.MethodPost, "/api/graphs/generate", map[string]string{"prompt": "Plan", "agent_id": agentID, "approver_id": approverID}))
	if w.Code != http.StatusConflict {
		t.Fatalf("agent without model: %d %s", w.Code, w.Body.String())
	}
}

type graphGenerationTestHTTPClient func(*http.Request) (*http.Response, error)

func (f graphGenerationTestHTTPClient) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestGraphGenerationUsesBoundedServerModelWithoutAgentSecrets(t *testing.T) {
	agentID, approverID := graphGenerationTestIdentity(t)
	if _, err := testPool.Exec(context.Background(), `UPDATE agent SET instructions='PRIVATE_AGENT_INSTRUCTIONS', custom_env='{"TOKEN":"PRIVATE_CREDENTIAL"}' WHERE id=$1`, agentID); err != nil {
		t.Fatal(err)
	}
	h := newGraphGenerationTestHandler(t, "")
	h.LLM = llm.New(llm.Config{APIKey: "fake-key", DefaultModel: "configured-server-model", MaxRetries: -1, HTTPClient: graphGenerationTestHTTPClient(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > graphGenerationTimeout {
			t.Error("upstream request lacks bounded deadline")
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "PRIVATE_") || strings.Contains(string(raw), agentID) || strings.Contains(string(raw), approverID) {
			t.Error("agent context or identities leaked to authoring provider")
		}
		var request struct {
			Model     string `json:"model"`
			MaxTokens int    `json:"max_completion_tokens"`
			Messages  []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err = json.Unmarshal(raw, &request); err != nil {
			t.Fatal(err)
		}
		if request.Model != "configured-server-model" || request.MaxTokens != 8192 || len(request.Messages) != 2 || request.Messages[1].Role != "user" || request.Messages[1].Content != "Research my topic" {
			t.Fatalf("unexpected bounded generation request: %+v", request)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":` + jsonString(graphGenerationValidPlan) + `},"finish_reason":"stop"}]}`)), Request: r}, nil
	})})
	w := httptest.NewRecorder()
	graphGenerationAPI(t, h).GenerateGraph(w, newRequest(http.MethodPost, "/api/graphs/generate", map[string]string{"prompt": "Research my topic", "agent_id": agentID, "approver_id": approverID}))
	if w.Code != http.StatusOK {
		t.Fatalf("generation: %d %s", w.Code, w.Body.String())
	}
}

func TestGraphGenerationRejectsCrossWorkspaceAndPrivateAgentReferences(t *testing.T) {
	agentID, approverID := graphGenerationTestIdentity(t)
	ctx := context.Background()
	var otherUser, otherWorkspace, otherMember string
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name,email) VALUES ('Other graph user',$1) RETURNING id`, uuid.NewString()+"@graph-test.invalid").Scan(&otherUser); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id=$1`, otherUser) })
	if err := testPool.QueryRow(ctx, `INSERT INTO workspace (name,slug,issue_prefix) VALUES ('Other graph workspace',$1,'GOT') RETURNING id`, "graph-other-"+uuid.NewString()).Scan(&otherWorkspace); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id=$1`, otherWorkspace) })
	if err := testPool.QueryRow(ctx, `INSERT INTO member (workspace_id,user_id,role) VALUES ($1,$2,'owner') RETURNING id`, otherWorkspace, otherUser).Scan(&otherMember); err != nil {
		t.Fatal(err)
	}
	request := func(approver string, want int) {
		t.Helper()
		w := httptest.NewRecorder()
		graphGenerationAPI(t, newGraphGenerationTestHandler(t, graphGenerationValidPlan)).GenerateGraph(w, newRequest(http.MethodPost, "/api/graphs/generate", map[string]string{"prompt": "Plan", "agent_id": agentID, "approver_id": approver}))
		if w.Code != want {
			t.Fatalf("expected %d, got %d: %s", want, w.Code, w.Body.String())
		}
	}
	request(otherMember, http.StatusNotFound)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET owner_id=$2 WHERE id=$1`, agentID, otherUser); err != nil {
		t.Fatal(err)
	}
	request(approverID, http.StatusForbidden)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET workspace_id=$2 WHERE id=$1`, agentID, otherWorkspace); err != nil {
		t.Fatal(err)
	}
	request(approverID, http.StatusNotFound)
}

func TestGraphGenerationReportsProviderFailuresAndDeadline(t *testing.T) {
	agentID, approverID := graphGenerationTestIdentity(t)
	for name, status := range map[string]int{"provider error": http.StatusBadGateway, "deadline": http.StatusGatewayTimeout} {
		t.Run(name, func(t *testing.T) {
			h := newGraphGenerationTestHandler(t, "")
			h.LLM = llm.New(llm.Config{APIKey: "fake-key", MaxRetries: -1, HTTPClient: graphGenerationTestHTTPClient(func(r *http.Request) (*http.Response, error) {
				if name == "deadline" {
					return nil, context.DeadlineExceeded
				}
				return &http.Response{StatusCode: http.StatusInternalServerError, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"PRIVATE_UPSTREAM_DETAIL"}}`)), Request: r}, nil
			})})
			w := httptest.NewRecorder()
			graphGenerationAPI(t, h).GenerateGraph(w, newRequest(http.MethodPost, "/api/graphs/generate", map[string]string{"prompt": "Plan", "agent_id": agentID, "approver_id": approverID}))
			if w.Code != status || strings.Contains(w.Body.String(), "PRIVATE_") {
				t.Fatalf("provider failure: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
