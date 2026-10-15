package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The Graph creation conversation reuses the agent-creation studio's machinery:
// a hidden kind='system' carrier agent, a chat_session, and the working state in
// the client's own Graph draft. What these tests hold down is the part the
// server actually owns — that the carrier is created correctly, that it never
// escapes into a user-facing list, that it dies with its session, and that the
// executor the caller picked is gated the same way the old authoring path gated
// it.

const graphBuilderTestModel = "graph-builder-test-model"

// deleteGraphBuilderCarriers removes every carrier this file's tests create.
// Registration is per test rather than global so a failure in one does not leave
// rows behind for the next.
func deleteGraphBuilderCarriers(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM agent
			WHERE workspace_id = $1 AND kind = 'system' AND system_key LIKE 'graph_builder:%'
		`, testWorkspaceID)
	})
}

// newGraphBuilderExecutor inserts an agent that can host a design conversation:
// workspace-invocable, bound to the fixture runtime, with a model set. The model
// matters because the carrier clones it, and the clone is what the test asserts.
func newGraphBuilderExecutor(t *testing.T, name string) string {
	t.Helper()

	runtimeID := newTestRuntime(t, "Graph builder executor runtime "+name, "online")
	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, mcp_config, model
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 'public_to', 1, $4,
		        '', '{}'::jsonb, '[]'::jsonb, '[]'::jsonb, $5)
		RETURNING id
	`, testWorkspaceID, name, runtimeID, testUserID, graphBuilderTestModel).Scan(&agentID); err != nil {
		t.Fatalf("create Graph builder executor: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id)
		VALUES ($1, 'workspace', $2)
		ON CONFLICT (agent_id, target_type, target_id) DO NOTHING
	`, agentID, testWorkspaceID); err != nil {
		t.Fatalf("seed executor invocation target: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}

// graphBuilderApproverID returns a member of the fixture workspace who can be
// named as the default approver.
func graphBuilderApproverID(t *testing.T) string {
	t.Helper()
	var approverID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2`,
		testWorkspaceID, testUserID).Scan(&approverID); err != nil {
		t.Fatalf("load approver member: %v", err)
	}
	return approverID
}

// startGraphBuilderSession calls the endpoint and decodes the response.
func startGraphBuilderSession(t *testing.T, body map[string]any) (*httptest.ResponseRecorder, CreateGraphBuilderSessionResponse) {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateGraphBuilderSession(w, newRequest(http.MethodPost, "/api/graph-builder/sessions", body))
	var response CreateGraphBuilderSessionResponse
	if w.Code == http.StatusCreated {
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode create response: %v", err)
		}
	}
	return w, response
}

// newGraphBuilderSession starts a conversation on a fresh executor and returns
// the response plus that executor's id.
func newGraphBuilderSession(t *testing.T) (CreateGraphBuilderSessionResponse, string) {
	t.Helper()
	deleteGraphBuilderCarriers(t)
	executorID := newGraphBuilderExecutor(t, "Graph builder executor "+uuid.NewString())
	w, response := startGraphBuilderSession(t, map[string]any{
		"default_agent_id": executorID,
		"approver_id":      graphBuilderApproverID(t),
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateGraphBuilderSession: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	return response, executorID
}

func TestCreateGraphBuilderSessionClonesExecutorRuntimeIntoHiddenCarrier(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	session, executorID := newGraphBuilderSession(t)
	ctx := context.Background()

	if session.SessionID == "" || session.BuilderAgentID == "" || session.RuntimeID == "" {
		t.Fatalf("missing identifiers in create response: %+v", session)
	}
	if session.DefaultAgentID != executorID {
		t.Fatalf("create response echoed the wrong executor: got %q want %q", session.DefaultAgentID, executorID)
	}

	var kind, systemKey, model, instructions, visibility, permissionMode string
	var carrierRuntimeID, ownerID string
	if err := testPool.QueryRow(ctx, `
		SELECT kind, system_key, model, instructions, visibility, permission_mode,
		       runtime_id, owner_id
		FROM agent WHERE id = $1
	`, session.BuilderAgentID).Scan(&kind, &systemKey, &model, &instructions,
		&visibility, &permissionMode, &carrierRuntimeID, &ownerID); err != nil {
		t.Fatalf("load carrier agent: %v", err)
	}
	if kind != "system" {
		t.Fatalf("carrier kind = %q, want system", kind)
	}
	if !strings.HasPrefix(systemKey, graphBuilderSystemKeyPrefix) {
		t.Fatalf("carrier system_key = %q, want the %q namespace", systemKey, graphBuilderSystemKeyPrefix)
	}
	// The flow id is what makes the namespace a set rather than one key, so the
	// suffix must be a fresh identifier and not a constant.
	flowID := strings.TrimPrefix(systemKey, graphBuilderSystemKeyPrefix)
	if _, err := uuid.Parse(flowID); err != nil {
		t.Fatalf("carrier system_key suffix %q is not a UUID: %v", flowID, err)
	}
	if model != graphBuilderTestModel {
		t.Fatalf("carrier model = %q, want the executor's %q", model, graphBuilderTestModel)
	}
	if instructions != graphBuilderInstructions {
		t.Fatal("carrier instructions are not the Graph builder contract")
	}
	if visibility != "private" || permissionMode != "private" {
		t.Fatalf("carrier is not private: visibility=%q permission_mode=%q", visibility, permissionMode)
	}
	if ownerID != testUserID {
		t.Fatalf("carrier owner = %q, want the caller %q", ownerID, testUserID)
	}

	// The carrier runs where the executor runs. It is a copy of that execution
	// environment with different instructions, never a second runtime.
	var executorRuntimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, executorID).Scan(&executorRuntimeID); err != nil {
		t.Fatalf("load executor runtime: %v", err)
	}
	if carrierRuntimeID != executorRuntimeID {
		t.Fatalf("carrier runtime = %q, want the executor's %q", carrierRuntimeID, executorRuntimeID)
	}
	if session.RuntimeID != executorRuntimeID {
		t.Fatalf("response runtime = %q, want %q", session.RuntimeID, executorRuntimeID)
	}

	var sessionAgentID, creatorID, title, status string
	if err := testPool.QueryRow(ctx, `
		SELECT agent_id, creator_id, title, status FROM chat_session WHERE id = $1
	`, session.SessionID).Scan(&sessionAgentID, &creatorID, &title, &status); err != nil {
		t.Fatalf("load chat session: %v", err)
	}
	if sessionAgentID != session.BuilderAgentID {
		t.Fatalf("chat session is not bound to the carrier: agent_id=%q carrier=%q", sessionAgentID, session.BuilderAgentID)
	}
	if creatorID != testUserID {
		t.Fatalf("chat session creator = %q, want %q", creatorID, testUserID)
	}
	if title != "Create a Graph" {
		t.Fatalf("chat session title = %q", title)
	}
	if status != "active" {
		t.Fatalf("chat session status = %q, want active", status)
	}
}

func TestGraphBuilderCarrierIsInvisibleToAgentSurfaces(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	session, _ := newGraphBuilderSession(t)

	w := httptest.NewRecorder()
	testHandler.ListAgents(w, newRequest(http.MethodGet, "/api/agents", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgents: %d: %s", w.Code, w.Body.String())
	}
	var listed []AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode agent list: %v", err)
	}
	for _, agent := range listed {
		if agent.ID == session.BuilderAgentID {
			t.Fatal("Graph builder carrier leaked into the user-facing agent list")
		}
	}

	// Knowing the id must not open the public detail, update or archive loaders.
	w = httptest.NewRecorder()
	testHandler.GetAgent(w, withURLParam(newRequest(http.MethodGet, "/api/agents/"+session.BuilderAgentID, nil), "id", session.BuilderAgentID))
	if w.Code != http.StatusNotFound {
		t.Fatalf("GetAgent(carrier): expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestGraphBuilderSessionDeleteIsScopedToItsOwnCarrier is the reason the carrier
// namespace carries a per-flow id at all. One key for the whole workspace would
// make the second design conversation share (or overwrite) the first carrier,
// and deleting either session would take the other's conversation down with it.
func TestGraphBuilderSessionDeleteIsScopedToItsOwnCarrier(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	first, _ := newGraphBuilderSession(t)
	second, _ := newGraphBuilderSession(t)
	ctx := context.Background()

	if first.BuilderAgentID == second.BuilderAgentID {
		t.Fatal("two Graph design flows shared one carrier agent")
	}
	if first.SessionID == second.SessionID {
		t.Fatal("each Graph design flow must receive a fresh chat session")
	}

	flowIDs := map[string]bool{}
	for _, session := range []CreateGraphBuilderSessionResponse{first, second} {
		var systemKey string
		if err := testPool.QueryRow(ctx, `SELECT system_key FROM agent WHERE id = $1`, session.BuilderAgentID).Scan(&systemKey); err != nil {
			t.Fatalf("load carrier key: %v", err)
		}
		flowID := strings.TrimPrefix(systemKey, graphBuilderSystemKeyPrefix)
		if _, err := uuid.Parse(flowID); err != nil {
			t.Fatalf("carrier key %q is not namespaced by a UUID flow id: %v", systemKey, err)
		}
		flowIDs[flowID] = true
	}
	if len(flowIDs) != 2 {
		t.Fatalf("two design flows produced the same carrier namespace: %v", flowIDs)
	}

	w := httptest.NewRecorder()
	req := withURLParams(newRequest(http.MethodDelete, "/api/chat/sessions/"+first.SessionID, nil), "sessionId", first.SessionID)
	req = withChatTestWorkspaceCtx(t, req)
	testHandler.DeleteChatSession(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteChatSession(first): expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var secondSurvives int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent WHERE id = $1`, second.BuilderAgentID).Scan(&secondSurvives); err != nil {
		t.Fatalf("count second carrier: %v", err)
	}
	if secondSurvives != 1 {
		t.Fatal("deleting one design conversation deleted the other's carrier")
	}
	var secondSessionSurvives int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM chat_session WHERE id = $1`, second.SessionID).Scan(&secondSessionSurvives); err != nil {
		t.Fatalf("count second session: %v", err)
	}
	if secondSessionSurvives != 1 {
		t.Fatal("deleting one design conversation deleted the other's session")
	}
}

func TestCreateGraphBuilderSessionRejectsUnusableExecutor(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	deleteGraphBuilderCarriers(t)
	ctx := context.Background()

	healthy := newGraphBuilderExecutor(t, "Graph builder healthy "+uuid.NewString())

	archived := newGraphBuilderExecutor(t, "Graph builder archived "+uuid.NewString())
	if _, err := testPool.Exec(ctx, `UPDATE agent SET archived_at = now() WHERE id = $1`, archived); err != nil {
		t.Fatalf("archive executor: %v", err)
	}

	offline := newGraphBuilderExecutor(t, "Graph builder offline "+uuid.NewString())
	offlineRuntime := newTestRuntime(t, "Graph builder offline runtime "+uuid.NewString(), "offline")
	if _, err := testPool.Exec(ctx, `UPDATE agent SET runtime_id = $1 WHERE id = $2`, offlineRuntime, offline); err != nil {
		t.Fatalf("repoint offline executor: %v", err)
	}

	// A private agent owned by someone else, with no invocation target: the
	// caller can see the id but may not run it.
	var foreignOwner string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (email, name) VALUES ($1, 'Graph builder foreign owner') RETURNING id
	`, "graph-builder-foreign-"+uuid.NewString()+"@example.test").Scan(&foreignOwner); err != nil {
		t.Fatalf("insert foreign owner: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, foreignOwner) })
	var restricted string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, mcp_config, model
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 'private', 1, $4,
		        '', '{}'::jsonb, '[]'::jsonb, '[]'::jsonb, $5)
		RETURNING id
	`, testWorkspaceID, "Graph builder restricted "+uuid.NewString(),
		handlerTestRuntimeID(t), foreignOwner, graphBuilderTestModel).Scan(&restricted); err != nil {
		t.Fatalf("insert restricted executor: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, restricted) })

	var foreignWorkspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Graph builder foreign workspace', 'graph-builder-foreign-' || gen_random_uuid()::text,
		        'Foreign workspace', 'GBF')
		RETURNING id
	`).Scan(&foreignWorkspaceID); err != nil {
		t.Fatalf("insert foreign workspace: %v", err)
	}
	var crossWorkspaceAgent string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			visibility, permission_mode, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, mcp_config, model
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, 'workspace', 'public_to', 1, $3,
		        '', '{}'::jsonb, '[]'::jsonb, '[]'::jsonb, $4)
		RETURNING id
	`, foreignWorkspaceID, "Graph builder cross workspace", testUserID, graphBuilderTestModel).Scan(&crossWorkspaceAgent); err != nil {
		t.Fatalf("insert cross-workspace executor: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, crossWorkspaceAgent)
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, foreignWorkspaceID)
	})

	approverID := graphBuilderApproverID(t)
	cases := []struct {
		name       string
		executorID string
		wantStatus int
		wantBody   string
	}{
		{"unknown", uuid.NewString(), http.StatusNotFound, "not_found"},
		{"archived", archived, http.StatusNotFound, "not_found"},
		{"cross-workspace", crossWorkspaceAgent, http.StatusNotFound, "not_found"},
		{"not-invocable", restricted, http.StatusForbidden, "forbidden"},
		// AgentReadiness reports an offline runtime as waitable, and the endpoint
		// refuses rather than queue: a design conversation that cannot start for
		// minutes is a worse answer than "bring that machine online".
		{"offline-runtime", offline, http.StatusUnprocessableEntity, "agent_unavailable"},
		{"malformed", "not-a-uuid", http.StatusBadRequest, "invalid default_agent_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := startGraphBuilderSession(t, map[string]any{
				"default_agent_id": tc.executorID,
				"approver_id":      approverID,
			})
			if w.Code != tc.wantStatus {
				t.Fatalf("expected %d, got %d: %s", tc.wantStatus, w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.wantBody) {
				t.Fatalf("expected %q in body, got %s", tc.wantBody, w.Body.String())
			}
		})
	}

	// The healthy executor still works after every refusal above, so the
	// refusals were the gates and not a broken fixture.
	if _, err := testPool.Exec(ctx, `SELECT 1 FROM agent WHERE id = $1`, healthy); err != nil {
		t.Fatalf("healthy fixture vanished: %v", err)
	}
	w, _ := startGraphBuilderSession(t, map[string]any{
		"default_agent_id": healthy,
		"approver_id":      approverID,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("healthy executor refused: %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateGraphBuilderSessionValidatesPickers(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	deleteGraphBuilderCarriers(t)
	executorID := newGraphBuilderExecutor(t, "Graph builder picker "+uuid.NewString())
	approverID := graphBuilderApproverID(t)
	ctx := context.Background()

	cases := []struct {
		name string
		body map[string]any
		want int
		code string
	}{
		{
			name: "missing executor",
			body: map[string]any{"approver_id": approverID},
			want: http.StatusBadRequest, code: "default_agent_id is required",
		},
		{
			name: "missing approver",
			body: map[string]any{"default_agent_id": executorID},
			want: http.StatusBadRequest, code: "approver_id is required",
		},
		{
			name: "malformed approver",
			body: map[string]any{"default_agent_id": executorID, "approver_id": "not-a-uuid"},
			want: http.StatusBadRequest, code: "invalid approver_id",
		},
		{
			name: "unknown approver",
			body: map[string]any{"default_agent_id": executorID, "approver_id": uuid.NewString()},
			want: http.StatusNotFound, code: "not_found",
		},
		{
			name: "scope entry outside the workspace",
			body: map[string]any{
				"default_agent_id": executorID,
				"approver_id":      approverID,
				"agent_scope":      []string{uuid.NewString()},
			},
			want: http.StatusNotFound, code: "not_found",
		},
		{
			name: "scope entry malformed",
			body: map[string]any{
				"default_agent_id": executorID,
				"approver_id":      approverID,
				"agent_scope":      []string{"not-a-uuid"},
			},
			want: http.StatusBadRequest, code: "invalid agent_scope[0]",
		},
		{
			name: "scope over the directory limit",
			body: map[string]any{
				"default_agent_id": executorID,
				"approver_id":      approverID,
				"agent_scope":      make([]string, graphBuilderDirectoryLimit+1),
			},
			want: http.StatusBadRequest, code: "invalid_request",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := startGraphBuilderSession(t, tc.body)
			if w.Code != tc.want {
				t.Fatalf("expected %d, got %d: %s", tc.want, w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.code) {
				t.Fatalf("expected code %q, got %s", tc.code, w.Body.String())
			}
		})
	}

	// The over-limit case is refused before any id is parsed, so the entries can
	// be empty strings; the limit itself is what a client must respect, since
	// the directory it describes is what the model is shown.
	var carriers int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent WHERE workspace_id = $1 AND kind = 'system' AND system_key LIKE 'graph_builder:%'`,
		testWorkspaceID).Scan(&carriers); err != nil {
		t.Fatalf("count carriers: %v", err)
	}
	if carriers != 0 {
		t.Fatalf("a refused request created %d carrier(s)", carriers)
	}
}

func TestDeleteGraphBuilderSessionRemovesCarrierWithoutTouchingTheExecutor(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	session, executorID := newGraphBuilderSession(t)
	ctx := context.Background()

	w := httptest.NewRecorder()
	req := withURLParams(newRequest(http.MethodDelete, "/api/chat/sessions/"+session.SessionID, nil), "sessionId", session.SessionID)
	req = withChatTestWorkspaceCtx(t, req)
	testHandler.DeleteChatSession(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteChatSession(carrier): expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var carriers int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent WHERE id = $1`, session.BuilderAgentID).Scan(&carriers); err != nil {
		t.Fatalf("count carrier: %v", err)
	}
	// The delete path is shared with the agent-creation studio, and its guard is
	// a namespace LIKE. A carrier the pattern misses would be orphaned here:
	// invisible to every list and reachable by no prune but workspace teardown.
	if carriers != 0 {
		t.Fatal("carrier survived deletion of its own chat session")
	}

	var executorSurvives int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent WHERE id = $1`, executorID).Scan(&executorSurvives); err != nil {
		t.Fatalf("count executor: %v", err)
	}
	if executorSurvives != 1 {
		t.Fatal("deleting a design conversation deleted the user's executor agent")
	}
}

func TestDeleteOrdinaryChatSessionLeavesItsAgentAlone(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	// The widened namespace guard is the risk this covers: if the delete query
	// lost its kind/system_key predicate, this ordinary session's agent would be
	// deleted with it.
	agentID := createHandlerTestAgent(t, "Ordinary chat agent "+uuid.NewString(), []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	w := httptest.NewRecorder()
	req := withURLParams(newRequest(http.MethodDelete, "/api/chat/sessions/"+sessionID, nil), "sessionId", sessionID)
	req = withChatTestWorkspaceCtx(t, req)
	testHandler.DeleteChatSession(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteChatSession: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var remaining int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent WHERE id = $1`, agentID).Scan(&remaining); err != nil {
		t.Fatalf("count agent: %v", err)
	}
	if remaining != 1 {
		t.Fatal("deleting an ordinary chat session deleted its user-authored agent")
	}
}

func TestListGraphBuilderSessionsIsCreatorScopedAndGraphOnly(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	session, _ := newGraphBuilderSession(t)
	ctx := context.Background()

	seedMessage := func(sessionID, role, content string) {
		t.Helper()
		if _, err := testPool.Exec(ctx, `
			INSERT INTO chat_message (chat_session_id, role, content)
			VALUES ($1, $2, $3)
		`, sessionID, role, content); err != nil {
			t.Fatalf("seed chat message: %v", err)
		}
	}
	seedMessage(session.SessionID, "user", "Draft a release workflow")

	// A Graph conversation with no messages is not yet a conversation. Nothing
	// about a Graph draft reaches the server before the first turn, so an empty
	// session carries no information to recover.
	empty, _ := newGraphBuilderSession(t)

	// The agent-creation studio's carrier sits in the same table behind a
	// different namespace. It must not appear in a Graph list.
	builderSession := newBuilderSession(t)
	seedMessage(builderSession.SessionID, "user", "Build me a reviewer agent")

	// An ordinary chat session is not a design conversation either.
	ordinaryAgent := createHandlerTestAgent(t, "Ordinary list agent "+uuid.NewString(), []byte("[]"))
	ordinarySession := createHandlerTestChatSession(t, ordinaryAgent)
	seedMessage(ordinarySession, "user", "hello")

	// A second creator's Graph conversation stays private to them: the same rule
	// loadChatSessionForUser enforces on every other chat read.
	var foreignUserID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (email, name) VALUES ($1, 'Graph builder foreign creator') RETURNING id
	`, "graph-builder-creator-"+uuid.NewString()+"@example.test").Scan(&foreignUserID); err != nil {
		t.Fatalf("insert foreign creator: %v", err)
	}
	var foreignMemberID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member') RETURNING id
	`, testWorkspaceID, foreignUserID).Scan(&foreignMemberID); err != nil {
		t.Fatalf("insert foreign member: %v", err)
	}
	var foreignAgentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, owner_id, kind, system_key, visibility, permission_mode, max_concurrent_tasks, runtime_mode, runtime_config)
		VALUES ($1, $2, $3, 'system', $4, 'private', 'private', 1, 'cloud', '{}'::jsonb)
		RETURNING id
	`, testWorkspaceID, ".multica-graph-builder-"+uuid.NewString(), foreignUserID,
		graphBuilderSystemKeyPrefix+uuid.NewString()).Scan(&foreignAgentID); err != nil {
		t.Fatalf("insert foreign carrier: %v", err)
	}
	var foreignSessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title)
		VALUES ($1, $2, $3, 'Create a Graph') RETURNING id
	`, testWorkspaceID, foreignAgentID, foreignUserID).Scan(&foreignSessionID); err != nil {
		t.Fatalf("insert foreign session: %v", err)
	}
	seedMessage(foreignSessionID, "user", "their draft")
	t.Cleanup(func() {
		// chat_session -> agent is not a cascade in this direction, so both go.
		testPool.Exec(ctx, `DELETE FROM chat_session WHERE id = $1`, foreignSessionID)
		testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, foreignAgentID)
		testPool.Exec(ctx, `DELETE FROM member WHERE id = $1`, foreignMemberID)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, foreignUserID)
	})

	w := httptest.NewRecorder()
	testHandler.ListGraphBuilderSessions(w, newRequest(http.MethodGet, "/api/graph-builder/sessions", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListGraphBuilderSessions: %d: %s", w.Code, w.Body.String())
	}
	var response ListGraphBuilderSessionsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode list response: %v", err)
	}

	seen := map[string]GraphBuilderSessionSummary{}
	for _, summary := range response.Sessions {
		seen[summary.SessionID] = summary
	}
	mine, ok := seen[session.SessionID]
	if !ok {
		t.Fatalf("the caller's own Graph conversation is missing: %+v", response.Sessions)
	}
	if mine.LastMessageContent != "Draft a release workflow" || mine.LastMessageRole != "user" {
		t.Fatalf("last message was not carried through: %+v", mine)
	}
	if mine.RuntimeID == "" || mine.Title == "" {
		t.Fatalf("summary is missing identity fields: %+v", mine)
	}
	for _, excluded := range []struct{ what, sessionID string }{
		{"a Graph conversation with no messages", empty.SessionID},
		{"an agent-creation conversation", builderSession.SessionID},
		{"an ordinary chat session", ordinarySession},
		{"another creator's Graph conversation", foreignSessionID},
	} {
		if _, present := seen[excluded.sessionID]; present {
			t.Errorf("list included %s", excluded.what)
		}
	}
}
