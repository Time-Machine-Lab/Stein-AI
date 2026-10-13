package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

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
