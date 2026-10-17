package execenv

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// isolateGraphCodexConfig removes ambient context from the config copied by the
// ordinary Codex runtime preparation. Provider tables retain native transport/auth
// fields without a Graph-specific field allowlist, including future CLI additions.
func isolateGraphCodexConfig(path string) error {
	config := map[string]any{
		"web_search":            "disabled",
		"project_doc_max_bytes": 0,
		"features":              map[string]any{"multi_agent": false, "memories": false, "apps": false, "plugins": false},
		"memories":              map[string]any{"generate_memories": false, "use_memories": false},
	}
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(raw) > 0 {
		var source map[string]any
		if err := toml.Unmarshal(raw, &source); err != nil {
			// Parser errors can include the source line containing a credential.
			return fmt.Errorf("invalid Codex runtime TOML configuration")
		}
		for _, key := range []string{"cli_auth_credentials_store", "forced_login_method", "forced_chatgpt_workspace_id", "openai_base_url"} {
			if value, exists := source[key]; exists {
				config[key] = value
			}
		}
		if selected, ok := source["model_provider"].(string); ok && selected != "" {
			config["model_provider"] = selected
			if providers, ok := source["model_providers"].(map[string]any); ok {
				if provider, ok := providers[selected].(map[string]any); ok {
					delete(provider, "instructions")
					config["model_providers"] = map[string]any{selected: provider}
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
	if err := os.Chmod(path, 0600); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(path, encoded, 0600)
}

func hydrateGraphCodexSkills(home string, skills []SkillContextForEnv) error {
	return writeSkillFiles(filepath.Join(home, "skills"), skills, nil)
}
