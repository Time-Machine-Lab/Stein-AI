package execenv

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestGraphCodexUsesExistingRuntimeAuthentication(t *testing.T) {
	for _, goos := range []string{"linux", "windows"} {
		for _, authFile := range []bool{false, true} {
			t.Run(goos+"/auth-file="+map[bool]string{false: "absent", true: "present"}[authFile], func(t *testing.T) {
				shared := t.TempDir()
				t.Setenv("CODEX_HOME", shared)
				config := `model_provider = "private"
cli_auth_credentials_store = "file"
model = "ambient-model"
[model_providers.private]
name = "private"
base_url = "https://provider.test/v1"
experimental_bearer_token = "fictional-regression-token"
env_key = "PRIVATE_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = true
[model_providers.private.http_headers]
X-Custom-Auth = "fictional-header"
[model_providers.private.env_http_headers]
X-Env-Auth = "PRIVATE_HEADER"
[model_providers.private.query_params]
api-version = "test-version"
[model_providers.unselected]
experimental_bearer_token = "fictional-other-token"
`
				if err := os.WriteFile(filepath.Join(shared, "config.toml"), []byte(config), 0600); err != nil {
					t.Fatal(err)
				}
				if authFile {
					if err := os.WriteFile(filepath.Join(shared, "auth.json"), []byte(`{"OPENAI_API_KEY":"fictional-file-token"}`), 0600); err != nil {
						t.Fatal(err)
					}
				}
				var prepared [2]map[string]any
				for i, graph := range []bool{false, true} {
					home := filepath.Join(t.TempDir(), "codex-home")
					if err := prepareCodexHomeWithOpts(home, CodexHomeOptions{GraphIsolated: graph, GOOS: goos}, testLogger()); err != nil {
						t.Fatal(err)
					}
					raw, err := os.ReadFile(filepath.Join(home, "config.toml"))
					if err != nil {
						t.Fatal(err)
					}
					if err := toml.Unmarshal(raw, &prepared[i]); err != nil {
						t.Fatal(err)
					}
					if authFile {
						got, err := os.ReadFile(filepath.Join(home, "auth.json"))
						if err != nil || string(got) != `{"OPENAI_API_KEY":"fictional-file-token"}` {
							t.Fatal("runtime auth file was not inherited")
						}
					}
				}
				ordinary := prepared[0]["model_providers"].(map[string]any)["private"]
				graphProviders := prepared[1]["model_providers"].(map[string]any)
				if !reflect.DeepEqual(ordinary, graphProviders["private"]) {
					t.Error("Graph changed the existing runtime's provider authentication/transport configuration")
				}
				if len(graphProviders) != 1 || prepared[1]["model_provider"] != "private" {
					t.Error("Graph did not retain only the selected runtime provider")
				}
				if prepared[1]["cli_auth_credentials_store"] != "file" {
					t.Error("Graph dropped the runtime credential store selection")
				}
				unchanged, err := os.ReadFile(filepath.Join(shared, "config.toml"))
				if err != nil || string(unchanged) != config {
					t.Fatal("preparing a task changed the shared runtime configuration")
				}
			})
		}
	}
}

func TestGraphRuntimeContextUsesDedicatedWorkflowAndFreshStores(t *testing.T) {
	ctx := TaskContextForEnv{GraphInstructions: "Only run multica graph node context and submit the frozen result.", AgentID: "graph-agent"}
	for _, provider := range []string{"codex", "claude", "hermes", "opencode"} {
		brief, err := InjectRuntimeConfig(t.TempDir(), provider, ctx)
		if err != nil {
			t.Fatal(err)
		}
		if brief != ctx.GraphInstructions {
			t.Errorf("%s brief contains non-Graph workflow: %s", provider, brief)
		}
		if strings.Contains(renderIssueContext(provider, ctx), "multica issue") {
			t.Errorf("%s injected ordinary issue workflow", provider)
		}
	}
	if CodexSessionStorePath("", ctx) != "" || HermesSessionStorePath("", "graph-agent", "", ctx) != "" {
		t.Fatal("Graph attempt uses a shared prior conversation store")
	}
}

func TestGraphCodexKeepsFreshSessionsWithOrdinaryRuntimeOptions(t *testing.T) {
	shared := t.TempDir()
	t.Setenv("CODEX_HOME", shared)
	home := filepath.Join(t.TempDir(), "codex-home")
	if err := prepareCodexHomeWithOpts(home, CodexHomeOptions{
		GraphIsolated: true, GOOS: "linux", IsLocalDirectory: true,
		SessionStoreKey: "ordinary-issue", ResumeSessionID: "prior-conversation",
	}, testLogger()); err != nil {
		t.Fatal(err)
	}
	sessions := filepath.Join(home, "sessions")
	info, err := os.Lstat(sessions)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("Graph sessions must be a fresh local directory")
	}
	entries, err := os.ReadDir(sessions)
	if err != nil || len(entries) != 0 {
		t.Fatal("Graph inherited a prior conversation")
	}
	if err := prepareCodexHomeWithOpts(home, CodexHomeOptions{GraphIsolated: true}, testLogger()); err == nil {
		t.Fatal("Graph accepted a previously prepared home")
	}
}

func TestGraphCodexPreparePreservesInlineOnlyAuthentication(t *testing.T) {
	shared := t.TempDir()
	t.Setenv("CODEX_HOME", shared)
	config := `model_provider = "private"
model_instructions_file = "missing-ambient-instructions.md"
[model_providers.private]
base_url = "https://provider.test/v1"
experimental_bearer_token = "fictional-inline-only-token"
requires_openai_auth = false
wire_api = "responses"
`
	if err := os.WriteFile(filepath.Join(shared, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace", TaskID: "graph-attempt", Provider: "codex",
		Task: TaskContextForEnv{GraphInstructions: "Frozen node instructions only"},
	}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(env.CodexHome, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := toml.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	provider := got["model_providers"].(map[string]any)["private"].(map[string]any)
	if provider["experimental_bearer_token"] != "fictional-inline-only-token" {
		t.Fatal("Graph Prepare dropped the runtime's only authentication source")
	}
	if _, err := os.Stat(filepath.Join(env.CodexHome, "auth.json")); !os.IsNotExist(err) {
		t.Fatal("inline-only fixture unexpectedly has an auth file")
	}
}

func TestGraphCodexUsesRuntimeWindowsSandboxArgs(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	home := filepath.Join(t.TempDir(), "codex-home")
	if err := prepareCodexHomeWithOpts(home, CodexHomeOptions{
		GraphIsolated: true, GOOS: "windows", CodexCustomArgs: []string{"-c", `windows.sandbox="unelevated"`},
	}, testLogger()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := toml.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if config["sandbox_mode"] != "workspace-write" {
		t.Fatal("Graph ignored the existing runtime's native sandbox arguments")
	}
}

func TestGraphCodexHomeDoesNotInheritImplicitContext(t *testing.T) {
	shared := t.TempDir()
	t.Setenv("CODEX_HOME", shared)
	t.Setenv(MulticaCodexMemoryEnv, "1")
	t.Setenv(MulticaCodexMultiAgentEnv, "1")
	config := `model = "ambient-model"
model_provider = "private"
developer_instructions = "ambient instructions"
[model_providers.private]
name = "private"
base_url = "https://provider.test/v1"
env_key = "PRIVATE_API_KEY"
wire_api = "responses"
instructions = "not transport"
[mcp_servers.ambient]
command = "ambient-mcp"
[features]
multi_agent = true
memories = true
`
	for name, content := range map[string]string{"config.toml": config, "instructions.md": "ambient instructions", "auth.json": "{}", "skills/ambient/SKILL.md": "ambient skill", "plugins/registry.json": "{}"} {
		path := filepath.Join(shared, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	home := filepath.Join(t.TempDir(), "codex-home")
	if err := prepareCodexHomeWithOpts(home, CodexHomeOptions{GraphIsolated: true, GOOS: "linux"}, testLogger()); err != nil {
		t.Fatal(err)
	}
	if err := hydrateGraphCodexSkills(home, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := toml.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"model", "developer_instructions", "mcp_servers", "plugins"} {
		if _, ok := got[forbidden]; ok {
			t.Errorf("inherited %s", forbidden)
		}
	}
	features := got["features"].(map[string]any)
	if features["multi_agent"] != false || features["memories"] != false {
		t.Fatalf("Graph inherited native feature escape hatch: %s", raw)
	}
	provider := got["model_providers"].(map[string]any)["private"].(map[string]any)
	if provider["base_url"] != "https://provider.test/v1" || provider["env_key"] != "PRIVATE_API_KEY" {
		t.Fatalf("lost live provider transport: %v", provider)
	}
	if _, ok := provider["instructions"]; ok {
		t.Fatal("copied non-transport provider config")
	}
	for _, name := range []string{"instructions.md", "plugins", "skills/ambient"} {
		if _, err := os.Lstat(filepath.Join(home, name)); !os.IsNotExist(err) {
			t.Errorf("ambient path exists: %s", name)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "auth.json")); err != nil {
		t.Fatalf("live auth unavailable: %v", err)
	}
}
