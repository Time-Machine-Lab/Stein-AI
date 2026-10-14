package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGraphClaudeHomePreservesTransportButNotAmbientBehavior(t *testing.T) {
	source := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", source)
	err := os.WriteFile(filepath.Join(source, "settings.json"), []byte(`{"model":"mutable","env":{"ANTHROPIC_BASE_URL":"https://api.deepseek.com/anthropic","ANTHROPIC_AUTH_TOKEN":"live-secret","CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD":"1"},"hooks":{"SessionStart":[{}]},"enabledPlugins":{"ambient":true}}`), 0600)
	if err != nil {
		t.Fatal(err)
	}
	env := &Environment{RootDir: t.TempDir()}
	if err := prepareGraphClaudeHome(env); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(env.ClaudeSettingsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"mutable", "hooks", "ambient", "CLAUDE_CODE_ADDITIONAL"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("ambient behavior escaped: %s", raw)
		}
	}
	if !strings.Contains(string(raw), "api.deepseek.com") || !strings.Contains(string(raw), "live-secret") {
		t.Fatal("transport/auth was lost")
	}
}
