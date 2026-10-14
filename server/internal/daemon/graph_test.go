package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/graph"
)

func graphTestTask(t *testing.T) Task {
	t.Helper()
	input := json.RawMessage(`{"goal":"build"}`)
	_, ih, err := graph.CanonicalJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	node := graph.Node{Key: "build", Type: graph.NodeTypeAgent, AgentID: "agent-1", Instructions: "Build exactly the frozen goal.", OutputSchema: json.RawMessage(`{"type":"object"}`), ExecutionMode: graph.ExecutionNone, MaxAttempts: 3, TimeoutSeconds: 60}
	spec := graph.NodeSpec{ProtocolVersion: 1, Node: node, Input: input, InputHash: ih, AgentConfig: json.RawMessage(`{"id":"agent-1","name":"Builder","instructions":"Frozen persona"}`)}
	identity := graph.AttemptIdentity{RunID: "run-1", NodeRunID: "node-1", TaskID: "task-1", AttemptNo: 1, ExecutionGeneration: 1, InputHash: ih}
	manifest, err := graph.NewAttemptManifest(spec, identity)
	if err != nil {
		t.Fatal(err)
	}
	_, hash, err := graph.CanonicalManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return Task{ID: "task-1", AgentID: "agent-1", WorkspaceID: "workspace-1", IssueID: "issue-1", Graph: &manifest, GraphManifestHash: hash, Agent: &AgentData{ID: "agent-1", Name: "Builder", Instructions: "Frozen persona"}}
}

func TestGraphScopedArtifactRefsDoNotCollideAcrossIterations(t *testing.T) {
	first := graphTestTask(t)
	if got := graphManagedRef(first); got != "refs/heads/multica/graphs/run-1/build/1" {
		t.Fatalf("legacy ref changed: %s", got)
	}
	first.Graph.NodeSpec.InstanceKey = "review/body/1/build"
	one := graphManagedRef(first)
	first.Graph.Identity.NodeRunID = "node-2"
	first.Graph.NodeSpec.InstanceKey = "review/body/2/build"
	two := graphManagedRef(first)
	if one == two {
		t.Fatal("iteration artifact refs collide")
	}
}

func TestGraphCheckoutCannotChangeFixedRepositoryOrHideTaskIdentity(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "workdir")
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{}
	d.graphRepoBindings.Store("graph-task", graphRepoBinding{WorkspaceID: "ws", URL: "https://repo.test/a.git", Ref: "fixed-sha", WorkDir: work, RootDir: root})
	for _, tt := range []struct {
		name, task, dir, ref string
		want                 int
	}{
		{"same", "graph-task", work, "fixed-sha", 200},
		{"different_ref", "graph-task", work, "main", 409},
		{"missing_task", "", work, "fixed-sha", 409},
		{"different_task", "ordinary-task", work, "fixed-sha", 409},
		{"nested", "", filepath.Join(work, "nested"), "", 409},
		{"ancestor", "", root, "", 409},
		{"outside", "graph-task", t.TempDir(), "fixed-sha", 409},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(repoCheckoutRequest{TaskID: tt.task, WorkspaceID: "ws", URL: "https://repo.test/a.git", WorkDir: tt.dir, Ref: tt.ref})
			w := httptest.NewRecorder()
			d.repoCheckoutHandler()(w, httptest.NewRequest("POST", "/repo/checkout", strings.NewReader(string(body))))
			if w.Code != tt.want {
				t.Fatalf("got %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestGraphEnvironmentKeepsOnlyExplicitAuthAndPrivateHomes(t *testing.T) {
	t.Setenv("AMBIENT_SECRET", "must-not-leak")
	t.Setenv("CODEX_AMBIENT_CONFIG", "must-not-leak")
	t.Setenv("OPENAI_API_KEY", "live-auth")
	env := &execenv.Environment{RootDir: t.TempDir(), GraphHome: t.TempDir(), CodexHome: t.TempDir()}
	got, err := graphExecutionEnvironment("codex", env, map[string]string{"MULTICA_TOKEN": "task-token", "PATH": "bin", "EXPLICIT_API_KEY": "explicit-auth"}, map[string]string{"EXPLICIT_API_KEY": "explicit-auth"})
	if err != nil {
		t.Fatal(err)
	}
	if got["AMBIENT_SECRET"] != "" || got["CODEX_AMBIENT_CONFIG"] != "" {
		t.Fatal("inherited ambient provider state")
	}
	if got["OPENAI_API_KEY"] != "live-auth" || got["EXPLICIT_API_KEY"] != "explicit-auth" || got["MULTICA_TOKEN"] != "task-token" {
		t.Fatal("lost authorized auth")
	}
	if got["HOME"] != env.GraphHome || got["USERPROFILE"] != env.GraphHome {
		t.Fatal("global skill roots are still visible")
	}
	for _, key := range []string{"HOME", "USERPROFILE", "CODEX_HOME", "XDG_CONFIG_HOME", "MULTICA_TOKEN", "NODE_OPTIONS", "CODEX_FEATURES"} {
		if _, err := graphExecutionEnvironment("codex", env, nil, map[string]string{key: "override"}); err == nil {
			t.Errorf("unsafe custom env accepted: %s", key)
		}
	}
}

func TestGraphTaskRejectsChangedManifestAndLegacyContext(t *testing.T) {
	base := graphTestTask(t)
	if err := validateGraphTask(base); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Task){func(t *Task) { t.GraphManifestHash = "bad" }, func(t *Task) { t.PriorSessionID = "old-session" }, func(t *Task) { t.PriorWorkDir = "old-dir" }, func(t *Task) { t.ProjectResources = []ProjectResourceData{{ResourceType: "local_directory"}} }, func(t *Task) { t.TriggerCommentID = "comment" }, func(t *Task) { t.AgentID = "another-agent" }} {
		task := base
		change(&task)
		if err := validateGraphTask(task); err == nil {
			t.Fatal("unsafe Graph task accepted")
		}
	}
}

func TestGraphTaskAcceptsPublishedSourceQualifiedSkillIdentity(t *testing.T) {
	task := graphTestTask(t)
	hash := "sha256:" + strings.Repeat("a", 64)
	task.Agent.SkillRefs = []SkillRefData{{ID: "builtin-graph-v1", Source: "builtin", Hash: hash}, {ID: "selected-skill", Source: "workspace", Hash: hash}}
	task.Graph.NodeSpec.Skills = []graph.SkillSnapshot{{ID: "builtin:builtin-graph-v1", Hash: hash, BundleRef: "builtin:builtin-graph-v1"}, {ID: "workspace:selected-skill", Hash: hash, BundleRef: "workspace:selected-skill"}}
	manifest, err := graph.NewAttemptManifest(task.Graph.NodeSpec, task.Graph.Identity)
	if err != nil {
		t.Fatal(err)
	}
	task.Graph = &manifest
	_, task.GraphManifestHash, err = graph.CanonicalManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateGraphTask(task); err != nil {
		t.Fatal(err)
	}
	task.Agent.SkillRefs[1].Source = "plugin"
	if err := validateGraphTask(task); err == nil {
		t.Fatal("skill from a different source accepted")
	}
}

func TestGraphPromptHasOnlyFrozenNodeWorkflow(t *testing.T) {
	task := graphTestTask(t)
	task.ActiveSiblingRuns = []ActiveSiblingRunData{{TaskID: "unrelated"}}
	prompt := BuildPrompt(task, "codex")
	for _, required := range []string{"multica graph node context", "multica graph node submit", "Build exactly the frozen goal.", `"goal":"build"`} {
		if !strings.Contains(prompt, required) {
			t.Errorf("missing %q", required)
		}
	}
	for _, forbidden := range []string{"multica issue get", "multica issue comment list", "Active sibling runs"} {
		if strings.Contains(prompt, forbidden) {
			t.Errorf("unexpected legacy context %q", forbidden)
		}
	}
}

func TestGraphFinalizerValidatesSameCommitBeforePublishing(t *testing.T) {
	task := graphTestTask(t)
	inputSHA := strings.Repeat("a", 40)
	commitSHA := strings.Repeat("b", 40)
	task.Graph.NodeSpec.Node.ExecutionMode = graph.ExecutionProduceCommit
	task.Graph.NodeSpec.Repository = &graph.RepositoryInput{RepositoryID: "repo-1", BaseSHA: inputSHA, CommitSHA: inputSHA}
	task.Repos = []RepoData{{URL: "https://example.test/repo.git", Ref: inputSHA}}
	artifact := graph.Artifact{ID: "artifact-1", Type: graph.ArtifactGitCommit, RepositoryID: "repo-1", BaseSHA: inputSHA, CommitSHA: commitSHA, ManagedRef: graphManagedRef(task), ProducerTaskID: task.ID}
	result := graph.Result{Identity: task.Graph.Identity, Outcome: graph.OutcomePass, Output: json.RawMessage(`{}`), Artifacts: []graph.Artifact{artifact}}
	for _, tt := range []struct {
		name, head, status string
		remote             string
		wantErr            bool
		wantPush           bool
	}{{"clean", commitSHA, "", "", false, true}, {"changed_head", inputSHA, "", "", true, false}, {"dirty", commitSHA, " M file.go", "", true, false}, {"ref_conflict", commitSHA, "", inputSHA + "\t" + artifact.ManagedRef, true, false}, {"idempotent", commitSHA, "", commitSHA + "\t" + artifact.ManagedRef, false, false}} {
		t.Run(tt.name, func(t *testing.T) {
			pushed := false
			remote := tt.remote
			run := func(_ context.Context, _ string, args ...string) (string, error) {
				switch args[0] {
				case "rev-parse":
					return tt.head, nil
				case "status":
					return tt.status, nil
				case "merge-base":
					return "", nil
				case "ls-remote":
					return remote, nil
				case "push":
					pushed = true
					remote = commitSHA + "\t" + artifact.ManagedRef
					return "", nil
				default:
					return "", fmt.Errorf("unexpected git command %v", args)
				}
			}
			_, err := finalizeGraphGit(context.Background(), task, "repo-dir", result, run)
			if (err != nil) != tt.wantErr || pushed != tt.wantPush {
				t.Fatalf("error %v, pushed %v", err, pushed)
			}
		})
	}
}

func TestGraphVerifyCannotPublishOrChangeCandidate(t *testing.T) {
	task := graphTestTask(t)
	sha := strings.Repeat("a", 40)
	task.Graph.NodeSpec.Node.ExecutionMode = graph.ExecutionVerifyCommit
	task.Graph.NodeSpec.Repository = &graph.RepositoryInput{RepositoryID: "repo-1", BaseSHA: sha, CommitSHA: sha}
	result := graph.Result{Identity: task.Graph.Identity, Outcome: graph.OutcomePass, Output: json.RawMessage(`{}`), TestedCommitSHA: sha}
	run := func(_ context.Context, _ string, args ...string) (string, error) {
		if args[0] == "rev-parse" {
			return sha, nil
		}
		if args[0] == "status" {
			return "", nil
		}
		t.Fatalf("unexpected write/network command %v", args)
		return "", nil
	}
	if _, err := finalizeGraphGit(context.Background(), task, "repo-dir", result, run); err != nil {
		t.Fatal(err)
	}
	result.TestedCommitSHA = strings.Repeat("b", 40)
	if _, err := finalizeGraphGit(context.Background(), task, "repo-dir", result, run); err == nil {
		t.Fatal("different tested SHA accepted")
	}
}

func TestGraphTerminalProtocolUsesOneExecutorIdentityAndDirectSubmission(t *testing.T) {
	task := graphTestTask(t)
	var instance string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if instance == "" {
			instance = r.Header.Get("X-Daemon-Instance-ID")
		}
		if instance == "" || instance != r.Header.Get("X-Daemon-Instance-ID") {
			t.Error("missing or changed executor identity")
		}
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(GraphSubmission{ID: "submission-1", TaskID: task.ID, Result: graph.Result{Identity: task.Graph.Identity, Outcome: graph.OutcomePass, Output: json.RawMessage(`{}`)}})
			return
		}
		var receipt GraphTaskReceipt
		if err := json.NewDecoder(r.Body).Decode(&receipt); err != nil {
			t.Error(err)
		}
		if receipt.ExecutorID != instance || receipt.Identity.TaskID != task.ID {
			t.Errorf("wrong stopped receipt: %+v", receipt)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := NewClient(server.URL)
	submission, err := client.GetGraphSubmission(context.Background(), task.ID)
	if err != nil || submission == nil || submission.ID != "submission-1" {
		t.Fatalf("submission: %+v %v", submission, err)
	}
	d := Daemon{client: client}
	if err := client.ReportGraphStopped(context.Background(), task.ID, d.graphStoppedReceipt(task, "success")); err != nil {
		t.Fatal(err)
	}
}

func TestGraphEnvironmentCredentialsStayWithinSelectedProvider(t *testing.T) {
	for _, key := range []string{"ANTHROPIC_API_KEY", "GITHUB_TOKEN", "GH_TOKEN", "COPILOT_GITHUB_TOKEN", "CURSOR_API_KEY", "KIRO_API_KEY", "OPENAI_API_KEY"} {
		t.Setenv(key, "ambient-"+key)
	}
	for _, provider := range []string{"claude", "copilot", "script"} {
		t.Run(provider, func(t *testing.T) {
			env := &execenv.Environment{GraphHome: t.TempDir(), CodexHome: t.TempDir()}
			got, err := graphExecutionEnvironment(provider, env, map[string]string{"EXPLICIT_SECRET": "authorized"}, map[string]string{"EXPLICIT_SECRET": "authorized"})
			if err != nil {
				t.Fatal(err)
			}
			allowed := map[string]bool{}
			if provider == "claude" {
				allowed["ANTHROPIC_API_KEY"] = true
			}
			if provider == "copilot" {
				for _, key := range []string{"GITHUB_TOKEN", "GH_TOKEN", "COPILOT_GITHUB_TOKEN"} {
					allowed[key] = true
				}
			}
			for _, key := range []string{"ANTHROPIC_API_KEY", "GITHUB_TOKEN", "GH_TOKEN", "COPILOT_GITHUB_TOKEN", "CURSOR_API_KEY", "KIRO_API_KEY", "OPENAI_API_KEY"} {
				if (got[key] != "") != allowed[key] {
					t.Errorf("%s credential present=%v want=%v", key, got[key] != "", allowed[key])
				}
			}
			if got["EXPLICIT_SECRET"] != "authorized" {
				t.Fatal("explicit authorized binding removed")
			}
		})
	}
}
