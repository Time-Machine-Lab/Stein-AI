package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/daemon/processtree"
	"github.com/multica-ai/multica/server/internal/daemon/repocache"
	"github.com/multica-ai/multica/server/internal/graph"
)

const graphDeliveryTimeout = 2 * time.Minute

// GraphTaskReceipt is the daemon's terminal evidence, separate from the staged business result.
type GraphTaskReceipt struct {
	Identity       graph.AttemptIdentity `json:"identity"`
	SubmissionID   string                `json:"submission_id,omitempty"`
	ProviderExit   string                `json:"provider_exit"`
	DeliveryStatus string                `json:"delivery_status"`
	FailureReason  string                `json:"failure_reason,omitempty"`
	Retryable      bool                  `json:"retryable"`
	ExecutorID     string                `json:"executor_id"`
	StoppedAt      time.Time             `json:"stopped_at"`
	Artifacts      json.RawMessage       `json:"artifacts,omitempty"`
}

type GraphSubmission struct {
	ID          string       `json:"id"`
	TaskID      string       `json:"task_id"`
	ContentHash string       `json:"content_hash"`
	Result      graph.Result `json:"result"`
}

type graphGitReceipt struct {
	ArtifactID     string `json:"artifact_id"`
	RepositoryID   string `json:"repository_id"`
	CommitSHA      string `json:"commit_sha"`
	ManagedRef     string `json:"managed_ref"`
	RemoteVerified bool   `json:"remote_verified"`
}

func validateGraphTask(task Task) error {
	if task.Graph == nil {
		return nil
	}
	_, hash, err := graph.CanonicalManifest(*task.Graph)
	if err != nil {
		return fmt.Errorf("invalid Graph manifest: %w", err)
	}
	if hash != task.GraphManifestHash {
		return fmt.Errorf("Graph manifest hash mismatch")
	}
	if task.Graph.Identity.TaskID != task.ID || task.Graph.NodeSpec.Node.AgentID != task.AgentID {
		return fmt.Errorf("Graph task identity mismatch")
	}
	if task.PriorSessionID != "" || task.PriorWorkDir != "" || task.TriggerCommentID != "" || len(task.CoalescedCommentIDs) > 0 || task.ChatSessionID != "" || task.AutopilotRunID != "" || task.QuickCreatePrompt != "" || task.SquadID != "" || task.IsLeaderTask {
		return fmt.Errorf("Graph attempts require a fresh, dedicated execution context")
	}
	for _, resource := range task.ProjectResources {
		if resource.ResourceType == "local_directory" {
			return fmt.Errorf("Graph V1 does not support local directories or local worktrees")
		}
	}
	if task.Agent == nil || task.Agent.ID != task.AgentID {
		return fmt.Errorf("Graph task requires frozen agent configuration")
	}
	if repository := task.Graph.NodeSpec.Repository; repository != nil {
		if len(task.Repos) != 1 || strings.TrimSpace(task.Repos[0].URL) == "" || task.Repos[0].Ref != repository.CommitSHA {
			return fmt.Errorf("Graph repository must resolve to the one frozen input commit")
		}
	} else if len(task.Repos) > 0 {
		return fmt.Errorf("Graph task has repositories outside its manifest")
	}
	refs := map[string]string{}
	for _, ref := range task.Agent.SkillRefs {
		key := ref.Source + ":" + ref.ID
		if _, exists := refs[key]; exists {
			return fmt.Errorf("Graph task contains duplicate skill identity %s", key)
		}
		refs[key] = ref.Hash
	}
	for _, skill := range task.Graph.NodeSpec.Skills {
		if refs[skill.ID] != skill.Hash {
			return fmt.Errorf("Graph skill %s is not pinned to its frozen hash", skill.ID)
		}
	}
	if len(refs) != len(task.Graph.NodeSpec.Skills) {
		return fmt.Errorf("Graph task contains skills outside its manifest")
	}
	return nil
}

func applyFrozenGraphAgent(task *Task) error {
	if task.Graph == nil {
		return nil
	}
	var frozen AgentData
	if err := json.Unmarshal(task.Graph.NodeSpec.AgentConfig, &frozen); err != nil {
		return fmt.Errorf("decode frozen Graph agent: %w", err)
	}
	if frozen.ID != task.AgentID {
		return fmt.Errorf("frozen Graph agent identity mismatch")
	}
	// Credential values are resolved by the server within the frozen key scope.
	frozen.CustomEnv = task.Agent.CustomEnv
	frozen.McpConfig = task.Agent.McpConfig
	task.Agent = &frozen
	return validateGraphTask(*task)
}

func buildGraphPrompt(task Task) string {
	if task.Graph == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("You are executing one immutable Multica Graph node. Complete only this node; the Graph engine owns scheduling, issue state, retry, and approval.\n\n")
	fmt.Fprintf(&b, "Node: %s; attempt: %d.\n\n", task.Graph.NodeSpec.Node.Key, task.Graph.Identity.AttemptNo)
	b.WriteString("Read your frozen context with `multica graph node context --output json`. Do not fetch new issue comments, change issue state, delegate work, or start another task.\n\n")
	if task.Agent != nil && task.Agent.Instructions != "" {
		fmt.Fprintf(&b, "Frozen agent instructions:\n%s\n\n", task.Agent.Instructions)
	}
	fmt.Fprintf(&b, "Node instructions:\n%s\n\nFrozen input JSON:\n%s\n\n", task.Graph.NodeSpec.Node.Instructions, task.Graph.NodeSpec.Input)
	if len(task.GraphRootSnapshot) > 0 {
		fmt.Fprintf(&b, "Frozen run context:\n%s\n\n", task.GraphRootSnapshot)
	}
	fmt.Fprintf(&b, "Execution mode: %s. Output JSON schema:\n%s\n\n", task.Graph.NodeSpec.Node.ExecutionMode, task.Graph.NodeSpec.Node.OutputSchema)
	if task.Graph.NodeSpec.Repository != nil {
		fmt.Fprintf(&b, "The repository has already been checked out beneath the task working directory at input commit %s. Work in that checkout; do not replace the input ref.\n", task.Graph.NodeSpec.Repository.CommitSHA)
	}
	if task.Graph.NodeSpec.Node.ExecutionMode == graph.ExecutionProduceCommit {
		fmt.Fprintf(&b, "Commit your completed code explicitly and declare that exact SHA in a git_commit artifact. Its managed_ref must be %s, repository_id %s, producer_task_id %s, and base_sha %s. The daemon publishes it after you exit; do not push the managed ref yourself.\n", graphManagedRef(task), task.Graph.NodeSpec.Repository.RepositoryID, task.ID, task.Graph.NodeSpec.Repository.CommitSHA)
	}
	if task.Graph.NodeSpec.Node.ExecutionMode == graph.ExecutionVerifyCommit {
		b.WriteString("Keep HEAD at the input candidate and leave the checkout clean. Return tested_commit_sha equal to that input commit.\n")
	}
	b.WriteString("Write a result JSON file with outcome (pass, environment_blocked, or needs_changes), output, optional artifacts, and diagnostics for any non-pass outcome. Submit it with `multica graph node submit --result-file <path> --output json`, then exit. The CLI binds the current attempt identity. Use `multica graph node block --reason-file <path> --output json` for an environment blocker, then exit. Put result/diagnostic files outside the repository checkout so the delivered code remains clean. A submission is staged until the executor stops and the engine accepts it.\n")
	return b.String()
}

func graphManagedRef(task Task) string {
	return "refs/heads/multica/graphs/" + task.Graph.Identity.RunID + "/" + task.Graph.NodeSpec.Node.Key + "/" + strconv.Itoa(task.Graph.Identity.AttemptNo)
}

type graphGitRunner func(context.Context, string, ...string) (string, error)

func runGraphGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-c", "core.hooksPath=", "-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := processtree.CombinedOutput(ctx, cmd, 5*time.Second)
	if err != nil {
		return "", fmt.Errorf("Graph git %s failed: %w: %s", args[0], err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func finalizeGraphGit(ctx context.Context, task Task, repoDir string, result graph.Result, run graphGitRunner) ([]graphGitReceipt, error) {
	spec := task.Graph.NodeSpec
	if result.Outcome != graph.OutcomePass || spec.Node.ExecutionMode == graph.ExecutionNone {
		return nil, nil
	}
	if repoDir == "" || spec.Repository == nil {
		return nil, fmt.Errorf("Graph repository checkout is missing")
	}
	head, err := run(ctx, repoDir, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	status, err := run(ctx, repoDir, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	if status != "" {
		return nil, fmt.Errorf("Graph repository has uncommitted changes")
	}
	if spec.Node.ExecutionMode == graph.ExecutionVerifyCommit {
		if head != spec.Repository.CommitSHA || result.TestedCommitSHA != spec.Repository.CommitSHA {
			return nil, fmt.Errorf("verification did not preserve the input candidate SHA")
		}
		return nil, nil
	}
	if len(task.Repos) != 1 {
		return nil, fmt.Errorf("Graph repository URL is unavailable")
	}
	var produced *graph.Artifact
	for i := range result.Artifacts {
		a := &result.Artifacts[i]
		if a.Type == graph.ArtifactGitCommit && a.ProducerTaskID == task.ID {
			if produced != nil {
				return nil, fmt.Errorf("Graph V1 permits one produced Git candidate")
			}
			produced = a
		}
	}
	if produced == nil || produced.RepositoryID != spec.Repository.RepositoryID || produced.BaseSHA != spec.Repository.CommitSHA || produced.CommitSHA != head || produced.ManagedRef != graphManagedRef(task) {
		return nil, fmt.Errorf("declared Git artifact does not match this attempt and final HEAD")
	}
	if _, err := run(ctx, repoDir, "merge-base", "--is-ancestor", spec.Repository.CommitSHA, head); err != nil {
		return nil, fmt.Errorf("candidate does not descend from input commit: %w", err)
	}
	remoteURL := task.Repos[0].URL
	remote, err := run(ctx, repoDir, "ls-remote", "--refs", remoteURL, produced.ManagedRef)
	if err != nil {
		return nil, err
	}
	if remote != "" {
		fields := strings.Fields(remote)
		if len(fields) != 2 || fields[0] != head || fields[1] != produced.ManagedRef {
			return nil, fmt.Errorf("managed ref already names a different candidate")
		}
	} else {
		// An empty expected lease prevents racing writers from creating a different ref.
		if _, err := run(ctx, repoDir, "push", "--force-with-lease="+produced.ManagedRef+":", remoteURL, head+":"+produced.ManagedRef); err != nil {
			return nil, err
		}
	}
	remote, err = run(ctx, repoDir, "ls-remote", "--refs", remoteURL, produced.ManagedRef)
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(remote)
	if len(fields) != 2 || fields[0] != head || fields[1] != produced.ManagedRef {
		return nil, fmt.Errorf("published candidate could not be verified")
	}
	return []graphGitReceipt{{ArtifactID: produced.ID, RepositoryID: produced.RepositoryID, CommitSHA: head, ManagedRef: produced.ManagedRef, RemoteVerified: true}}, nil
}

func (d *Daemon) prepareGraphRepository(ctx context.Context, task Task, env *execenv.Environment) (string, error) {
	if task.Graph == nil {
		return "", nil
	}
	d.graphRepoBindings.Store(task.ID, graphRepoBinding{WorkspaceID: task.WorkspaceID, WorkDir: env.WorkDir, RootDir: env.RootDir})
	if task.Graph.NodeSpec.Repository == nil {
		return "", nil
	}
	if d.repoCache == nil {
		return "", fmt.Errorf("Graph repository cache is unavailable")
	}
	if err := d.ensureRepoReady(ctx, task.WorkspaceID, task.Repos[0].URL); err != nil {
		return "", err
	}
	params := repocache.WorktreeParams{WorkspaceID: task.WorkspaceID, RepoURL: task.Repos[0].URL, WorkDir: env.WorkDir, Ref: task.Graph.NodeSpec.Repository.CommitSHA, AgentName: task.Agent.Name, TaskID: task.ID, IsolatedGitMetadata: true, LockWaitTimeout: 30 * time.Second}
	cache, ok := d.repoCache.(interface {
		CreateWorktreeContext(context.Context, repocache.WorktreeParams) (*repocache.WorktreeResult, error)
	})
	if !ok {
		return "", fmt.Errorf("Graph requires cancellable fixed-commit checkout support")
	}
	result, err := cache.CreateWorktreeContext(ctx, params)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(env.WorkDir, result.Path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("Graph repository checkout escaped its managed environment")
	}
	head, err := runGraphGit(ctx, result.Path, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	if head != task.Graph.NodeSpec.Repository.CommitSHA {
		return "", fmt.Errorf("Graph checkout HEAD does not match frozen input")
	}
	d.graphRepoBindings.Store(task.ID, graphRepoBinding{WorkspaceID: task.WorkspaceID, URL: task.Repos[0].URL, Ref: task.Graph.NodeSpec.Repository.CommitSHA, WorkDir: env.WorkDir, RootDir: env.RootDir, Result: *result})
	return result.Path, nil
}

type graphRepoBinding struct {
	WorkspaceID, URL, Ref, WorkDir, RootDir string
	Result                                  repocache.WorktreeResult
}

func graphPathContains(root, path string) bool {
	if root == "" || path == "" {
		return false
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return false
	}
	// Resolve existing ancestors so a symlink plus a not-yet-created child cannot bypass the fence.
	resolve := func(path string) string {
		var suffix []string
		for {
			if real, err := filepath.EvalSymlinks(path); err == nil {
				for i := len(suffix) - 1; i >= 0; i-- {
					real = filepath.Join(real, suffix[i])
				}
				return real
			}
			parent := filepath.Dir(path)
			if parent == path {
				return path
			}
			suffix = append(suffix, filepath.Base(path))
			path = parent
		}
	}
	root, path = resolve(root), resolve(path)
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func (d *Daemon) graphCheckoutBinding(req repoCheckoutRequest) (graphRepoBinding, bool, bool) {
	var binding graphRepoBinding
	var found, valid bool
	d.graphRepoBindings.Range(func(key, value any) bool {
		candidate := value.(graphRepoBinding)
		root := candidate.RootDir
		if root == "" {
			root = candidate.WorkDir
		}
		if key.(string) != req.TaskID && !graphPathContains(root, req.WorkDir) && !graphPathContains(req.WorkDir, root) {
			return true
		}
		found, binding = true, candidate
		valid = key.(string) == req.TaskID && candidate.URL != "" && req.WorkspaceID == candidate.WorkspaceID && req.URL == candidate.URL && (req.Ref == "" || req.Ref == candidate.Ref) && graphPathContains(candidate.WorkDir, req.WorkDir) && graphPathContains(req.WorkDir, candidate.WorkDir)
		return false
	})
	return binding, found, valid
}

func graphBlockedEnvironmentKey(key string) bool {
	upper := strings.ToUpper(key)
	if isBlockedEnvKey(key) || strings.HasPrefix(upper, "CODEX_") || strings.HasPrefix(upper, "XDG_") {
		return true
	}
	switch upper {
	case "USERPROFILE", "APPDATA", "LOCALAPPDATA", "HOMEDRIVE", "HOMEPATH", "NODE_OPTIONS", "NODE_PATH", "BASH_ENV", "ENV", "ZDOTDIR", "LD_PRELOAD", "LD_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES":
		return true
	}
	return false
}

// graphExecutionEnvironment keeps provider credentials live without inheriting provider configuration switches.
func graphExecutionEnvironment(env *execenv.Environment, explicit, custom map[string]string) (map[string]string, error) {
	if env.GraphHome == "" || env.CodexHome == "" {
		return nil, fmt.Errorf("Graph provider isolation is missing")
	}
	for key := range custom {
		if graphBlockedEnvironmentKey(key) {
			return nil, fmt.Errorf("Graph custom environment key %s is reserved", key)
		}
	}
	names := []string{"PATH", "SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT", "TEMP", "TMP", "TMPDIR", "LANG", "LC_ALL", "LC_CTYPE", "TZ", "OPENAI_API_KEY", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "SSL_CERT_FILE", "SSL_CERT_DIR", "NODE_EXTRA_CA_CERTS"}
	if raw, err := os.ReadFile(filepath.Join(env.CodexHome, "config.toml")); err == nil {
		var config map[string]any
		if err := toml.Unmarshal(raw, &config); err != nil {
			return nil, err
		}
		if providers, ok := config["model_providers"].(map[string]any); ok {
			for _, item := range providers {
				if provider, ok := item.(map[string]any); ok {
					if key, ok := provider["env_key"].(string); ok && !graphBlockedEnvironmentKey(key) {
						names = append(names, key)
					}
					if headers, ok := provider["env_http_headers"].(map[string]any); ok {
						for _, value := range headers {
							if key, ok := value.(string); ok && !graphBlockedEnvironmentKey(key) {
								names = append(names, key)
							}
						}
					}
				}
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	out := map[string]string{}
	for _, name := range names {
		if value, ok := os.LookupEnv(name); ok {
			out[name] = value
		}
	}
	for key, value := range explicit {
		out[key] = value
	}
	out["CODEX_HOME"], out["HOME"], out["USERPROFILE"] = env.CodexHome, env.GraphHome, env.GraphHome
	for key, suffix := range map[string]string{"XDG_CONFIG_HOME": "config", "XDG_DATA_HOME": "data", "XDG_CACHE_HOME": "cache", "XDG_STATE_HOME": "state", "APPDATA": "appdata", "LOCALAPPDATA": "localappdata"} {
		path := filepath.Join(env.GraphHome, suffix)
		if err := os.MkdirAll(path, 0700); err != nil {
			return nil, err
		}
		out[key] = path
	}
	return out, nil
}

func (d *Daemon) graphStoppedReceipt(task Task, providerExit string) *GraphTaskReceipt {
	if task.Graph == nil {
		return nil
	}
	return &GraphTaskReceipt{Identity: task.Graph.Identity, ProviderExit: providerExit, DeliveryStatus: "not_required", ExecutorID: d.client.instanceID, StoppedAt: time.Now().UTC()}
}

func (d *Daemon) finalizeGraphTask(ctx context.Context, task Task, result *TaskResult, runErr error) {
	if task.Graph == nil {
		return
	}
	if result.GraphExecutionStopped == nil || !*result.GraphExecutionStopped {
		result.GraphReceipt = nil
		result.FailureReason = "execution_unknown"
		return
	}
	exit := "failed"
	if runErr == nil && result.Status == "completed" {
		exit = "success"
	} else if result.Status == "cancelled" {
		exit = "cancelled"
	}
	receipt := d.graphStoppedReceipt(task, exit)
	result.GraphReceipt = receipt
	if exit != "success" {
		receipt.FailureReason = result.FailureReason
		return
	}
	finalCtx, cancel := context.WithTimeout(ctx, graphDeliveryTimeout)
	defer cancel()
	submission, err := d.client.GetGraphSubmission(finalCtx, task.ID)
	if err != nil || submission == nil {
		receipt.DeliveryStatus = "failed"
		receipt.FailureReason = "output_invalid"
		return
	}
	receipt.SubmissionID = submission.ID
	if err := graph.ValidateIdentity(task.Graph.Identity, submission.Result.Identity); err != nil {
		receipt.DeliveryStatus = "failed"
		receipt.FailureReason = "stale_attempt"
		return
	}
	if err := graph.ValidateResult(task.Graph.NodeSpec.Node, submission.Result); err != nil {
		receipt.DeliveryStatus = "failed"
		receipt.FailureReason = "output_invalid"
		return
	}
	if submission.Result.Outcome != graph.OutcomePass {
		return
	}
	artifacts, err := finalizeGraphGit(finalCtx, task, result.GraphRepositoryPath, submission.Result, runGraphGit)
	if err != nil {
		receipt.DeliveryStatus = "failed"
		receipt.FailureReason = "artifact_unavailable"
		return
	}
	if len(artifacts) > 0 {
		receipt.Artifacts, err = json.Marshal(artifacts)
		if err != nil {
			receipt.DeliveryStatus = "failed"
			receipt.FailureReason = "artifact_unavailable"
			return
		}
	}
	receipt.DeliveryStatus = "delivered"
}

func isGraphHold(err error) bool {
	var request *requestError
	if !errors.As(err, &request) || request.StatusCode != http.StatusConflict {
		return false
	}
	var body struct {
		Code string `json:"code"`
	}
	if json.Unmarshal([]byte(request.Body), &body) != nil {
		return false
	}
	return body.Code == "run_paused"
}

func (c *Client) GetGraphSubmission(ctx context.Context, taskID string) (*GraphSubmission, error) {
	var response GraphSubmission
	if err := c.getJSON(ctx, "/api/daemon/tasks/"+taskID+"/graph-submission", &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) ReportGraphStopped(ctx context.Context, taskID string, receipt *GraphTaskReceipt) error {
	return c.postJSONWithRetry(ctx, "/api/daemon/tasks/"+taskID+"/graph-stopped", receipt, nil, defaultTerminalRetrySchedule)
}
