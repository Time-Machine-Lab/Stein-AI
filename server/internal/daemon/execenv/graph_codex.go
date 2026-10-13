package execenv

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// prepareGraphCodexHome admits transport/auth as live runtime dependencies, while
// excluding instructions, model defaults, MCP, plugins, skills, memory and history.
func prepareGraphCodexHome(home string, opts CodexHomeOptions, logger *slog.Logger) error {
	if entries, err := os.ReadDir(home); err == nil && len(entries) != 0 {
		return fmt.Errorf("Graph requires a fresh Codex home")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0700); err != nil {
		return err
	}
	shared := resolveSharedCodexHome()
	if _, err := os.Stat(filepath.Join(shared, "auth.json")); err == nil {
		if err := ensureSymlink(filepath.Join(shared, "auth.json"), filepath.Join(home, "auth.json")); err != nil {
			return fmt.Errorf("bind Graph Codex auth: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	config := map[string]any{
		"web_search":            "disabled",
		"project_doc_max_bytes": 0,
		"features":              map[string]any{"multi_agent": false, "memories": false, "apps": false, "plugins": false},
		"memories":              map[string]any{"generate_memories": false, "use_memories": false},
	}
	raw, err := os.ReadFile(filepath.Join(shared, "config.toml"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(raw) > 0 {
		var source map[string]any
		if err := toml.Unmarshal(raw, &source); err != nil {
			return fmt.Errorf("read Codex provider transport: %w", err)
		}
		if selected, ok := source["model_provider"].(string); ok && selected != "" {
			config["model_provider"] = selected
			if providers, ok := source["model_providers"].(map[string]any); ok {
				if provider, ok := providers[selected].(map[string]any); ok {
					transport := map[string]any{}
					for _, key := range []string{"name", "base_url", "env_key", "env_key_instructions", "wire_api", "http_headers", "env_http_headers", "requires_openai_auth", "request_max_retries", "stream_max_retries", "stream_idle_timeout_ms"} {
						if value, exists := provider[key]; exists {
							transport[key] = value
						}
					}
					config["model_providers"] = map[string]any{selected: transport}
				}
			}
		}
		// Preserve a user's explicit native Windows isolation opt-in.
		if win, ok := source["windows"].(map[string]any); ok {
			if sandbox, exists := win["sandbox"]; exists {
				config["windows"] = map[string]any{"sandbox": sandbox}
			}
		}
	}
	encoded, err := toml.Marshal(config)
	if err != nil {
		return err
	}
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		return err
	}
	winState := windowsSandboxAbsent
	if resolveGOOS(opts.GOOS) == "windows" {
		winState = resolveWindowsSandboxState(path, nil, statSharedCodexConfig(shared), nil, logger)
	}
	return ensureCodexSandboxConfig(path, codexSandboxPolicyForConfig(opts.GOOS, opts.CodexVersion, winState), opts.CodexVersion, logger)
}

func hydrateGraphCodexSkills(home string, skills []SkillContextForEnv) error {
	return writeSkillFiles(filepath.Join(home, "skills"), skills, nil)
}
