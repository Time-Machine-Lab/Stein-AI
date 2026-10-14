package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/graph"
)

const (
	graphGenerationPromptBytes   = 16000
	graphGenerationTimeout       = 90 * time.Second
	graphGenerationMaxConcurrent = 4
	graphGenerationCooldown      = 10 * time.Second
)

// Graph generation is a bounded authoring utility. It never executes an agent,
// persists a draft, publishes a version, or starts a run.
type graphGenerationRequest struct {
	Prompt            string                  `json:"prompt"`
	AgentID           string                  `json:"agent_id"`
	ApproverID        string                  `json:"approver_id"`
	CurrentDefinition *graph.Definition       `json:"current_definition,omitempty"`
	Messages          []graphAuthoringMessage `json:"messages,omitempty"`
}

type graphAuthoringMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type generatedGraphStep struct {
	Key            string                        `json:"node_key"`
	Instructions   string                        `json:"instructions"`
	Inputs         map[string]graph.InputBinding `json:"inputs"`
	OutputSchema   json.RawMessage               `json:"output_schema"`
	ExecutionMode  graph.ExecutionMode           `json:"execution_mode"`
	MaxAttempts    int                           `json:"max_attempts"`
	TimeoutSeconds int                           `json:"timeout_seconds"`
}

type generatedGraphPlan struct {
	Name        string               `json:"name"`
	InputSchema json.RawMessage      `json:"input_schema"`
	Steps       []generatedGraphStep `json:"steps"`
}

type graphGenerationSlot struct {
	inFlight  bool
	expiresAt time.Time
}

// The zero value is ready for use, including self-host deployments without
// Redis. Both active requests and retained actor cooldowns have fixed limits.
type graphGenerationGate struct {
	mu     sync.Mutex
	active int
	slots  map[string]graphGenerationSlot
}

func (g *graphGenerationGate) acquire(key string) (func(), bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	for actor, slot := range g.slots {
		if !slot.inFlight && !now.Before(slot.expiresAt) {
			delete(g.slots, actor)
		}
	}
	if _, exists := g.slots[key]; exists || g.active >= graphGenerationMaxConcurrent || len(g.slots) >= 256 {
		return nil, false
	}
	if g.slots == nil {
		g.slots = make(map[string]graphGenerationSlot)
	}
	g.slots[key] = graphGenerationSlot{inFlight: true}
	g.active++
	return func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.active--
		g.slots[key] = graphGenerationSlot{expiresAt: time.Now().Add(graphGenerationCooldown)}
	}, true
}

func (h *Handler) GetGraphGenerationOptions(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	if _, ok = h.workspaceMember(w, r, ws); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": h.LLM != nil && h.LLM.Enabled() && h.graphGeneration != nil})
}

func (h *Handler) GenerateGraph(w http.ResponseWriter, r *http.Request) {
	ws, user, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	if _, ok = h.workspaceMember(w, r, ws); !ok {
		return
	}
	if h.LLM == nil || !h.LLM.Enabled() || h.graphGeneration == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "generation_unavailable", "AI Graph creation is not configured. Ask an administrator to configure the server AI provider, or create a Graph manually.")
		return
	}
	var req graphGenerationRequest
	r.Body = http.MaxBytesReader(w, r.Body, 512<<10)
	if !readGraphBody(w, r, &req) {
		return
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" || len(req.Prompt) > graphGenerationPromptBytes {
		writeErrorCode(w, http.StatusBadRequest, "invalid_request", "Describe the workflow in 1 to 16000 bytes.")
		return
	}
	if err := validateGraphAuthoringHistory(req.Messages); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}
	approverID, ok := parseUUIDOrBadRequest(w, req.ApproverID, "approver_id")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), graphGenerationTimeout)
	defer cancel()
	agent, err := h.Queries.GetAgent(ctx, agentID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (uuidToString(agent.WorkspaceID) != ws || agent.ArchivedAt.Valid) {
		writeErrorCode(w, http.StatusNotFound, "not_found", "The selected agent is unavailable in this workspace.")
		return
	}
	if err != nil {
		writeGraphError(w, err)
		return
	}
	if !h.canInvokeAgent(ctx, agent, "member", user, user, ws) {
		writeErrorCode(w, http.StatusForbidden, "forbidden", "You do not have permission to use the selected agent.")
		return
	}
	if agent.RuntimeID.Valid {
		runtime, runtimeErr := h.Queries.GetAgentRuntime(ctx, agent.RuntimeID)
		if errors.Is(runtimeErr, pgx.ErrNoRows) || runtimeErr == nil && uuidToString(runtime.WorkspaceID) != ws {
			writeErrorCode(w, http.StatusConflict, "capability_missing", "The selected agent's runtime is unavailable in this workspace.")
			return
		}
		if runtimeErr != nil {
			writeGraphError(w, runtimeErr)
			return
		}
	}
	approver, err := h.Queries.GetMember(ctx, approverID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && uuidToString(approver.WorkspaceID) != ws {
		writeErrorCode(w, http.StatusNotFound, "not_found", "The selected approver is unavailable in this workspace.")
		return
	}
	if err != nil {
		writeGraphError(w, err)
		return
	}
	// Reuse publication eligibility checks (including model/config/plugin
	// constraints). Release the read transaction before the upstream request.
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeGraphError(w, err)
		return
	}
	_, err = h.prepareGraphNode(ctx, tx, ws, user, graph.Node{Type: graph.NodeTypeAgent, AgentID: uuidToString(agent.ID)})
	rollbackErr := tx.Rollback(ctx)
	if err != nil {
		writeGraphError(w, err)
		return
	}
	if rollbackErr != nil {
		writeGraphError(w, rollbackErr)
		return
	}
	contextText, actors, err := h.graphAuthoringContext(ctx, ws, user, req)
	if err != nil {
		writeGraphError(w, err)
		return
	}
	release, allowed := h.graphGeneration.acquire(ws + ":" + user)
	if !allowed {
		w.Header().Set("Retry-After", "10")
		writeErrorCode(w, http.StatusTooManyRequests, "generation_busy", "AI Graph creation is busy. Wait a few seconds before trying again.")
		return
	}
	defer release()
	// Only user-authored graph content and conversation are sent. Actor IDs are
	// replaced with aliases; agent secrets, skills and runtime config stay local.
	raw, err := h.LLM.GenerateJSON(ctx, "", graphGenerationV2SystemPrompt, contextText, 0.2, 8192)
	if err != nil {
		slog.Warn("Graph draft generation failed", "workspace_id", ws, "error", err)
		if errors.Is(err, context.DeadlineExceeded) {
			writeErrorCode(w, http.StatusGatewayTimeout, "generation_timeout", "AI Graph creation timed out. Try a shorter description.")
		} else {
			writeErrorCode(w, http.StatusBadGateway, "generation_failed", "AI Graph creation failed. Try again or create the Graph manually.")
		}
		return
	}
	definition, err := parseGeneratedGraphActors(raw, actors)
	if err != nil {
		writeErrorCode(w, http.StatusBadGateway, "generation_invalid", "AI returned an invalid workflow. Try again with a more specific description.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"definition": definition})
}

func parseGeneratedGraph(raw, agentID, approverID string) (graph.Definition, error) {
	return parseGeneratedGraphActors(raw, graphAuthoringActors{Agents: map[string]string{"agent_default": agentID}, Approvers: map[string]string{"approver_default": approverID}})
}

func parseGeneratedGraphActors(raw string, actors graphAuthoringActors) (graph.Definition, error) {
	var result graph.Definition
	if len(raw) > graph.MaxDefinitionBytes {
		return result, errors.New("generated plan exceeds size limit")
	}
	// Reuse the Graph decoder's duplicate-key, depth and exact-number limits
	// before decoding the narrower authoring shape.
	canonical, _, err := graph.CanonicalJSON(json.RawMessage(raw))
	if err != nil {
		return result, err
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(canonical, &envelope); err != nil {
		return result, err
	}
	if _, versioned := envelope["schema_version"]; versioned {
		decoder := json.NewDecoder(strings.NewReader(string(canonical)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&result); err != nil {
			return result, err
		}
		if err := graphTransformActors(&result, actors.Agents, actors.Approvers); err != nil {
			return result, err
		}
		return result, graph.ValidateDefinition(result)
	}
	var plan generatedGraphPlan
	decoder := json.NewDecoder(strings.NewReader(string(canonical)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return result, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return result, errors.New("expected one generated plan")
	}
	if len(plan.Steps) < 1 || len(plan.Steps) > graph.MaxAgentNodes {
		return result, errors.New("generated plan requires 1 to 16 steps")
	}
	result = graph.Definition{SchemaVersion: graph.SchemaVersion, Name: plan.Name, InputSchema: plan.InputSchema, Nodes: []graph.Node{{Key: "start", Type: graph.NodeTypeStart}}}
	for _, step := range plan.Steps {
		result.Nodes = append(result.Nodes, graph.Node{Key: step.Key, Type: graph.NodeTypeAgent, AgentID: actors.Agents["agent_default"], Instructions: step.Instructions, Inputs: step.Inputs, OutputSchema: step.OutputSchema, ExecutionMode: step.ExecutionMode, MaxAttempts: step.MaxAttempts, TimeoutSeconds: step.TimeoutSeconds})
	}
	result.Nodes = append(result.Nodes, graph.Node{Key: "approval", Type: graph.NodeTypeApproval, ApproverID: actors.Approvers["approver_default"]}, graph.Node{Key: "end", Type: graph.NodeTypeEnd})
	for i := 1; i < len(result.Nodes); i++ {
		result.Edges = append(result.Edges, graph.Edge{From: result.Nodes[i-1].Key, To: result.Nodes[i].Key})
	}
	return result, graph.ValidateDefinition(result)
}
