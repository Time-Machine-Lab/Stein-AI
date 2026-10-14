package execenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func graphExtraTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for key, value := range map[string]string{"HOME": home, "USERPROFILE": home, "APPDATA": filepath.Join(home, "AppData", "Roaming"), "XDG_CONFIG_HOME": filepath.Join(home, ".config"), "XDG_DATA_HOME": filepath.Join(home, ".local", "share"), "ProgramData": filepath.Join(home, "programdata"), "CODEBUDDY_CONFIG_DIR": "", "COPILOT_HOME": "", "COPILOT_PROVIDERS_CONFIG": "", "CURSOR_CONFIG_DIR": "", "QODER_CONFIG_DIR": "", "DSH_HOME": "", "OPENCODE_CONFIG": "", "OPENCODE_CONFIG_DIR": "", "OPENCODE_CONFIG_CONTENT": "", "DEVECO_CONFIG": "", "DEVECO_CONFIG_DIR": "", "DEVECO_CONFIG_CONTENT": "", "KIRO_API_KEY": "test-key", "CURSOR_API_KEY": "test-key", "QODER_PERSONAL_ACCESS_TOKEN": "test-key"} {
		t.Setenv(key, value)
	}
	return home
}

func graphExtraWrite(t *testing.T, path, raw string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestGraphExtraProviderOverlays(t *testing.T) {
	cases := []struct{ provider, source, target, raw, want string }{
		{"codebuddy", ".codebuddy/settings.json", ".codebuddy/settings.json", `{"env":{"CODEBUDDY_API_KEY":"test-secret","CODEBUDDY_BASE_URL":"https://gateway.example","CODEBUDDY_MODEL":"ambient-model","NODE_OPTIONS":"ambient-hook"},"model":"ambient-model","hooks":{"x":"ambient-hook"}}`, "test-secret"},
		{"copilot", ".copilot/providers.json", ".copilot/providers.json", `{"providers":{"custom":{"baseUrl":"https://gateway.example","apiKey":"test-secret"}},"models":{"frozen":{"provider":"custom"}},"hooks":{"x":"ambient-hook"}}`, "test-secret"},
		{"cursor", ".cursor/cli-config.json", ".cursor/cli-config.json", `{"network":{"useHttp1ForAgent":true,"unexpected":"ambient-hook"},"model":{"id":"ambient-model"},"permissions":{"allow":["ambient-tool"]}}`, "useHttp1ForAgent"},
		{"qoder", ".qoder/settings.json", ".qoder/settings.json", `{"vpcInstanceName":"private-vpc","modelConfigs":{"customAliases":{"ambient-model":{}}},"hooks":{"x":"ambient-hook"}}`, "private-vpc"},
		{"qoder-cn", ".qoder-cn/settings.json", ".qoder-cn/settings.json", `{"vpcInstanceName":"private-vpc","model":{"name":"ambient-model"},"mcpServers":{"ambient-tool":{}}}`, "private-vpc"},
		{"qoderclicn", ".qoder-cn/settings.json", ".qoder-cn/settings.json", `{"vpcInstanceName":"private-vpc","model":{"name":"ambient-model"}}`, "private-vpc"},
		{"opencode", ".config/opencode/opencode.jsonc", "config/opencode/opencode.json", `{/*comment*/"provider":{"custom":{"npm":"@ai-sdk/openai-compatible","options":{"baseURL":"https://gateway.example","apiKey":"test-secret"}}},"model":"ambient-model","mcp":{"ambient-tool":{}},"plugin":["ambient-hook"],}`, "test-secret"},
		{"deveco", ".config/deveco/deveco.jsonc", "config/deveco/deveco.json", `{"provider":{"custom":{"options":{"baseURL":"https://gateway.example","apiKey":"test-secret"}}},"model":"ambient-model","agent":{"ambient-agent":{}}}`, "test-secret"},
		{"dsh", ".dsh/.credentials.yaml", ".dsh/.credentials.yaml", "version: 1\nrefs:\n  DEEPSEEK_API_KEY: test-secret\nrecords: {}\n", "test-secret"},
		{"traecli", ".trae/traecli.toml", ".trae/traecli.toml", "trae_login_base_url = 'https://login.example'\nmodel = 'ambient-model'\n[mcp_servers.ambient]\ncommand = 'ambient-tool'\n", "https://login.example"},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			home := graphExtraTestHome(t)
			source := filepath.Join(home, filepath.FromSlash(tc.source))
			graphExtraWrite(t, source, tc.raw)
			graphExtraWrite(t, filepath.Join(filepath.Dir(source), "history.json"), "ambient-history")
			env := &Environment{GraphHome: t.TempDir()}
			params := PrepareParams{Provider: tc.provider, Task: TaskContextForEnv{GraphInstructions: "frozen", GraphEnvironment: map[string]string{}}}
			if tc.provider == "cursor" {
				params.Task.GraphEnvironment["CURSOR_CONFIG_DIR"] = filepath.Dir(source)
			}
			if err := prepareGraphPortableProvider(env, params); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(env.GraphHome, filepath.FromSlash(tc.target)))
			if err != nil {
				t.Fatalf("private native overlay absent: %v", err)
			}
			if !strings.Contains(string(raw), tc.want) || strings.Contains(string(raw), "ambient-") {
				t.Fatalf("incorrect native overlay: %s", raw)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Join(env.GraphHome, filepath.FromSlash(tc.target))), "history.json")); !os.IsNotExist(err) {
				t.Fatal("native history copied")
			}
			original, err := os.ReadFile(source)
			if err != nil || string(original) != tc.raw {
				t.Fatal("source config mutated")
			}
		})
	}
}

func TestGraphExtraProviderOverridesAreExplicitAndPrivate(t *testing.T) {
	graphExtraTestHome(t)
	source := t.TempDir()
	graphExtraWrite(t, filepath.Join(source, "providers.json"), `{"providers":{"custom":{"apiKey":"frozen-source"}},"models":{}}`)
	env := &Environment{GraphHome: t.TempDir()}
	if err := prepareGraphPortableProvider(env, PrepareParams{Provider: "copilot", Task: TaskContextForEnv{GraphEnvironment: map[string]string{"COPILOT_HOME": source}}}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(env.GraphHome, ".copilot", "providers.json"))
	if err != nil || !strings.Contains(string(raw), "frozen-source") {
		t.Fatal("explicit source was not honored")
	}
	graphExtraWrite(t, filepath.Join(env.GraphHome, ".copilot", "providers.json"), `{}`)
	raw, _ = os.ReadFile(filepath.Join(source, "providers.json"))
	if !strings.Contains(string(raw), "frozen-source") {
		t.Fatal("overlay writes escaped into source")
	}
	if err := prepareGraphPortableProvider(&Environment{GraphHome: t.TempDir()}, PrepareParams{Provider: "copilot", Task: TaskContextForEnv{GraphEnvironment: map[string]string{"COPILOT_HOME": filepath.Join(source, "missing")}}}); err == nil {
		t.Fatal("missing explicit native source silently discarded")
	}
}

func TestGraphExtraProviderRejectsUnsupportedAuthenticationConfiguration(t *testing.T) {
	cases := []struct{ provider, key, path, raw string }{
		{"codebuddy", "", ".codebuddy/settings.json", `{"apiKeyHelper":"must-not-run"}`},
		{"opencode", "", ".local/share/opencode/auth.json", `{"https://org.example":{"type":"wellknown","key":"TOKEN","token":"secret-value"}}`},
		{"deveco", "", ".local/share/deveco/auth.json", `{"https://org.example":{"type":"wellknown","key":"TOKEN","token":"secret-value"}}`},
		{"cursor", "CURSOR_API_KEY", "", ""},
		{"kiro", "KIRO_API_KEY", "", ""},
		{"qoder", "QODER_PERSONAL_ACCESS_TOKEN", "", ""},
		{"qoder-cn", "QODER_PERSONAL_ACCESS_TOKEN", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			home := graphExtraTestHome(t)
			if tc.path != "" {
				graphExtraWrite(t, filepath.Join(home, filepath.FromSlash(tc.path)), tc.raw)
			}
			if tc.key != "" {
				t.Setenv(tc.key, "")
			}
			err := prepareGraphPortableProvider(&Environment{GraphHome: t.TempDir()}, PrepareParams{Provider: tc.provider})
			if err == nil {
				t.Fatal("unisolated configuration accepted")
			}
			if strings.Contains(err.Error(), "secret-value") || strings.Contains(err.Error(), "must-not-run") {
				t.Fatal("configuration secret leaked in error")
			}
		})
	}
}

func TestGraphExtraProviderRefusesManagedOpenCodeConfiguration(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows managed path fixture")
	}
	home := graphExtraTestHome(t)
	graphExtraWrite(t, filepath.Join(home, "programdata", "opencode", "opencode.json"), `{"mcp":{"ambient-tool":{}}}`)
	if err := prepareGraphPortableProvider(&Environment{GraphHome: t.TempDir()}, PrepareParams{Provider: "opencode"}); err == nil {
		t.Fatal("higher precedence managed configuration was ignored")
	}
	if err := prepareGraphExtraProvider(&Environment{GraphHome: t.TempDir()}, PrepareParams{Provider: "opencode", Task: TaskContextForEnv{GraphEnvironment: map[string]string{"ProgramData": t.TempDir()}}}); err == nil {
		t.Fatal("custom environment hid system managed configuration")
	}
}

func TestGraphExtraProviderCopiesOnlyTokenStoreAndProviderProxy(t *testing.T) {
	home := graphExtraTestHome(t)
	graphExtraWrite(t, filepath.Join(home, ".local", "share", "opencode", "auth.json"), `{"custom":{"type":"api","key":"test-secret"}}`)
	graphExtraWrite(t, filepath.Join(home, ".local", "share", "opencode", "mcp-auth.json"), `{"ambient-tool":"ambient-token"}`)
	env := &Environment{GraphHome: t.TempDir()}
	if err := prepareGraphPortableProvider(env, PrepareParams{Provider: "opencode"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(env.GraphHome, "data", "opencode", "auth.json"))
	if err != nil || !strings.Contains(string(raw), "test-secret") {
		t.Fatal("OpenCode token store missing")
	}
	if _, err := os.Stat(filepath.Join(env.GraphHome, "data", "opencode", "mcp-auth.json")); !os.IsNotExist(err) {
		t.Fatal("unbound MCP auth copied")
	}
	graphExtraWrite(t, filepath.Join(home, ".copilot", "settings.json"), `{/*proxy*/"proxyUrl":"http://proxy.example:8080","proxyKerberosServicePrincipal":"HTTP/proxy.example","model":"ambient-model","hooks":{"ambient-hook":{}}}`)
	env = &Environment{GraphHome: t.TempDir()}
	if err := prepareGraphPortableProvider(env, PrepareParams{Provider: "copilot"}); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(filepath.Join(env.GraphHome, ".copilot", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["proxyUrl"] != "http://proxy.example:8080" || cfg["model"] != nil || cfg["hooks"] != nil {
		t.Fatal("proxy lost or live behavior imported")
	}
}

func TestGraphExtraProviderResolvesLayeredTransportAndReferences(t *testing.T) {
	home := graphExtraTestHome(t)
	graphExtraWrite(t, filepath.Join(home, ".config", "opencode", "opencode.json"), `{"provider":{"custom":{"options":{"baseURL":"https://global.example","apiKey":"old"}}}}`)
	custom := t.TempDir()
	graphExtraWrite(t, filepath.Join(custom, "token.txt"), "private-token\n")
	graphExtraWrite(t, filepath.Join(custom, "config.jsonc"), `{"provider":{"custom":{"options":{"baseURL":"https://custom.example","apiKey":"{file:token.txt}","headers":{"X-Account":"{env:ACCOUNT}"}}}},"model":"ambient-model"}`)
	params := PrepareParams{Provider: "opencode", Task: TaskContextForEnv{GraphEnvironment: map[string]string{"OPENCODE_CONFIG": filepath.Join(custom, "config.jsonc"), "ACCOUNT": "frozen-account", "OPENCODE_CONFIG_CONTENT": `{"provider":{"custom":{"options":{"baseURL":"https://inline.example"}}},"plugin":["ambient-plugin"]}`}}}
	env := &Environment{GraphHome: t.TempDir()}
	if err := prepareGraphExtraProvider(env, params); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(env.GraphHome, "config", "opencode", "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"https://inline.example", "private-token", "frozen-account"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("layered transport lost %s", want)
		}
	}
	if strings.Contains(string(raw), "ambient-") || strings.Contains(string(raw), "{file:") || strings.Contains(string(raw), "{env:") {
		t.Fatal("unresolved or ambient config copied")
	}
}

func TestGraphExtraProviderBoundsExpandedCredentialsAcrossProviders(t *testing.T) {
	graphExtraTestHome(t)
	source := t.TempDir()
	graphExtraWrite(t, filepath.Join(source, "token.txt"), strings.Repeat("x", 3<<20))
	graphExtraWrite(t, filepath.Join(source, "config.json"), `{"provider":{"a":{"options":{"apiKey":"{file:token.txt}"}},"b":{"options":{"apiKey":"{file:token.txt}"}}}}`)
	err := prepareGraphExtraProvider(&Environment{GraphHome: t.TempDir()}, PrepareParams{Provider: "opencode", Task: TaskContextForEnv{GraphEnvironment: map[string]string{"OPENCODE_CONFIG": filepath.Join(source, "config.json")}}})
	if err == nil {
		t.Fatal("expanded credentials escaped shared size budget")
	}
}

func TestGraphExtraProviderMalformedSourceDoesNotLeakSecrets(t *testing.T) {
	cases := []struct{ provider, path, raw string }{
		{"codebuddy", ".codebuddy/settings.json", `{"env":{"CODEBUDDY_API_KEY":secret-value}}`},
		{"copilot", ".copilot/providers.json", `["secret-value"]`},
		{"cursor", ".cursor/cli-config.json", `{"network":"secret-value"}`},
		{"qoder", ".qoder/settings.json", `secret-value`},
		{"qoder-cn", ".qoder-cn/settings.json", `secret-value`},
		{"opencode", ".config/opencode/opencode.json", `{"provider":"secret-value"}`},
		{"deveco", ".config/deveco/deveco.json", `{"provider":"secret-value"}`},
		{"traecli", ".trae/traecli.toml", `secret-value = [`},
		{"dsh", ".dsh/.credentials.yaml", "version: 1\nrefs: [secret-value\n"},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			home := graphExtraTestHome(t)
			graphExtraWrite(t, filepath.Join(home, filepath.FromSlash(tc.path)), tc.raw)
			params := PrepareParams{Provider: tc.provider}
			if tc.provider == "cursor" {
				params.Task.GraphEnvironment = map[string]string{"CURSOR_CONFIG_DIR": filepath.Join(home, ".cursor")}
			}
			err := prepareGraphExtraProvider(&Environment{GraphHome: t.TempDir()}, params)
			if err == nil {
				t.Fatal("malformed source silently ignored")
			}
			if strings.Contains(err.Error(), "secret-value") {
				t.Fatal("credential leaked in error")
			}
		})
	}
}

func TestGraphExtraProviderKiroKeyIsFrozenAndHasNoImportedHistory(t *testing.T) {
	home := graphExtraTestHome(t)
	graphExtraWrite(t, filepath.Join(home, ".kiro", "data.sqlite"), "ambient-history")
	env := &Environment{GraphHome: t.TempDir()}
	if err := prepareGraphExtraProvider(env, PrepareParams{Provider: "kiro", Task: TaskContextForEnv{GraphEnvironment: map[string]string{"KIRO_API_KEY": "frozen-key"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(env.GraphHome, ".kiro", "data.sqlite")); !os.IsNotExist(err) {
		t.Fatal("Kiro history copied")
	}
	if err := prepareGraphExtraProvider(env, PrepareParams{Provider: "kiro", Task: TaskContextForEnv{GraphEnvironment: map[string]string{"KIRO_API_KEY": ""}}}); err == nil {
		t.Fatal("empty frozen key fell back to ambient key")
	}
}

func TestGraphExtraProviderTraeLegacyCatalogWithoutModelSelection(t *testing.T) {
	home := graphExtraTestHome(t)
	var source, target string
	switch runtime.GOOS {
	case "windows":
		source = filepath.Join(home, "AppData", "Roaming", "trae_cli", "trae_cli.yaml")
		target = "AppData/Roaming/trae_cli/trae_cli.yaml"
	case "darwin":
		source = filepath.Join(home, "Library", "Application Support", "trae_cli", "trae_cli.yaml")
		target = "Library/Application Support/trae_cli/trae_cli.yaml"
	default:
		source = filepath.Join(home, ".config", "trae_cli", "trae_cli.yaml")
		target = "config/trae_cli/trae_cli.yaml"
	}
	graphExtraWrite(t, source, "models:\n  - name: gateway\n    open_ai:\n      base_url: https://gateway.example\n      api_key: test-secret\n      model: frozen-model\nmodel:\n  name: ambient-model\nmcp_servers:\n  - name: ambient-tool\n")
	env := &Environment{GraphHome: t.TempDir()}
	if err := prepareGraphExtraProvider(env, PrepareParams{Provider: "traecli"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(env.GraphHome, filepath.FromSlash(target)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "test-secret") || strings.Contains(string(raw), "ambient-") {
		t.Fatal("Trae legacy catalog lost or behavior copied")
	}
}

func TestGraphExtraProviderCodeBuddyFrozenCredentialsOverrideFileValues(t *testing.T) {
	home := graphExtraTestHome(t)
	graphExtraWrite(t, filepath.Join(home, ".codebuddy", "settings.json"), `{"env":{"CODEBUDDY_API_KEY":"old-account","CODEBUDDY_BASE_URL":"https://old.example"}}`)
	env := &Environment{GraphHome: t.TempDir()}
	params := PrepareParams{Provider: "codebuddy", Task: TaskContextForEnv{GraphEnvironment: map[string]string{"CODEBUDDY_API_KEY": "frozen-account", "CODEBUDDY_BASE_URL": "https://frozen.example"}}}
	if err := prepareGraphExtraProvider(env, params); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(env.GraphHome, ".codebuddy", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "old-account") || !strings.Contains(string(raw), "frozen-account") {
		t.Fatal("native file overrides explicit runtime credentials")
	}
}

func TestGraphExtraProviderRejectsTooLargeSourceBeforeParsing(t *testing.T) {
	home := graphExtraTestHome(t)
	graphExtraWrite(t, filepath.Join(home, ".dsh", ".credentials.yaml"), strings.Repeat("x", (4<<20)+1))
	err := prepareGraphExtraProvider(&Environment{GraphHome: t.TempDir()}, PrepareParams{Provider: "dsh"})
	if err == nil || !strings.Contains(err.Error(), "4 MiB") {
		t.Fatal("oversized source read without a bound")
	}
}

func TestGraphExtraProviderRejectsMissingFrozenCredentialReferences(t *testing.T) {
	home := graphExtraTestHome(t)
	t.Setenv("CUSTOM_PROVIDER_KEY", "ambient-secret")
	graphExtraWrite(t, filepath.Join(home, ".config", "opencode", "opencode.json"), `{"provider":{"custom":{"options":{"apiKey":"{env:CUSTOM_PROVIDER_KEY}"}}}}`)
	err := prepareGraphExtraProvider(&Environment{GraphHome: t.TempDir()}, PrepareParams{Provider: "opencode", Task: TaskContextForEnv{GraphEnvironment: map[string]string{"CUSTOM_PROVIDER_KEY": ""}}})
	if err == nil {
		t.Fatal("explicit empty credential reference fell back to ambient account")
	}
	if strings.Contains(err.Error(), "ambient-secret") {
		t.Fatal("credential leaked")
	}
}
