package execenv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/processtree"
)

// These calls never launch a model. Native agents add owns registry format
// compatibility across the JSON and SQLite OpenClaw releases.
var graphOpenclawCLI = execGraphOpenclawCLI

func execGraphOpenclawCLI(ctx context.Context, bin string, prefix []string, environment map[string]string, args ...string) (string, error) {
	if bin == "" {
		bin = "openclaw"
	}
	cmd := exec.Command(bin, append(append([]string{}, prefix...), args...)...)
	if environment != nil {
		for key, value := range environment {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	var output bytes.Buffer
	cmd.Stdout = &graphBoundedBuffer{buffer: &output, remaining: 4 << 20}
	// Config discovery may print credentials. Never include its stdout/stderr
	// in errors or daemon logs.
	if err := processtree.Run(ctx, cmd, 5*time.Second); err != nil {
		return "", fmt.Errorf("OpenClaw Graph preparation command failed: %w", err)
	}
	return output.String(), nil
}

type graphBoundedBuffer struct {
	buffer    *bytes.Buffer
	remaining int
}

func (w *graphBoundedBuffer) Write(value []byte) (int, error) {
	if len(value) > w.remaining {
		return 0, fmt.Errorf("provider preparation output exceeds limit")
	}
	w.remaining -= len(value)
	return w.buffer.Write(value)
}

func prepareGraphOpenclawHome(env *Environment, params PrepareParams) error {
	if params.Task.GraphEffectiveModel == "" || params.Task.GraphNativeAgentID == "" {
		return fmt.Errorf("Graph OpenClaw requires a published effective model and native agent id")
	}
	if !params.OpenclawGateway.IsZero() {
		return fmt.Errorf("Graph OpenClaw requires a Multica daemon on the execution host using local mode; gateway mode cannot prove isolated checkout, scoped MCP or remote tool termination")
	}
	source := os.Getenv("OPENCLAW_STATE_DIR")
	if value := params.Task.GraphEnvironment["OPENCLAW_STATE_DIR"]; value != "" {
		source = value
	}
	if source == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		source = filepath.Join(home, ".openclaw")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sourceEnvironment := map[string]string{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			sourceEnvironment[key] = value
		}
	}
	for key, value := range params.Task.GraphEnvironment {
		sourceEnvironment[key] = value
	}
	sourceEnvironment["OPENCLAW_STATE_DIR"] = source
	delete(sourceEnvironment, "NODE_OPTIONS")
	delete(sourceEnvironment, "NODE_PATH")
	raw, err := graphOpenclawCLI(ctx, params.OpenclawBin, params.GraphRuntimeArgs, sourceEnvironment, "config", "get", "--json")
	if err != nil {
		return err
	}
	original := map[string]any{}
	if err = json.Unmarshal([]byte(raw), &original); err != nil {
		return fmt.Errorf("invalid OpenClaw provider configuration: %w", err)
	}
	target := filepath.Join(env.GraphHome, ".openclaw")
	id := params.Task.GraphNativeAgentID
	if strings.ContainsAny(id, "/\\.") || strings.TrimSpace(id) == "" {
		return fmt.Errorf("invalid frozen OpenClaw agent id")
	}
	agentDir := filepath.Join(target, "agents", id, "agent")
	if err = os.MkdirAll(agentDir, 0700); err != nil {
		return err
	}
	mcp, _, err := openclawManagedMcpServers(params.McpConfig)
	if err != nil {
		return err
	}
	for _, item := range mcp {
		entry := item.(map[string]any)
		if kind, ok := entry["type"].(string); ok {
			switch kind {
			case "http", "streamable-http":
				entry["transport"] = "streamable-http"
			case "sse":
				entry["transport"] = "sse"
			}
			delete(entry, "type")
		}
	}
	config := map[string]any{}
	for _, key := range []string{"models", "auth", "secrets"} {
		if value, ok := original[key]; ok {
			config[key] = value
		}
	}
	config["agents"] = map[string]any{"defaults": map[string]any{"workspace": env.WorkDir, "model": map[string]any{"primary": params.Task.GraphEffectiveModel, "fallbacks": []any{}}}}
	config["mcp"] = map[string]any{"servers": mcp}
	config["tools"] = map[string]any{"deny": []string{"sessions_spawn", "sessions_send", "cron", "gateway", "message"}}
	env.OpenclawConfigPath = filepath.Join(target, "openclaw.json")
	if err = writeGraphJSON(env.OpenclawConfigPath, config); err != nil {
		return err
	}
	private := map[string]string{}
	for _, key := range []string{"PATH", "SystemRoot", "SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT", "TEMP", "TMP", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY"} {
		if value, ok := os.LookupEnv(key); ok {
			private[key] = value
		}
	}
	private["HOME"], private["USERPROFILE"], private["OPENCLAW_HOME"] = env.GraphHome, env.GraphHome, env.GraphHome
	private["OPENCLAW_STATE_DIR"], private["OPENCLAW_CONFIG_PATH"] = target, env.OpenclawConfigPath
	if _, err = graphOpenclawCLI(ctx, params.OpenclawBin, params.GraphRuntimeArgs, private, "agents", "add", id, "--workspace", env.WorkDir, "--model", params.Task.GraphEffectiveModel, "--agent-dir", agentDir, "--non-interactive", "--json"); err != nil {
		return err
	}
	sourceAgent := filepath.Join(source, "agents", id, "agent")
	// Registered agents can use a non-default credential directory. Resolve
	// only that directory; no source registry/session state is copied.
	if agents, ok := original["agents"].(map[string]any); ok {
		if list, ok := agents["list"].([]any); ok {
			for _, item := range list {
				entry, _ := item.(map[string]any)
				if entry["id"] == id {
					if dir, ok := entry["agentDir"].(string); ok && filepath.IsAbs(dir) {
						sourceAgent = dir
					}
				}
			}
		}
	}
	authPath := filepath.Join(agentDir, "auth-profiles.json")
	if err = mergeGraphOpenclawLegacyAuth([]string{filepath.Join(source, "agents", "main", "agent", "auth-profiles.json"), filepath.Join(sourceAgent, "auth-profiles.json")}, authPath); err != nil {
		return err
	}
	if err = copyGraphOpenclawSQLiteAuth(ctx, source, sourceAgent, authPath); err != nil {
		return err
	}
	if _, err = os.Stat(authPath); err == nil {
		// Official migration imports credential JSON into the version's private
		// SQLite auth store. It never sees the user's shared state directory.
		if _, err = graphOpenclawCLI(ctx, params.OpenclawBin, params.GraphRuntimeArgs, private, "doctor", "--fix", "--non-interactive"); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	// Native registration/doctor may rewrite defaults. Re-apply the frozen
	// configuration after migration, without touching its private registry.
	return writeGraphJSON(env.OpenclawConfigPath, config)
}

// New OpenClaw versions co-locate auth, sessions and transcripts in SQLite.
// Export only the documented credential JSON cells through Node's bundled
// SQLite reader; never copy a database containing another task's history.
func copyGraphOpenclawSQLiteAuth(ctx context.Context, stateRoot, agentDir, target string) error {
	shared, local := filepath.Join(stateRoot, "state", "openclaw.sqlite"), filepath.Join(agentDir, "openclaw-agent.sqlite")
	exists := false
	for _, file := range []string{shared, local} {
		if _, err := os.Stat(file); err == nil {
			exists = true
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if !exists {
		return nil
	}
	script := `const fs=require('node:fs');const {DatabaseSync}=require('node:sqlite');
const [shared,local,target]=process.argv.slice(1);let profiles={};let version=1;
if(fs.existsSync(target)){const value=JSON.parse(fs.readFileSync(target,'utf8'));profiles=value.profiles||{};version=value.version||1;}
function read(file,table,sql){if(!fs.existsSync(file))return;const db=new DatabaseSync(file,{readOnly:true});try{const found=db.prepare("SELECT type FROM sqlite_master WHERE name=?").get(table);if(!found)return;if(found.type!=='table')throw Error('Invalid credential table');const row=db.prepare(sql).get();if(!row)return;const raw=Object.values(row)[0];if(typeof raw!=='string'||raw.length>4*1024*1024)throw Error('Invalid credential cell');const value=JSON.parse(raw);if(!value.profiles||typeof value.profiles!=='object')throw Error('Invalid credential profiles');profiles={...profiles,...value.profiles};version=value.version||version;}finally{db.close()}}
read(shared,'config_machine_state',"SELECT value_json FROM config_machine_state WHERE state_key='authProfiles.store'");
read(local,'auth_profile_store',"SELECT store_json FROM auth_profile_store WHERE store_key='primary'");
if(Object.keys(profiles).length)fs.writeFileSync(target,JSON.stringify({version,profiles}),{mode:0o600});`
	cmd := exec.Command("node", "-e", script, shared, local, target)
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if !strings.EqualFold(key, "NODE_OPTIONS") && !strings.EqualFold(key, "NODE_PATH") {
			cmd.Env = append(cmd.Env, item)
		}
	}
	if err := processtree.Run(ctx, cmd, 5*time.Second); err != nil {
		return fmt.Errorf("read isolated OpenClaw credentials (requires Node.js with node:sqlite): %w", err)
	}
	return nil
}

func mergeGraphOpenclawLegacyAuth(sources []string, target string) error {
	profiles := map[string]any{}
	for _, source := range sources {
		data, err := os.ReadFile(source)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if len(data) > 4<<20 {
			return fmt.Errorf("OpenClaw credential file exceeds 4 MiB")
		}
		var store struct {
			Profiles map[string]any `json:"profiles"`
		}
		if err := json.Unmarshal(data, &store); err != nil {
			return fmt.Errorf("invalid OpenClaw credential store")
		}
		for key, value := range store.Profiles {
			profiles[key] = value
		}
	}
	if len(profiles) == 0 {
		return nil
	}
	return writeGraphJSON(target, map[string]any{"version": 1, "profiles": profiles})
}
