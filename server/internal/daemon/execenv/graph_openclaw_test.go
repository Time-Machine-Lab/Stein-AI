package execenv

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGraphOpenclawRegistersPrivateAgentAndFrozenModel(t *testing.T) {
	source := t.TempDir()
	t.Setenv("OPENCLAW_STATE_DIR", source)
	if err := os.MkdirAll(filepath.Join(source, "agents", "worker", "agent"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "agents", "worker", "agent", "auth-profiles.json"), []byte(`{"version":1,"profiles":{"deepseek:default":{"type":"api_key","provider":"deepseek","key":"secret"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	old := graphOpenclawCLI
	t.Cleanup(func() { graphOpenclawCLI = old })
	calls := 0
	graphOpenclawCLI = func(ctx context.Context, bin string, prefix []string, env map[string]string, args ...string) (string, error) {
		calls++
		if args[0] == "config" {
			if env["OPENCLAW_STATE_DIR"] != source {
				t.Fatal("custom native credential root was not selected")
			}
			return `{"models":{"providers":{"deepseek":{"baseUrl":"https://api.deepseek.com","apiKey":"secret"}}},"agents":{"defaults":{"model":"mutable","workspace":"shared"}},"hooks":{"ambient":true},"channels":{"telegram":{}},"plugins":{"ambient":true}}`, nil
		}
		if env["OPENCLAW_STATE_DIR"] == source || env["OPENCLAW_STATE_DIR"] == "" {
			t.Fatal("native command can mutate shared registry")
		}
		if args[0] == "agents" && args[1] == "add" && (!strings.Contains(strings.Join(args, " "), "--model deepseek/frozen") || !strings.Contains(strings.Join(args, " "), "--agent-dir")) {
			t.Fatalf("model or private agent dir not passed: %v", args)
		}
		return `{}`, nil
	}
	env := &Environment{RootDir: t.TempDir(), WorkDir: t.TempDir(), GraphHome: t.TempDir()}
	params := PrepareParams{Provider: "openclaw", Task: TaskContextForEnv{GraphInstructions: "frozen", GraphNativeAgentID: "worker", GraphEffectiveModel: "deepseek/frozen"}, McpConfig: json.RawMessage(`{"mcpServers":{"pinned":{"type":"http","url":"http://127.0.0.1:9001/mcp"}}}`)}
	if err := prepareGraphOpenclawHome(env, params); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(env.OpenclawConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "ambient") || strings.Contains(string(raw), "mutable") || strings.Contains(string(raw), "$include") {
		t.Fatalf("ambient behavior leaked: %s", raw)
	}
	if !strings.Contains(string(raw), "deepseek/frozen") || !strings.Contains(string(raw), "streamable-http") {
		t.Fatalf("frozen model/MCP missing: %s", raw)
	}
	if calls < 2 {
		t.Fatal("private native agent was not registered")
	}
}

func TestGraphOpenclawSQLiteExportsCredentialsWithoutSessions(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node not installed")
	}
	root := t.TempDir()
	agentDir := filepath.Join(root, "agents", "worker", "agent")
	if err := os.MkdirAll(agentDir, 0700); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(agentDir, "openclaw-agent.sqlite")
	script := `const {DatabaseSync}=require('node:sqlite');const db=new DatabaseSync(process.argv[1]);db.exec("CREATE TABLE auth_profile_store(store_key TEXT,store_json TEXT);CREATE TABLE sessions(content TEXT);INSERT INTO sessions VALUES('ambient conversation')");db.prepare('INSERT INTO auth_profile_store VALUES(?,?)').run('primary',JSON.stringify({version:1,profiles:{'deepseek:default':{type:'api_key',provider:'deepseek',key:'secret'}}}));db.close();`
	if data, err := exec.Command("node", "-e", script, database).CombinedOutput(); err != nil {
		t.Fatalf("sqlite fixture: %v %s", err, data)
	}
	target := filepath.Join(t.TempDir(), "auth-profiles.json")
	if err := copyGraphOpenclawSQLiteAuth(context.Background(), root, agentDir, target); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "ambient") {
		t.Fatalf("invalid credential export: %s", raw)
	}
	if entries, _ := os.ReadDir(filepath.Dir(target)); len(entries) != 1 {
		t.Fatal("native database or history copied")
	}
}
