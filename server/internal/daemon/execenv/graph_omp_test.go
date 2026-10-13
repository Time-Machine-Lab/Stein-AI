package execenv

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func graphOMPTestHome(t *testing.T) string {
	t.Helper()
	home := graphExtraTestHome(t)
	for _, key := range []string{"PI_CONFIG_DIR", "PI_CODING_AGENT_DIR", "OMP_PROFILE", "PI_PROFILE", "PI_CONFIG_FILES", "OMP_AUTH_BROKER_URL", "OMP_AUTH_BROKER_TOKEN"} {
		t.Setenv(key, "")
	}
	return home
}

func TestGraphOMPModernYAMLAndFrozenMCP(t *testing.T) {
	for _, suffix := range []string{"yml", "yaml"} {
		t.Run(suffix, func(t *testing.T) {
			home := graphOMPTestHome(t)
			source := filepath.Join(home, ".omp", "agent")
			graphExtraWrite(t, filepath.Join(source, "models."+suffix), "providers:\n  custom:\n    baseUrl: https://gateway.example/v1\n    api: openai-completions\n    apiKey: CUSTOM_TOKEN\n    models:\n      - id: work\n        contextWindow: 32000\n")
			graphExtraWrite(t, filepath.Join(source, "config."+suffix), "auth:\n  broker:\n    url: https://auth.example\n    token: old-token\nmodelRoles:\n  default: ambient-model\nplugins: [ambient-plugin]\nmcp:\n  enableProjectConfig: true\nbrowser:\n  enabled: true\n")
			graphExtraWrite(t, filepath.Join(source, "mcp.json"), `{"mcpServers":{"ambient":{"command":"ambient-tool"}}}`)
			env := &Environment{GraphHome: t.TempDir()}
			params := PrepareParams{Provider: "omp", Task: TaskContextForEnv{GraphEnvironment: map[string]string{"CUSTOM_TOKEN": "frozen-key", "OMP_AUTH_BROKER_TOKEN": "frozen-broker"}}, McpConfig: json.RawMessage(`{"mcpServers":{"bound":{"type":"http","url":"https://mcp.example","headers":{"Authorization":"Bearer frozen-mcp"}}}}`)}
			if err := prepareGraphPortableProvider(env, params); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(env.GraphHome, ".omp", "agent")
			models, err := os.ReadFile(filepath.Join(target, "models.yml"))
			if err != nil {
				t.Fatalf("modern provider catalog missing: %v", err)
			}
			if !bytes.Contains(models, []byte("frozen-key")) || bytes.Contains(models, []byte("CUSTOM_TOKEN")) {
				t.Fatalf("provider credential was not resolved: %s", models)
			}
			raw, err := os.ReadFile(filepath.Join(target, "config.yml"))
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(raw, []byte("ambient")) || bytes.Contains(raw, []byte("old-token")) {
				t.Fatalf("live configuration imported: %s", raw)
			}
			var config map[string]any
			if err := yaml.Unmarshal(raw, &config); err != nil {
				t.Fatal(err)
			}
			if config["mcp"].(map[string]any)["enableProjectConfig"] != false || config["browser"].(map[string]any)["enabled"] != false {
				t.Fatal("native MCP isolation missing")
			}
			mcp, err := os.ReadFile(filepath.Join(target, "mcp.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(mcp, []byte("frozen-mcp")) || bytes.Contains(mcp, []byte("ambient")) {
				t.Fatalf("MCP was not replaced: %s", mcp)
			}
		})
	}
}

func TestGraphOMPSourceSelectionAndUnsafeConfig(t *testing.T) {
	cases := []struct {
		name, models string
		environment  map[string]string
	}{
		{"command credential", "providers:\n  custom:\n    apiKey: '!echo secret'\n", nil},
		{"command header", "providers:\n  custom:\n    headers:\n      Authorization: '!echo secret'\n", nil},
		{"unknown provider field", "providers:\n  custom:\n    futurePlugin: ambient-extension\n", nil},
		{"missing explicit directory", "", map[string]string{"PI_CODING_AGENT_DIR": "MISSING"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := graphOMPTestHome(t)
			source := filepath.Join(home, ".omp", "agent")
			if tc.models != "" {
				graphExtraWrite(t, filepath.Join(source, "models.yml"), tc.models)
			}
			if tc.environment != nil {
				tc.environment["PI_CODING_AGENT_DIR"] = filepath.Join(home, "missing")
			}
			err := prepareGraphPortableProvider(&Environment{GraphHome: t.TempDir()}, PrepareParams{Provider: "omp", Task: TaskContextForEnv{GraphEnvironment: tc.environment}})
			if err == nil {
				t.Fatal("unsafe or unavailable native configuration accepted")
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("error exposed credential")
			}
		})
	}
}

func graphOMPNode(t *testing.T, script string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", append([]string{"--no-warnings", "-e", script}, args...)...)
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if !strings.EqualFold(key, "NODE_OPTIONS") && !strings.EqualFold(key, "NODE_PATH") {
			cmd.Env = append(cmd.Env, item)
		}
	}
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("SQLite fixture: %v: %s", err, raw)
	}
	return raw
}

func graphOMPRequireSQLite(t *testing.T) {
	t.Helper()
	cmd := exec.Command("node", "--no-warnings", "-e", "require('node:sqlite')")
	if err := cmd.Run(); err != nil {
		t.Skip("Node.js with node:sqlite required for native SQLite fixture")
	}
}

const graphOMPFixtureSchema = `CREATE TABLE auth_credentials(id INTEGER PRIMARY KEY,provider TEXT NOT NULL,credential_type TEXT NOT NULL,data TEXT NOT NULL,disabled_cause TEXT,identity_key TEXT,created_at INTEGER,updated_at INTEGER); CREATE TABLE auth_schema_version(id INTEGER PRIMARY KEY,version INTEGER); INSERT INTO auth_schema_version VALUES(1,8); CREATE TABLE sessions(secret TEXT); INSERT INTO sessions VALUES('unrelated-history');`

func TestGraphOMPCredentialOnlySQLiteProjection(t *testing.T) {
	graphOMPRequireSQLite(t)
	home := graphOMPTestHome(t)
	source := filepath.Join(home, ".omp", "agent", "agent.db")
	if err := os.MkdirAll(filepath.Dir(source), 0700); err != nil {
		t.Fatal(err)
	}
	graphOMPNode(t, `const {DatabaseSync}=require('node:sqlite');const db=new DatabaseSync(process.argv[1]);db.exec(process.argv[2]);const q=db.prepare('INSERT INTO auth_credentials VALUES(?,?,?,?,?,?,1,1)');q.run(1,'custom','api_key',JSON.stringify({key:'test-key',source:'login'}),null,null);q.run(2,'oauth','oauth',JSON.stringify({access:'access',refresh:'refresh',expires:9999999999999}),null,'account:fixture');q.run(3,'disabled','api_key',JSON.stringify({key:'disabled-secret'}),'revoked',null);db.close();`, source, graphOMPFixtureSchema)
	before, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	// The helper must not run ambient Node preload code while reading credentials.
	t.Setenv("NODE_OPTIONS", "--require=graph-preload-must-not-run")
	env := &Environment{GraphHome: t.TempDir()}
	if err := prepareGraphPortableProvider(env, PrepareParams{Provider: "omp"}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(env.GraphHome, ".omp", "agent", "agent.db")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("credential database missing: %v", err)
	}
	result := graphOMPNode(t, `const {DatabaseSync}=require('node:sqlite');const db=new DatabaseSync(process.argv[1],{readOnly:true});const names=db.prepare("SELECT name FROM sqlite_master WHERE type='table'").all().map(x=>x.name);const rows=db.prepare('SELECT provider,credential_type,data FROM auth_credentials ORDER BY provider').all();process.stdout.write(JSON.stringify({names,rows}));db.close();`, target)
	if bytes.Contains(result, []byte("sessions")) || bytes.Contains(result, []byte("disabled")) || !bytes.Contains(result, []byte("test-key")) || !bytes.Contains(result, []byte("refresh")) {
		t.Fatalf("bad credential projection: %s", result)
	}
	after, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("source database was modified")
	}
}

func TestGraphOMPRejectsUnknownSQLiteSchema(t *testing.T) {
	graphOMPRequireSQLite(t)
	for _, change := range []string{"UPDATE auth_schema_version SET version=999", "ALTER TABLE auth_credentials ADD COLUMN unknown_auth TEXT", "INSERT INTO auth_credentials VALUES(1,'x','future','{}',NULL,NULL,1,1)"} {
		t.Run(change, func(t *testing.T) {
			home := graphOMPTestHome(t)
			source := filepath.Join(home, ".omp", "agent", "agent.db")
			if err := os.MkdirAll(filepath.Dir(source), 0700); err != nil {
				t.Fatal(err)
			}
			graphOMPNode(t, `const {DatabaseSync}=require('node:sqlite');const db=new DatabaseSync(process.argv[1]);db.exec(process.argv[2]);db.close();`, source, graphOMPFixtureSchema+change)
			if err := prepareGraphPortableProvider(&Environment{GraphHome: t.TempDir()}, PrepareParams{Provider: "omp"}); err == nil {
				t.Fatal("unknown authentication schema silently ignored")
			}
		})
	}
}

func TestGraphOMPExplicitEmptyBrokerTokenDoesNotRestoreAmbientFile(t *testing.T) {
	home := graphOMPTestHome(t)
	graphExtraWrite(t, filepath.Join(home, ".omp", "agent", "config.yml"), "auth:\n  broker:\n    url: https://auth.example\n")
	graphExtraWrite(t, filepath.Join(home, ".omp", "auth-broker.token"), "ambient-token")
	if err := prepareGraphOMPHome(&Environment{GraphHome: t.TempDir()}, PrepareParams{Provider: "omp", Task: TaskContextForEnv{GraphEnvironment: map[string]string{"OMP_AUTH_BROKER_TOKEN": ""}}}); err == nil {
		t.Fatal("explicit empty token restored ambient broker credential")
	}
}

func TestGraphOMPBoundsExpandedProviderCredentials(t *testing.T) {
	home := graphOMPTestHome(t)
	graphExtraWrite(t, filepath.Join(home, ".omp", "agent", "models.yml"), "providers:\n  first:\n    apiKey: BIG_TOKEN\n  second:\n    apiKey: BIG_TOKEN\n")
	err := prepareGraphOMPHome(&Environment{GraphHome: t.TempDir()}, PrepareParams{Provider: "omp", Task: TaskContextForEnv{GraphEnvironment: map[string]string{"BIG_TOKEN": strings.Repeat("x", 3<<20)}}})
	if err == nil {
		t.Fatal("expanded provider credentials exceeded budget")
	}
}

func TestGraphOMPReadsNativeDotenvCredentialsOnly(t *testing.T) {
	home := graphOMPTestHome(t)
	graphExtraWrite(t, filepath.Join(home, ".omp", "agent", "models.yml"), "providers:\n  custom:\n    apiKey: MY_PRIVATE_KEY\n")
	graphExtraWrite(t, filepath.Join(home, ".omp", ".env"), "MY_PRIVATE_KEY='root-secret'\nANTHROPIC_API_KEY=provider-secret\nOMP_AUTH_BROKER_URL=https://broker.example\nOMP_AUTH_BROKER_TOKEN=broker-secret\nPI_SLOW_MODEL=ambient-model\nNODE_OPTIONS=ambient-hook\n")
	graphExtraWrite(t, filepath.Join(home, ".omp", "agent", ".env"), "export MY_PRIVATE_KEY=\"agent-secret\"\n")
	env := &Environment{GraphHome: t.TempDir()}
	if err := prepareGraphOMPHome(env, PrepareParams{Provider: "omp"}); err != nil {
		t.Fatal(err)
	}
	models, err := os.ReadFile(filepath.Join(env.GraphHome, ".omp", "agent", "models.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(models, []byte("agent-secret")) {
		t.Fatal("native dotenv credential reference was lost")
	}
	dotenv, err := os.ReadFile(filepath.Join(env.GraphHome, ".omp", "agent", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(dotenv, []byte("provider-secret")) || bytes.Contains(dotenv, []byte("ambient")) {
		t.Fatalf("dotenv was not projected: %s", dotenv)
	}
}

func TestGraphOMPSelectsProfileAndYAMLPrecedence(t *testing.T) {
	home := graphOMPTestHome(t)
	source := filepath.Join(home, ".omp", "profiles", "work", "agent")
	graphExtraWrite(t, filepath.Join(source, "models.yml"), "providers:\n  custom:\n    apiKey: canonical\n")
	graphExtraWrite(t, filepath.Join(source, "models.yaml"), "providers:\n  custom:\n    apiKey: wrong-fallback\n")
	graphExtraWrite(t, filepath.Join(home, ".omp", "agent", "models.yml"), "providers:\n  custom:\n    apiKey: wrong-default\n")
	env := &Environment{GraphHome: t.TempDir()}
	if err := prepareGraphOMPHome(env, PrepareParams{Provider: "omp", Task: TaskContextForEnv{GraphEnvironment: map[string]string{"OMP_PROFILE": "work", "PI_CODING_AGENT_DIR": "ignored-for-named-profile"}}}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(env.GraphHome, ".omp", "agent", "models.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("canonical")) || bytes.Contains(raw, []byte("wrong")) {
		t.Fatal("wrong native profile or fallback selected")
	}
}

func TestGraphOMPReadsCommittedWALWithoutCopyingHistory(t *testing.T) {
	graphOMPRequireSQLite(t)
	home := graphOMPTestHome(t)
	source := filepath.Join(home, ".omp", "agent", "agent.db")
	if err := os.MkdirAll(filepath.Dir(source), 0700); err != nil {
		t.Fatal(err)
	}
	graphOMPNode(t, `const {DatabaseSync}=require('node:sqlite');const db=new DatabaseSync(process.argv[1]);db.exec(process.argv[2]);db.close();`, source, graphOMPFixtureSchema)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "--no-warnings", "-e", `const {DatabaseSync}=require('node:sqlite');const db=new DatabaseSync(process.argv[1]);db.exec('PRAGMA journal_mode=WAL');db.prepare('INSERT INTO auth_credentials VALUES(1,?,?,?,?,?,1,1)').run('wal-provider','api_key',JSON.stringify({key:'wal-key'}),null,null);process.stdout.write('ready\n');setInterval(()=>{},1000);`, source)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	if ready, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || ready != "ready\n" {
		t.Fatalf("WAL fixture did not become ready: %v", err)
	}
	env := &Environment{GraphHome: t.TempDir()}
	if err := prepareGraphOMPHome(env, PrepareParams{Provider: "omp"}); err != nil {
		t.Fatal(err)
	}
	raw := graphOMPNode(t, `const {DatabaseSync}=require('node:sqlite');const db=new DatabaseSync(process.argv[1],{readOnly:true});process.stdout.write(db.prepare('SELECT data FROM auth_credentials WHERE provider=?').get('wal-provider').data);db.close();`, filepath.Join(env.GraphHome, ".omp", "agent", "agent.db"))
	if !bytes.Contains(raw, []byte("wal-key")) {
		t.Fatal("committed WAL credential was lost")
	}
}

func TestGraphOMPSQLiteCredentialBudget(t *testing.T) {
	graphOMPRequireSQLite(t)
	home := graphOMPTestHome(t)
	source := filepath.Join(home, ".omp", "agent", "agent.db")
	if err := os.MkdirAll(filepath.Dir(source), 0700); err != nil {
		t.Fatal(err)
	}
	graphOMPNode(t, `const {DatabaseSync}=require('node:sqlite');const db=new DatabaseSync(process.argv[1]);db.exec(process.argv[2]);db.prepare('INSERT INTO auth_credentials VALUES(1,?,?,?,?,?,1,1)').run('large','api_key',JSON.stringify({key:'x'.repeat(4*1024*1024)}),null,null);db.close();`, source, graphOMPFixtureSchema)
	if err := prepareGraphOMPHome(&Environment{GraphHome: t.TempDir()}, PrepareParams{Provider: "omp"}); err == nil {
		t.Fatal("oversized credential database accepted")
	}
}

func TestGraphOMPBrokerDoesNotImportUnusedLocalDatabase(t *testing.T) {
	home := graphOMPTestHome(t)
	graphExtraWrite(t, filepath.Join(home, ".omp", "agent", "agent.db"), "unused local database")
	env := &Environment{GraphHome: t.TempDir()}
	params := PrepareParams{Provider: "omp", Task: TaskContextForEnv{GraphEnvironment: map[string]string{"OMP_AUTH_BROKER_URL": "https://broker.example", "OMP_AUTH_BROKER_TOKEN": "test-token"}}}
	if err := prepareGraphOMPHome(env, params); err != nil {
		t.Fatalf("native broker mode incorrectly used local credentials: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.GraphHome, ".omp", "agent", "agent.db")); !os.IsNotExist(err) {
		t.Fatal("unused local credential database was imported")
	}
}
