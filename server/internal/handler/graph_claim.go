package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/graph"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/pluginruntime"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func (h *Handler) graphManagedTask(ctx context.Context, id pgtype.UUID) (bool, error) {
	if h.GraphService == nil {
		return false, nil
	}
	return h.GraphService.IsTask(ctx, uuidToString(id))
}

func graphHydrateCredentials(frozenRaw, liveRaw json.RawMessage, paths [][]string) (json.RawMessage, error) {
	if len(paths) == 0 {
		return frozenRaw, nil
	}
	var frozen, live any
	if err := json.Unmarshal(frozenRaw, &frozen); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(liveRaw, &live); err != nil {
		return nil, err
	}
	for _, path := range paths {
		value, ok := graphJSONPath(live, path)
		if !ok {
			return nil, graphConfigurationError("a required credential binding is unavailable")
		}
		// Freeze the nonsecret portion of a credential-bearing URL as well.
		if len(path) > 0 && strings.EqualFold(path[len(path)-1], "url") {
			old, exists := graphJSONPath(frozen, path)
			var refs [][]string
			redacted := graphFreezeMCP(map[string]any{"url": value}, nil, &refs).(map[string]any)["url"]
			if !exists || !reflect.DeepEqual(old, redacted) {
				return nil, graphConfigurationError("MCP endpoint changed; publish a new Graph version")
			}
		}
		if !graphSetJSONPath(frozen, path, value) {
			return nil, graphConfigurationError("credential path does not match the published configuration")
		}
	}
	return json.Marshal(frozen)
}

func (h *Handler) buildGraphClaim(r *http.Request, task db.AgentTaskQueue, runtime db.AgentRuntime) (AgentTaskResponse, error) {
	resp := taskToResponse(task, uuidToString(runtime.WorkspaceID))
	if !requestHasClientCapability(r, protocol.DaemonCapabilityGraphExecutionV1) {
		return resp, &service.GraphError{Code: "capability_missing", Message: "daemon does not support Graph execution"}
	}
	executor := r.Header.Get("X-Daemon-Instance-ID")
	if _, err := parseGraphUUID(executor); err != nil {
		return resp, graphConfigurationError("daemon process identity is required")
	}
	gc, err := h.GraphService.TaskContext(r.Context(), uuidToString(runtime.WorkspaceID), uuidToString(task.ID))
	if err != nil {
		return resp, err
	}
	if _, digest, err := graph.CanonicalManifest(gc.Manifest); err != nil || digest != gc.ManifestHash {
		return resp, graphConfigurationError("attempt manifest digest does not match")
	}
	var frozen graphFrozenAgent
	if err = json.Unmarshal(gc.Manifest.NodeSpec.AgentConfig, &frozen); err != nil {
		return resp, err
	}
	if frozen.Runtime != nil {
		live, loadErr := loadGraphRuntimeSnapshot(r.Context(), h.Queries, runtime.ID)
		if loadErr != nil {
			return resp, loadErr
		}
		if !sameGraphRuntime(frozen.Runtime, live) {
			return resp, graphConfigurationError("runtime profile changed; publish a new Graph version")
		}
	}
	if gc.Manifest.NodeSpec.Node.Type == graph.NodeTypeAgent && !graphRuntimeCompatible(runtime) || gc.Manifest.NodeSpec.Node.Type == graph.NodeTypeScript && !runtimeHasCapability(runtime.Metadata, protocol.DaemonCapabilityGraphExecutionV2) {
		return resp, &service.GraphError{Code: "capability_missing", Message: "runtime lacks Graph execution capabilities"}
	}
	if len(frozen.PluginEntries) > 0 {
		manifest, loadErr := h.Queries.GetPluginExecutionManifestByTask(r.Context(), task.ID)
		if loadErr != nil {
			return resp, loadErr
		}
		entries, loadErr := pluginruntime.ParseEntries(manifest.OrderedContributions)
		if loadErr != nil || !reflect.DeepEqual(entries, frozen.PluginEntries) {
			return resp, graphConfigurationError("task plugin manifest differs from the published Graph")
		}
		_, _, resp.PluginExecutionManifest, loadErr = h.TaskService.LoadTaskPluginSkillBundles(r.Context(), task.ID)
		if loadErr != nil {
			return resp, loadErr
		}
		var diagnostics []string
		resp.RemoteMCPConnections, diagnostics, loadErr = h.PluginService.ResolveTaskRemoteMCPConnections(r.Context(), task.ID)
		if loadErr != nil {
			return resp, loadErr
		}
		if len(resp.RemoteMCPConnections) > 0 && !requestHasClientCapability(r, protocol.DaemonCapabilityRemoteMCPV1) {
			return resp, &service.GraphError{Code: "capability_missing", Message: "daemon does not support Graph Remote MCP"}
		}
		if resp.PluginExecutionManifest != nil {
			resp.PluginExecutionManifest.Diagnostics = append(resp.PluginExecutionManifest.Diagnostics, diagnostics...)
		}
	}
	agent, err := h.Queries.GetAgent(r.Context(), task.AgentID)
	if err != nil {
		return resp, err
	}
	var env map[string]string
	if len(agent.CustomEnv) > 0 {
		if err = json.Unmarshal(agent.CustomEnv, &env); err != nil {
			return resp, err
		}
	}
	frozen.CustomEnv = map[string]string{}
	liveDigests := graphEnvironmentValueDigests(env)
	for _, key := range frozen.EnvironmentKeys {
		value, ok := env[key]
		if !ok {
			return resp, graphConfigurationError("required environment credential is unavailable")
		}
		if expected, pinned := frozen.EnvironmentValueDigests[key]; pinned && liveDigests[key] != expected {
			return resp, graphConfigurationError("environment configuration changed; publish a new Graph version")
		}
		frozen.CustomEnv[key] = value
	}
	liveMCP, err := h.graphResolvedMCP(r.Context(), h.Queries, agent)
	if err != nil {
		return resp, err
	}
	frozen.McpConfig, err = graphHydrateCredentials(frozen.McpConfig, liveMCP, frozen.CredentialPaths)
	if err != nil {
		return resp, err
	}
	frozen.RuntimeConfig, err = graphHydrateCredentials(frozen.RuntimeConfig, agent.RuntimeConfig, frozen.RuntimeCredentialPaths)
	if err != nil {
		return resp, err
	}
	resp.Agent = &frozen.TaskAgentData
	resp.GraphManifest = &gc.Manifest
	resp.GraphManifestHash = gc.ManifestHash
	resp.GraphRootSnapshot = gc.RootSnapshot
	resp.LeaderRoleResolved = true
	var root struct {
		Title              string `json:"title"`
		WorkspaceContext   string `json:"workspace_context"`
		ProjectID          string `json:"project_id"`
		ProjectTitle       string `json:"project_title"`
		ProjectDescription string `json:"project_description"`
		RepositoryURL      string `json:"repository_url"`
	}
	if err = json.Unmarshal(gc.RootSnapshot, &root); err != nil {
		return resp, err
	}
	resp.ThreadName = root.Title + " · " + gc.Manifest.NodeSpec.Node.Key
	resp.WorkspaceContext = root.WorkspaceContext
	resp.ProjectID = root.ProjectID
	resp.ProjectTitle = root.ProjectTitle
	resp.ProjectDescription = root.ProjectDescription
	if repo := gc.Manifest.NodeSpec.Repository; repo != nil {
		if root.RepositoryURL == "" {
			return resp, graphConfigurationError("frozen repository URL is missing")
		}
		resp.Repos = []RepoData{{URL: root.RepositoryURL, Ref: repo.CommitSHA}}
	}
	// Bind only after the complete claim can be delivered. A missing credential
	// must not pin a never-started attempt to a daemon that received no work.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		return resp, err
	}
	defer tx.Rollback(r.Context())
	if _, err = h.GraphService.AdmitTaskTx(r.Context(), tx, uuidToString(task.ID), "claim"); err != nil {
		return resp, err
	}
	if err = h.GraphService.BindTaskExecutorForClaimTx(r.Context(), tx, uuidToString(task.ID), executor, task.DispatchedAt.Time); err != nil {
		return resp, err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return resp, err
	}
	return resp, nil
}

func (h *Handler) resolveGraphSkillBundles(w http.ResponseWriter, r *http.Request, task db.AgentTaskQueue, ws string, req resolveSkillBundlesRequest) bool {
	managed, err := h.graphManagedTask(r.Context(), task.ID)
	if err != nil {
		writeGraphError(w, err)
		return true
	}
	if !managed {
		return false
	}
	gc, err := h.GraphService.TaskContext(r.Context(), ws, uuidToString(task.ID))
	if err != nil {
		writeGraphError(w, err)
		return true
	}
	var frozen graphFrozenAgent
	if err = json.Unmarshal(gc.Manifest.NodeSpec.AgentConfig, &frozen); err != nil {
		writeGraphError(w, err)
		return true
	}
	byKey := map[string]service.AgentSkillData{}
	for _, skill := range frozen.Skills {
		byKey[skill.Source+":"+skill.ID] = skill
	}
	result := []service.AgentSkillData{}
	for _, ref := range req.Skills {
		skill, ok := byKey[ref.Source+":"+ref.ID]
		if !ok || ref.Hash != skill.Hash {
			writeError(w, http.StatusConflict, "published Graph skill bundle does not match")
			return true
		}
		result = append(result, skill)
	}
	writeJSON(w, http.StatusOK, map[string]any{"bundles": result})
	return true
}

func (h *Handler) validateGraphReceiptRequest(w http.ResponseWriter, r *http.Request, taskID string, receipt *service.GraphTaskReceipt) bool {
	if receipt == nil {
		return true
	}
	if receipt.Identity.TaskID != taskID || receipt.ExecutorID == "" || receipt.ExecutorID != r.Header.Get("X-Daemon-Instance-ID") {
		writeError(w, http.StatusConflict, "Graph receipt does not match the daemon execution")
		return false
	}
	return true
}

func (h *Handler) validateGraphArtifacts(ctx context.Context, tx pgx.Tx, run service.GraphRunRecord, node service.GraphNodeRunRecord, result graph.Result, receipt service.GraphTaskReceipt) error {
	var delivered []struct {
		ArtifactID     string `json:"artifact_id"`
		RepositoryID   string `json:"repository_id"`
		CommitSHA      string `json:"commit_sha"`
		ManagedRef     string `json:"managed_ref"`
		RemoteVerified bool   `json:"remote_verified"`
	}
	if len(receipt.Artifacts) > 0 {
		if err := json.Unmarshal(receipt.Artifacts, &delivered); err != nil {
			return graphConfigurationError("invalid artifact receipt")
		}
	}
	for _, artifact := range result.Artifacts {
		if artifact.ProducerTaskID != result.Identity.TaskID {
			var original []byte
			if err := tx.QueryRow(ctx, `SELECT document FROM graph_artifact WHERE id=$1 AND workspace_id=$2 AND run_id=$3 AND task_id=$4`, artifact.ID, run.WorkspaceID, run.ID, artifact.ProducerTaskID).Scan(&original); err != nil {
				return graphConfigurationError("upstream artifact is unavailable")
			}
			var previous graph.Artifact
			if err := json.Unmarshal(original, &previous); err != nil {
				return err
			}
			if previous != artifact {
				return graphConfigurationError("upstream artifact identity was changed")
			}
			continue
		}
		switch artifact.Type {
		case graph.ArtifactAttachmentRef:
			if err := service.ValidateGraphAttachmentTx(ctx, tx, run.WorkspaceID, result.Identity.TaskID, artifact); err != nil {
				return err
			}
		case graph.ArtifactGitCommit:
			if node.Spec == nil || node.Spec.Repository == nil {
				return graphConfigurationError("repository input is missing")
			}
			repo := node.Spec.Repository
			if artifact.RepositoryID != repo.RepositoryID || artifact.BaseSHA != repo.CommitSHA {
				return graphConfigurationError("Git artifact changed repository identity")
			}
			expected := fmt.Sprintf("refs/heads/multica/graphs/%s/%s/%d", run.ID, node.NodeKey, result.Identity.AttemptNo)
			if node.Spec != nil && node.Spec.InstanceKey != "" {
				expected = fmt.Sprintf("refs/heads/multica/graphs/%s/%s/%d", run.ID, result.Identity.NodeRunID, result.Identity.AttemptNo)
			}
			if artifact.ManagedRef != expected {
				return graphConfigurationError("Git artifact ref is outside this attempt")
			}
			found := false
			for _, item := range delivered {
				if item.ArtifactID == artifact.ID && item.RepositoryID == artifact.RepositoryID && item.CommitSHA == artifact.CommitSHA && item.ManagedRef == artifact.ManagedRef && item.RemoteVerified {
					found = true
					break
				}
			}
			if !found {
				return graphConfigurationError("Git artifact has no matching durable delivery receipt")
			}
		default:
			return graphConfigurationError("unsupported artifact type")
		}
	}
	return nil
}
