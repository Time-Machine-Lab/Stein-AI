package execenv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Graph Claude launches in documented --bare mode. Only authentication and
// provider transport are inherited; instructions, hooks, plugins and model
// defaults remain excluded. Task-scoped managed MCP and Skills are explicit.
func prepareGraphClaudeHome(env *Environment) error {
	source := os.Getenv("CLAUDE_CONFIG_DIR")
	if source == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		source = filepath.Join(home, ".claude")
	}
	env.GraphHome = filepath.Join(env.RootDir, "graph-home")
	target := filepath.Join(env.GraphHome, ".claude")
	if err := os.MkdirAll(target, 0700); err != nil {
		return err
	}
	settings := map[string]any{"disableAllHooks": true, "autoMemoryEnabled": false}
	var sourceSettings struct {
		Env map[string]string `json:"env"`
	}
	raw, err := os.ReadFile(filepath.Join(source, "settings.json"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(raw) > 0 && json.Unmarshal(raw, &sourceSettings) != nil {
		return fmt.Errorf("invalid Claude provider settings")
	}
	transport := map[string]string{}
	for _, key := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "ANTHROPIC_CUSTOM_HEADERS", "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY", "AWS_REGION", "AWS_DEFAULT_REGION", "ANTHROPIC_VERTEX_PROJECT_ID", "CLOUD_ML_REGION", "ANTHROPIC_FOUNDRY_RESOURCE"} {
		if value, ok := sourceSettings.Env[key]; ok {
			transport[key] = value
		}
	}
	settings["env"] = transport
	for _, name := range []string{".credentials.json"} {
		path := filepath.Join(source, name)
		if _, err := os.Stat(path); err == nil {
			if err := ensureSymlink(path, filepath.Join(target, name)); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	env.ClaudeSettingsPath = filepath.Join(target, "graph-settings.json")
	return os.WriteFile(env.ClaudeSettingsPath, encoded, 0600)
}
