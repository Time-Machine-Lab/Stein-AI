package execenv

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGraphQwenpawHasPrivateAgentRegistryAndNoSharedInstructions(t *testing.T) {
	source := t.TempDir()
	t.Setenv("QWENPAW_WORKING_DIR", source)
	if err := os.WriteFile(filepath.Join(source, "config.json"), []byte(`{"agents":{"active_agent":"shared","profiles":{"shared":{"workspace_dir":"shared"}}},"instructions":"must not leak"}`), 0600); err != nil {
		t.Fatal(err)
	}
	env, err := Prepare(PrepareParams{WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace", TaskID: "task", Provider: "qwenpaw", Task: TaskContextForEnv{GraphInstructions: "Frozen only"}}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(env.QwenpawWorkspace, "agent.json"))
	if err != nil {
		t.Fatalf("Graph private agent.json missing: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["id"] != "multica-graph" || cfg["system_prompt"] != nil {
		t.Fatalf("invalid private config: %s", raw)
	}
	root, err := os.ReadFile(filepath.Join(env.GraphHome, ".qwenpaw", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var registry struct {
		Agents struct {
			Active   string `json:"active_agent"`
			Profiles map[string]struct {
				Workspace string `json:"workspace_dir"`
			} `json:"profiles"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(root, &registry); err != nil {
		t.Fatal(err)
	}
	if registry.Agents.Active != "multica-graph" || registry.Agents.Profiles["multica-graph"].Workspace != env.QwenpawWorkspace {
		t.Fatalf("registry can write another agent: %s", root)
	}
	shared, _ := os.ReadFile(filepath.Join(source, "config.json"))
	if string(shared) != `{"agents":{"active_agent":"shared","profiles":{"shared":{"workspace_dir":"shared"}}},"instructions":"must not leak"}` {
		t.Fatal("shared config mutated")
	}
}

func TestGraphNativeProviderHomesPreserveAccountsButExcludeAmbientBehavior(t *testing.T) {
	cases := []struct{ provider, folder, file, raw, secret string }{
		{"antigravity", ".gemini/antigravity-cli", "settings.json", `{"modelProvider":"gemini","model":"ambient-model","customizations":{"ambient-agent":{}}}`, "gemini"},
		{"pi", ".pi/agent", "models.json", `{"providers":{"deepseek":{"baseUrl":"https://api.deepseek.com","apiKey":"secret"}}}`, "deepseek"},
		{"omp", ".omp/agent", "models.json", `{"providers":{"deepseek":{"baseUrl":"https://api.deepseek.com","apiKey":"secret"}}}`, "deepseek"},
		{"kimi", ".kimi", "config.toml", "default_model = 'ambient-model'\n[providers.deepseek]\nbase_url = 'https://api.deepseek.com'\napi_key = 'secret'\n[models.frozen]\nprovider = 'deepseek'\nmodel = 'deepseek-chat'\n[agent]\nname = 'ambient-agent'\n", "deepseek"},
		{"reasonix", ".reasonix", "config.toml", "default_model = 'ambient-model'\n[providers.deepseek]\nbase_url = 'https://api.deepseek.com'\napi_key_env = 'DEEPSEEK_API_KEY'\n[mcp]\ncommand = 'ambient-tool'\n", "deepseek"},
		{"grok", ".grok", "config.toml", "model = 'ambient-model'\n[auth]\napi_key = 'secret'\n[mcp]\ncommand = 'ambient-tool'\n", "secret"},
		{"qwen", ".qwen", "settings.json", `{"model":{"name":"ambient-model"},"modelProviders":{"openai":[{"id":"deepseek-chat","baseUrl":"https://api.deepseek.com"}]},"security":{"auth":{"selectedType":"openai"},"trustedFolders":{"ambient-folder":true}},"mcpServers":{"ambient-tool":{}},"hooks":{"ambient-hook":{}}}`, "deepseek"},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("APPDATA", filepath.Join(home, "appdata"))
			source := filepath.Join(home, filepath.FromSlash(tc.folder))
			if err := os.MkdirAll(source, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(source, tc.file), []byte(tc.raw), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(source, "settings.json"+".ambient"), []byte("ambient-session"), 0600); err != nil {
				t.Fatal(err)
			}
			selectors := map[string]string{"REASONIX_HOME": source, "GROK_HOME": source, "PI_CODING_AGENT_DIR": source, "KIMI_SHARE_DIR": source}
			env := &Environment{GraphHome: t.TempDir()}
			if err := prepareGraphPortableProvider(env, PrepareParams{Provider: tc.provider, Task: TaskContextForEnv{GraphEnvironment: selectors}}); err != nil {
				t.Fatal(err)
			}
			targetFile := tc.file
			if tc.provider == "omp" {
				targetFile = "models.yml"
			}
			raw, err := os.ReadFile(filepath.Join(env.GraphHome, filepath.FromSlash(tc.folder), targetFile))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), tc.secret) {
				t.Fatalf("native transport lost: %s", raw)
			}
			if strings.Contains(string(raw), "ambient-") {
				t.Fatalf("ambient behavior imported: %s", raw)
			}
			if _, err := os.Stat(filepath.Join(env.GraphHome, filepath.FromSlash(tc.folder), "settings.json.ambient")); !os.IsNotExist(err) {
				t.Fatal("unselected native state imported")
			}
			original, _ := os.ReadFile(filepath.Join(source, tc.file))
			if string(original) != tc.raw {
				t.Fatal("shared config modified")
			}
		})
	}
}

func TestGraphHermesPreservesProviderAuthWithoutSharedMemory(t *testing.T) {
	source := t.TempDir()
	raw := "model:\n  default: ambient-model\n  provider: custom\n  base_url: https://api.deepseek.com\ncustom_providers:\n  - name: deepseek\n    base_url: https://api.deepseek.com\nmcp_servers:\n  ambient: {}\nmemory:\n  memory_enabled: true\n"
	if err := os.WriteFile(filepath.Join(source, "config.yaml"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".env"), []byte("DEEPSEEK_API_KEY=secret\nHERMES_MODEL=ambient-model\n"), 0600); err != nil {
		t.Fatal(err)
	}
	env := &Environment{GraphHome: t.TempDir()}
	if err := prepareGraphHermesHome(env, PrepareParams{HermesSourceHome: source}); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(env.HermesHome, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), "ambient") || !strings.Contains(string(config), "api.deepseek.com") || !strings.Contains(string(config), "memory_enabled: false") {
		t.Fatalf("unisolated Hermes config: %s", config)
	}
	auth, _ := os.ReadFile(filepath.Join(env.HermesHome, ".env"))
	if string(auth) != "DEEPSEEK_API_KEY=secret\n" {
		t.Fatalf("wrong credential selection: %q", auth)
	}
}

func TestGraphAntigravityMCPUsesNativeServerURL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	env := &Environment{GraphHome: t.TempDir()}
	if err := prepareGraphPortableProvider(env, PrepareParams{Provider: "antigravity", McpConfig: json.RawMessage(`{"mcpServers":{"pinned":{"type":"http","url":"http://127.0.0.1:8000/mcp","headers":{"Authorization":"Bearer scoped"}}}}`)}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(env.GraphHome, ".gemini", "config", "mcp_config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"serverUrl":"http://127.0.0.1:8000/mcp"`) || !strings.Contains(string(raw), "Bearer scoped") || strings.Contains(string(raw), `"url":`) {
		t.Fatalf("invalid native MCP config: %s", raw)
	}
}

func TestGraphPiWritesOnlyFrozenManagedMCP(t *testing.T) {
	source := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", source)
	if err := os.WriteFile(filepath.Join(source, "mcp.json"), []byte(`{"mcpServers":{"ambient":{"command":"unbound"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	env := &Environment{GraphHome: t.TempDir()}
	config := json.RawMessage(`{"mcpServers":{"frozen":{"type":"http","url":"http://127.0.0.1:9876/mcp","headers":{"Authorization":"Bearer scoped"}}}}`)
	if err := prepareGraphPortableProvider(env, PrepareParams{Provider: "pi", McpConfig: config}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(env.GraphHome, ".pi", "agent", "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "ambient") || !strings.Contains(string(raw), "scoped") {
		t.Fatalf("unexpected MCP config: %s", raw)
	}
}
