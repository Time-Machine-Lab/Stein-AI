package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/graph"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// All existing custom_env values are treated as credentials, as on the Agent
// resource API. Only their key allowlist is persisted in a published version.
type graphFrozenAgent struct {
	TaskAgentData
	EnvironmentKeys        []string                 `json:"environment_keys,omitempty"`
	CredentialPaths        [][]string               `json:"credential_paths,omitempty"`
	RuntimeCredentialPaths [][]string               `json:"runtime_credential_paths,omitempty"`
	Plugins                []service.GraphPluginPin `json:"plugins,omitempty"`
}

func parseGraphUUID(value string) (pgtype.UUID, error) {
	id, err := util.ParseUUID(value)
	if err != nil {
		return id, &service.GraphError{Code: "invalid_id", Message: "invalid Graph resource UUID"}
	}
	return id, nil
}

func graphConfigurationError(message string) error {
	return &service.GraphError{Code: "snapshot_invalid", Message: message}
}

func graphSecretKey(key string) bool {
	k := strings.ToLower(strings.ReplaceAll(key, "_", ""))
	return strings.Contains(k, "secret") || strings.Contains(k, "password") || strings.Contains(k, "token") || strings.Contains(k, "apikey") || k == "authorization" || k == "credential"
}

// Sensitive MCP leaves are reference bindings. URL query values are also
// bindings; the URL authority/path and query keys remain frozen for validation.
func graphFreezeMCP(value any, path []string, refs *[][]string) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			p := append(append([]string{}, path...), key)
			parent := ""
			if len(path) > 0 {
				parent = strings.ToLower(path[len(path)-1])
			}
			if graphSecretKey(key) || parent == "env" || parent == "headers" {
				*refs = append(*refs, p)
				out[key] = nil
				continue
			}
			if text, ok := v[key].(string); ok && strings.EqualFold(key, "url") {
				if u, err := url.Parse(text); err == nil && (u.User != nil || u.RawQuery != "") {
					*refs = append(*refs, p)
					u.User = nil
					q := u.Query()
					for k := range q {
						q[k] = []string{""}
					}
					u.RawQuery = q.Encode()
					out[key] = u.String()
					continue
				}
			}
			out[key] = graphFreezeMCP(v[key], p, refs)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = graphFreezeMCP(item, append(append([]string{}, path...), fmt.Sprint(i)), refs)
		}
		return out
	default:
		return value
	}
}

func graphJSONPath(root any, path []string) (any, bool) {
	value := root
	for _, key := range path {
		switch v := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = v[key]
			if !ok {
				return nil, false
			}
		case []any:
			i, err := strconv.Atoi(key)
			if err != nil || i < 0 || i >= len(v) {
				return nil, false
			}
			value = v[i]
		default:
			return nil, false
		}
	}
	return value, true
}
func graphSetJSONPath(root any, path []string, value any) bool {
	if len(path) == 0 {
		return false
	}
	parent, ok := graphJSONPath(root, path[:len(path)-1])
	if !ok {
		return false
	}
	key := path[len(path)-1]
	switch v := parent.(type) {
	case map[string]any:
		v[key] = value
		return true
	case []any:
		i, err := strconv.Atoi(key)
		if err != nil || i < 0 || i >= len(v) {
			return false
		}
		v[i] = value
		return true
	}
	return false
}

func (h *Handler) graphResolvedMCP(ctx context.Context, q *db.Queries, agent db.Agent) (json.RawMessage, error) {
	config := json.RawMessage(agent.McpConfig)
	bound, err := q.ListEnabledAgentMcpServers(ctx, agent.ID)
	if err != nil {
		return nil, err
	}
	bindings := make([]WorkspaceMcpBinding, 0, len(bound))
	for _, server := range bound {
		bindings = append(bindings, WorkspaceMcpBinding{Name: server.Name, Config: json.RawMessage(server.Config)})
	}
	if len(bindings) > 0 {
		return ResolveAgentMcpConfig(bindings, config)
	}
	if len(config) == 0 {
		return json.RawMessage(`{}`), nil
	}
	return config, nil
}

func (h *Handler) prepareGraphNode(ctx context.Context, tx pgx.Tx, ws, user string, node graph.Node) (service.GraphNodeSnapshot, error) {
	var snapshot service.GraphNodeSnapshot
	q := h.Queries.WithTx(tx)
	agent, err := q.GetAgent(ctx, parseUUID(node.AgentID))
	if err != nil {
		return snapshot, err
	}
	if uuidToString(agent.WorkspaceID) != ws || agent.ArchivedAt.Valid {
		return snapshot, graphConfigurationError("agent is unavailable in this workspace")
	}
	if strings.TrimSpace(agent.Model.String) == "" {
		return snapshot, graphConfigurationError("Graph agents require an explicit model before publication")
	}
	if !h.canInvokeAgent(ctx, agent, "member", user, user, ws) {
		return snapshot, &service.GraphError{Code: "forbidden", Message: "publication requires invocation permission for every agent"}
	}
	frozen := graphFrozenAgent{TaskAgentData: TaskAgentData{ID: node.AgentID, Name: agent.Name, Instructions: agent.Instructions, Model: agent.Model.String, ThinkingLevel: agent.ThinkingLevel.String, ServiceTier: agent.ServiceTier.String, RuntimeConfig: agent.RuntimeConfig}}
	if agent.SystemKey.String == service.MikaSystemKey {
		frozen.Instructions = service.ComposeMikaInstructions(agent.Name, agent.Instructions)
	}
	if len(agent.CustomArgs) > 0 {
		if err = json.Unmarshal(agent.CustomArgs, &frozen.CustomArgs); err != nil {
			return snapshot, err
		}
	}
	if len(frozen.CustomArgs) > 0 {
		return snapshot, graphConfigurationError("Graph V1 does not support custom launch arguments")
	}
	if config := strings.TrimSpace(string(agent.RuntimeConfig)); config != "" && config != "{}" && config != "null" {
		return snapshot, graphConfigurationError("Graph V1 does not support custom runtime configuration")
	}
	var environment map[string]string
	if len(agent.CustomEnv) > 0 {
		if err = json.Unmarshal(agent.CustomEnv, &environment); err != nil {
			return snapshot, err
		}
	}
	for key := range environment {
		frozen.EnvironmentKeys = append(frozen.EnvironmentKeys, key)
	}
	sort.Strings(frozen.EnvironmentKeys)
	mcp, err := h.graphResolvedMCP(ctx, q, agent)
	if err != nil {
		return snapshot, err
	}
	var mcpValue any
	if err = json.Unmarshal(mcp, &mcpValue); err != nil {
		return snapshot, err
	}
	frozen.McpConfig, err = json.Marshal(graphFreezeMCP(mcpValue, nil, &frozen.CredentialPaths))
	if err != nil {
		return snapshot, err
	}
	if len(agent.RuntimeConfig) > 0 {
		var runtimeValue any
		if err = json.Unmarshal(agent.RuntimeConfig, &runtimeValue); err != nil {
			return snapshot, err
		}
		frozen.RuntimeConfig, err = json.Marshal(graphFreezeMCP(runtimeValue, nil, &frozen.RuntimeCredentialPaths))
		if err != nil {
			return snapshot, err
		}
	}
	// Skill source and supporting files are copied in the publication transaction.
	rows, err := q.ListAgentSkills(ctx, agent.ID)
	if err != nil {
		return snapshot, err
	}
	skills := make([]service.AgentSkillData, 0, len(rows))
	for _, row := range rows {
		skill := service.AgentSkillData{ID: uuidToString(row.ID), Name: row.Name, Description: row.Description, Content: row.Content}
		files, e := q.ListSkillFiles(ctx, row.ID)
		if e != nil {
			return snapshot, e
		}
		for _, file := range files {
			skill.Files = append(skill.Files, service.AgentSkillFileData{Path: file.Path, Content: file.Content})
		}
		skills = append(skills, skill)
	}
	pluginSkills, pins, err := h.PluginService.SnapshotGraphSkills(ctx, tx, agent.WorkspaceID, agent.ID)
	if err != nil {
		return snapshot, err
	}
	frozen.Plugins = pins
	skills = append(skills, pluginSkills...)
	skills = append(skills, graphBuiltinSkill())
	frozen.Skills, frozen.SkillRefs = service.BuildAgentSkillBundles(skills)
	for _, skill := range frozen.SkillRefs {
		snapshot.Skills = append(snapshot.Skills, graph.SkillSnapshot{ID: skill.Source + ":" + skill.ID, Hash: skill.Hash, BundleRef: skill.Source + ":" + skill.ID + "@" + skill.Hash})
	}
	snapshot.AgentConfig, err = json.Marshal(frozen)
	return snapshot, err
}

func graphBuiltinSkill() service.AgentSkillData {
	return service.AgentSkillData{ID: "builtin-graph-v1", Source: "builtin", Name: "multica-graph", Description: "Execute the current immutable Graph node and submit a structured result.", Content: `# Multica Graph node
Run multica graph node context to read the frozen node, input, output schema, attempt identity and repository candidate. Follow only this node's instructions. Comments are progress evidence and do not change the input.
Submit the result with multica graph node submit --result-file result.json. The result contains outcome (pass, environment_blocked or needs_changes), output matching the output schema, optional artifact references, diagnostics, and tested_commit_sha for verify_commit. The CLI fills the current identity and uses an idempotency key.
For an environmental blocker use multica graph node block --reason-file reason.json; this file is a nonempty diagnostics JSON object, for example {"reason":"credential unavailable"}. The CLI sets outcome=environment_blocked. For failed verification submit outcome=needs_changes; do not delegate, retry, change Issue status, create tasks or approve the run.
Post progress with multica graph node comment --body "Progress text"; this posts only to this node's child Issue and never changes its frozen input.
Use multica attachment upload <path> for files; it automatically binds the current task_id. Copy returned id, content_hash, size and media_type into an artifact with type=attachment_ref and producer_task_id from the attempt identity. Never submit a local path or signed URL as a durable artifact.
In produce_commit mode commit intended changes, leave the checkout clean, and declare the exact commit. Use refs/heads/multica/graphs/<run_id>/<node_key>/<attempt_no> as managed_ref. The daemon verifies and delivers the commit after successful provider exit. In verify_commit mode keep HEAD exactly at the supplied candidate and report tested_commit_sha. Preserve upstream artifact IDs and producer_task_id when referencing them.
Submitting stages a candidate. The platform accepts it only after the provider exits and artifact delivery is verified. Human approval is a separate platform operation.`}
}

func (h *Handler) authorizeGraphSnapshot(ctx context.Context, tx pgx.Tx, ws string, node graph.Node, snapshot service.GraphNodeSnapshot) error {
	var frozen graphFrozenAgent
	if err := json.Unmarshal(snapshot.AgentConfig, &frozen); err != nil {
		return err
	}
	return h.PluginService.ValidateGraphPluginPins(ctx, tx, parseUUID(ws), parseUUID(node.AgentID), frozen.Plugins)
}

func (h *Handler) authorizeGraphRun(ctx context.Context, tx pgx.Tx, ws, user string, definition graph.Definition, bindings map[string]string) error {
	q := h.Queries.WithTx(tx)
	if _, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: parseUUID(user), WorkspaceID: parseUUID(ws)}); err != nil {
		return &service.GraphError{Code: "authorization_revoked", Message: "initiator is no longer a workspace member"}
	}
	for _, node := range definition.Nodes {
		if node.Type != graph.NodeTypeAgent {
			continue
		}
		agentID, err := parseGraphUUID(node.AgentID)
		if err != nil {
			return err
		}
		runtimeID, err := parseGraphUUID(bindings[node.Key])
		if err != nil {
			return err
		}
		agent, err := q.GetAgent(ctx, agentID)
		if err != nil {
			return err
		}
		if uuidToString(agent.WorkspaceID) != ws || agent.ArchivedAt.Valid {
			return &service.GraphError{Code: "authorization_revoked", Message: "agent is unavailable"}
		}
		if !h.canInvokeAgent(ctx, agent, "member", user, user, ws) {
			return &service.GraphError{Code: "authorization_revoked", Message: "initiator can no longer invoke an agent"}
		}
		runtime, err := q.GetAgentRuntime(ctx, runtimeID)
		if err != nil {
			return err
		}
		if runtime.Provider != "codex" {
			return &service.GraphError{Code: "capability_missing", Message: "Graph V1 currently requires the isolated Codex runtime adapter"}
		}
		if runtime.ProfileID.Valid {
			return &service.GraphError{Code: "capability_missing", Message: "Graph V1 requires the built-in Codex runtime adapter"}
		}
		if uuidToString(runtime.WorkspaceID) != ws || runtime.Status != "online" || !runtimeHasCapability(runtime.Metadata, protocol.DaemonCapabilityGraphExecutionV1) {
			return &service.GraphError{Code: "capability_missing", Message: "every node requires an online runtime with graph-execution-v1"}
		}
		if runtime.Visibility == "private" && uuidToString(runtime.OwnerID) != user {
			return &service.GraphError{Code: "forbidden", Message: "runtime is private to another member"}
		}
		if agent.RuntimeID.Valid && agent.RuntimeID != runtime.ID {
			return &service.GraphError{Code: "runtime_mismatch", Message: "runtime differs from the agent binding"}
		}
	}
	return nil
}

func (h *Handler) prepareGraphRun(ctx context.Context, tx pgx.Tx, ws, user string, raw json.RawMessage, repository *graph.RepositoryInput) (json.RawMessage, error) {
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	q := h.Queries.WithTx(tx)
	workspace, err := q.GetWorkspace(ctx, parseUUID(ws))
	if err != nil {
		return nil, err
	}
	root["workspace_context"] = workspace.Context
	var projectID string
	if id, ok := root["project_id"].(string); ok {
		projectID = id
	}
	if projectID != "" {
		project, err := q.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: parseUUID(projectID), WorkspaceID: parseUUID(ws)})
		if err != nil {
			return nil, err
		}
		root["project_title"] = project.Title
		root["project_description"] = project.Description.String
	}
	if repository != nil {
		if repository.CommitSHA == "" {
			repository.CommitSHA = repository.BaseSHA
		}
		var repoURL string
		if projectID != "" {
			resources, err := q.ListProjectResources(ctx, parseUUID(projectID))
			if err != nil {
				return nil, err
			}
			for _, resource := range resources {
				if uuidToString(resource.ID) != repository.RepositoryID {
					continue
				}
				if resource.ResourceType != "github_repo" {
					return nil, graphConfigurationError("Graph requires a remote repository")
				}
				var ref githubRepoRef
				if err = json.Unmarshal(resource.ResourceRef, &ref); err != nil {
					return nil, err
				}
				repoURL = ref.URL
			}
		}
		if repoURL == "" {
			var repos []RepoData
			if err = json.Unmarshal(workspace.Repos, &repos); err != nil && len(workspace.Repos) > 0 {
				return nil, err
			}
			for _, repo := range repos {
				if repo.URL == repository.RepositoryID {
					repoURL = repo.URL
					break
				}
			}
		}
		if repoURL == "" || !isValidGitRepoURL(repoURL) {
			return nil, graphConfigurationError("repository_id must name a project remote repository resource or an existing workspace repository URL")
		}
		if u, err := url.Parse(repoURL); err == nil {
			if u.User != nil {
				if _, hasPassword := u.User.Password(); hasPassword || u.User.Username() != "git" {
					return nil, graphConfigurationError("repository URLs must not embed credentials")
				}
			}
			if u.RawQuery != "" {
				return nil, graphConfigurationError("repository URLs must not include query parameters")
			}
		}
		root["repository_url"] = repoURL
	}
	return json.Marshal(root)
}
