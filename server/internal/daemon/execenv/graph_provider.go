package execenv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

// Provider credentials are private copies rather than linked config roots:
// native CLIs may persist settings alongside token refreshes. A Graph attempt
// must never mutate the account's shared instructions, model or session state.
func copyGraphCredentialFile(source, target string) error {
	data, err := os.ReadFile(source)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(data) > 4<<20 {
		return fmt.Errorf("provider credential file exceeds 4 MiB")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	return os.WriteFile(target, data, 0600)
}

func writeGraphJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func prepareGraphQwenpawHome(env *Environment, params PrepareParams) error {
	sourceEnv := func(key string) string {
		if value, ok := params.Task.GraphEnvironment[key]; ok {
			return value
		}
		return os.Getenv(key)
	}
	source := strings.TrimSpace(sourceEnv("QWENPAW_WORKING_DIR"))
	if source == "" {
		source = strings.TrimSpace(sourceEnv("COPAW_WORKING_DIR"))
	}
	if source == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		source = filepath.Join(home, ".qwenpaw")
		if info, err := os.Stat(filepath.Join(home, ".copaw")); err == nil && info.IsDir() {
			source = filepath.Join(home, ".copaw")
		}
	}
	target := filepath.Join(env.GraphHome, ".qwenpaw")
	// The ACP model RPC saves through the root registry, independently of
	// --workspace. Both must resolve to this attempt's private agent.json.
	config := map[string]any{"agents": map[string]any{"active_agent": "multica-graph", "profiles": map[string]any{"multica-graph": map[string]any{"id": "multica-graph", "workspace_dir": env.QwenpawWorkspace, "enabled": true}}}}
	if err := writeGraphJSON(filepath.Join(target, "config.json"), config); err != nil {
		return err
	}
	profile := map[string]any{"id": "multica-graph", "name": "Graph attempt", "workspace_dir": env.QwenpawWorkspace, "project_dir": env.WorkDir, "fallback_models": []any{}, "fallback_policy": map[string]any{"enabled": false}, "llm_routing": map[string]any{"enabled": false}, "mcp": map[string]any{"clients": map[string]any{}}, "channels": map[string]any{}, "system_prompt_files": []string{}}
	if err := writeGraphJSON(filepath.Join(env.QwenpawWorkspace, "agent.json"), profile); err != nil {
		return err
	}
	secretSource := sourceEnv("QWENPAW_SECRET_DIR")
	if secretSource == "" {
		secretSource = source + ".secret"
	}
	// QwenPaw stores auth/transport separately from instructions and memory.
	// Preserve built-in/custom provider records, excluding executable plugins.
	for _, kind := range []string{"builtin", "custom"} {
		entries, err := os.ReadDir(filepath.Join(secretSource, "providers", kind))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
				if err := copyGraphCredentialFile(filepath.Join(secretSource, "providers", kind, entry.Name()), filepath.Join(target+".secret", "providers", kind, entry.Name())); err != nil {
					return err
				}
			}
		}
	}
	return copyGraphCredentialFile(filepath.Join(secretSource, "providers.json"), filepath.Join(target+".secret", "providers.json"))
}

func prepareGraphHermesHome(env *Environment, params PrepareParams) error {
	source := params.HermesSourceHome
	if source == "" {
		source = platformDefaultHermesHome()
	}
	if params.HermesSourceMustExist {
		if info, err := os.Stat(source); err != nil || !info.IsDir() {
			return fmt.Errorf("Hermes profile home does not exist")
		}
	}
	target := filepath.Join(env.GraphHome, ".hermes")
	if err := os.MkdirAll(target, 0700); err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(source, "config.yaml"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	original := map[string]any{}
	if len(raw) > 0 {
		if err := yaml.Unmarshal(raw, &original); err != nil {
			return fmt.Errorf("invalid Hermes provider config: %w", err)
		}
	}
	config := map[string]any{}
	// Model transport and credential routing are needed for custom gateways;
	// instructions, tools, plugins, MCP and history are never imported.
	for _, key := range []string{"providers", "custom_providers", "provider", "base_url", "api_key"} {
		if value, ok := original[key]; ok {
			config[key] = value
		}
	}
	if model, ok := original["model"].(map[string]any); ok {
		transport := map[string]any{}
		for _, key := range []string{"provider", "base_url", "api_key"} {
			if value, ok := model[key]; ok {
				transport[key] = value
			}
		}
		config["model"] = transport
	}
	config["skills"] = map[string]any{"external_dirs": []string{}}
	config["memory"] = map[string]any{"provider": "", "memory_enabled": false, "user_profile_enabled": false}
	encoded, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(target, "config.yaml"), encoded, 0600); err != nil {
		return err
	}
	// These are Hermes' token stores, not its sessions/state databases.
	for _, name := range []string{"auth.json", "oauth_tokens.json", "credentials.json"} {
		if err := copyGraphCredentialFile(filepath.Join(source, name), filepath.Join(target, name)); err != nil {
			return err
		}
	}
	dotenv, err := os.ReadFile(filepath.Join(source, ".env"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var allowed strings.Builder
	for _, line := range strings.Split(string(dotenv), "\n") {
		key := dotenvLineKey(line)
		if graphProviderCredentialKey(key) {
			allowed.WriteString(line)
			allowed.WriteByte('\n')
		}
	}
	if err := os.WriteFile(filepath.Join(target, ".env"), []byte(allowed.String()), 0600); err != nil {
		return err
	}
	if err := writeHermesBoundSkills(target, params.Task.AgentSkills, nil); err != nil {
		return err
	}
	env.HermesHome = target
	return nil
}

func graphProviderCredentialKey(key string) bool {
	switch key {
	case "OPENAI_API_KEY", "OPENAI_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "OPENROUTER_API_KEY", "DEEPSEEK_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY", "GROQ_API_KEY", "XAI_API_KEY", "KIMI_API_KEY", "MINIMAX_API_KEY":
		return true
	}
	return false
}

func prepareGraphPortableProvider(env *Environment, params PrepareParams) error {
	provider := params.Provider
	sourceEnv := func(key string) string {
		if value, ok := params.Task.GraphEnvironment[key]; ok {
			return value
		}
		return os.Getenv(key)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	switch provider {
	case "antigravity":
		raw, err := os.ReadFile(filepath.Join(home, ".gemini", "antigravity-cli", "settings.json"))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		original := map[string]any{}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &original); err != nil {
				return err
			}
		}
		selected := map[string]any{}
		if value, ok := original["modelProvider"]; ok {
			selected["modelProvider"] = value
		}
		if err := writeGraphJSON(filepath.Join(env.GraphHome, ".gemini", "antigravity-cli", "settings.json"), selected); err != nil {
			return err
		}
		var root map[string]json.RawMessage
		if len(params.McpConfig) > 0 && string(params.McpConfig) != "null" {
			if err := json.Unmarshal(params.McpConfig, &root); err != nil {
				return err
			}
		}
		servers := map[string]map[string]any{}
		if raw, ok := root["mcpServers"]; ok {
			if err := json.Unmarshal(raw, &servers); err != nil {
				return err
			}
		} else {
			for key, raw := range root {
				var item map[string]any
				if err := json.Unmarshal(raw, &item); err != nil {
					return err
				}
				servers[key] = item
			}
		}
		for _, item := range servers {
			if url, ok := item["url"]; ok {
				item["serverUrl"] = url
				delete(item, "url")
			}
			delete(item, "type")
		}
		// The native schema uses serverUrl; url/httpUrl are silently ignored.
		return writeGraphJSON(filepath.Join(env.GraphHome, ".gemini", "config", "mcp_config.json"), map[string]any{"mcpServers": servers})
	case "reasonix", "grok":
		source := filepath.Join(home, "."+provider)
		if provider == "reasonix" && runtime.GOOS == "windows" && os.Getenv("APPDATA") != "" {
			source = filepath.Join(os.Getenv("APPDATA"), "reasonix")
		}
		if value := sourceEnv(strings.ToUpper(provider) + "_HOME"); value != "" {
			source = value
		}
		target := filepath.Join(env.GraphHome, "."+provider)
		keys := []string{"providers", "config_version"}
		if provider == "grok" {
			keys = []string{"auth", "grok_com_config", "api_base_url"}
		}
		if err := copyGraphTOMLFields(filepath.Join(source, "config.toml"), filepath.Join(target, "config.toml"), keys); err != nil {
			return err
		}
		if provider == "grok" {
			return copyGraphCredentialFile(filepath.Join(source, "auth.json"), filepath.Join(target, "auth.json"))
		}
		// Reasonix's global .env contains provider credentials only. It is never
		// loaded as process environment and remains private to its native resolver.
		return copyGraphCredentialFile(filepath.Join(source, ".env"), filepath.Join(target, ".env"))
	case "omp":
		return prepareGraphOMPHome(env, params)
	case "pi":
		source := filepath.Join(home, "."+provider, "agent")
		if provider == "pi" && sourceEnv("PI_CODING_AGENT_DIR") != "" {
			source = sourceEnv("PI_CODING_AGENT_DIR")
		}
		target := filepath.Join(env.GraphHome, "."+provider, "agent")
		for _, name := range []string{"auth.json", "models.json"} {
			if err := copyGraphCredentialFile(filepath.Join(source, name), filepath.Join(target, name)); err != nil {
				return err
			}
		}
		if err := writeGraphJSON(filepath.Join(target, "settings.json"), map[string]any{"extensions": []any{}, "skills": []any{}, "prompts": []any{}, "packages": []any{}}); err != nil {
			return err
		}
		servers := map[string]json.RawMessage{}
		if len(params.McpConfig) > 0 && string(params.McpConfig) != "null" {
			if err := json.Unmarshal(params.McpConfig, &servers); err != nil {
				return err
			}
			if nested, ok := servers["mcpServers"]; ok {
				if err := json.Unmarshal(nested, &servers); err != nil {
					return err
				}
			}
		}
		for key, raw := range servers {
			var server map[string]any
			if err := json.Unmarshal(raw, &server); err != nil {
				return err
			}
			server["exposure"] = "direct"
			encoded, err := json.Marshal(server)
			if err != nil {
				return err
			}
			servers[key] = encoded
		}
		return writeGraphJSON(filepath.Join(target, "mcp.json"), map[string]any{"mcpServers": servers})
	case "kimi":
		for _, layout := range []struct{ folder, key string }{{".kimi", "KIMI_SHARE_DIR"}, {".kimi-code", "KIMI_CODE_HOME"}} {
			source := filepath.Join(home, layout.folder)
			if sourceEnv(layout.key) != "" {
				source = sourceEnv(layout.key)
			}
			target := filepath.Join(env.GraphHome, layout.folder)
			raw, err := os.ReadFile(filepath.Join(source, "config.toml"))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			var original map[string]any
			if err := toml.Unmarshal(raw, &original); err != nil {
				return err
			}
			selected := map[string]any{}
			for _, key := range []string{"models", "providers", "services"} {
				if value, ok := original[key]; ok {
					selected[key] = value
				}
			}
			encoded, err := toml.Marshal(selected)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(target, 0700); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(target, "config.toml"), encoded, 0600); err != nil {
				return err
			}
			for _, name := range []string{"credentials.json", "oauth/kimi-code.json"} {
				if err := copyGraphCredentialFile(filepath.Join(source, name), filepath.Join(target, name)); err != nil {
					return err
				}
			}
		}
	case "qwen":
		source := filepath.Join(home, ".qwen")
		target := filepath.Join(env.GraphHome, ".qwen")
		raw, err := os.ReadFile(filepath.Join(source, "settings.json"))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		original := map[string]any{}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &original); err != nil {
				return err
			}
		}
		selected := map[string]any{}
		for _, key := range []string{"modelProviders", "security"} {
			if value, ok := original[key]; ok {
				selected[key] = value
			}
		}
		// Only authentication may pass through the security section; permissions
		// and workspace trust are scoped to the fresh attempt.
		if security, ok := selected["security"].(map[string]any); ok {
			selected["security"] = map[string]any{"auth": security["auth"]}
		}
		if err := writeGraphJSON(filepath.Join(target, "settings.json"), selected); err != nil {
			return err
		}
		return copyGraphCredentialFile(filepath.Join(source, "oauth_creds.json"), filepath.Join(target, "oauth_creds.json"))
	}
	return prepareGraphExtraProvider(env, params)
}

func copyGraphTOMLFields(source, target string, keys []string) error {
	raw, err := os.ReadFile(source)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	original := map[string]any{}
	if err := toml.Unmarshal(raw, &original); err != nil {
		return err
	}
	selected := map[string]any{}
	for _, key := range keys {
		if value, ok := original[key]; ok {
			selected[key] = value
		}
	}
	encoded, err := toml.Marshal(selected)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	return os.WriteFile(target, encoded, 0600)
}
