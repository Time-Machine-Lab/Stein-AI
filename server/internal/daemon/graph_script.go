package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/daemon/processtree"
	"github.com/multica-ai/multica/server/internal/graph"
)

func graphScriptSource(language, code string) (string, error) {
	switch language {
	case "javascript":
		return "import fs from 'node:fs';\nconst input=JSON.parse(fs.readFileSync(0,'utf8'));\nconst output=await(async(input)=>{\n" + code + "\n})(input);\nif(output===undefined)throw Error('Script must return JSON output');\nprocess.stdout.write(JSON.stringify(output));\n", nil
	case "python":
		return "import json,sys\ninput=json.load(sys.stdin)\n" + code + "\nif 'output' not in globals(): raise ValueError('Script must assign JSON output')\nsys.stdout.write(json.dumps(output,allow_nan=False))\n", nil
	default:
		return "", fmt.Errorf("unsupported Graph script language %q", language)
	}
}

type graphScriptOutput struct {
	buffer bytes.Buffer
	limit  int
	cancel context.CancelFunc
}

func (w *graphScriptOutput) Write(p []byte) (int, error) {
	if len(p) > w.limit-w.buffer.Len() {
		w.cancel()
		return 0, fmt.Errorf("script output exceeded %d bytes", w.limit)
	}
	return w.buffer.Write(p)
}

func (d *Daemon) runGraphScript(ctx context.Context, task Task, taskLog *slog.Logger) (result TaskResult, returnErr error) {
	stopped := true
	defer func() { result.GraphExecutionStopped = &stopped }()
	if err := validateGraphTask(task); err != nil {
		return result, err
	}
	if err := applyFrozenGraphAgent(&task); err != nil {
		return result, err
	}
	script := task.Graph.NodeSpec.Node.Script
	if script == nil || script.TimeoutSeconds < 1 || script.TimeoutSeconds > 3600 {
		return result, fmt.Errorf("invalid bounded Graph script")
	}
	source, err := graphScriptSource(script.Language, script.Code)
	if err != nil {
		return result, err
	}
	executable, extension := "node", "mjs"
	if script.Language == "python" {
		executable, extension = "python3", "py"
		if runtime.GOOS == "windows" {
			executable = "python"
		}
		if _, err := exec.LookPath(executable); err != nil {
			executable = "python"
		}
	}
	binary, err := exec.LookPath(executable)
	if err != nil {
		return result, fmt.Errorf("Graph script interpreter %s is not installed on this runtime", executable)
	}
	prepareCtx, cancelPrepare := context.WithTimeout(ctx, d.effectiveTaskPrepareTimeout())
	defer cancelPrepare()
	stopLease := d.startTaskPrepareLeaseExtender(prepareCtx, task, taskLog)
	defer stopLease()
	env, err := execenv.Prepare(execenv.PrepareParams{WorkspacesRoot: d.cfg.WorkspacesRoot, WorkspaceID: task.WorkspaceID, TaskID: task.ID, AgentName: task.Agent.Name, Provider: "script", Task: execenv.TaskContextForEnv{GraphInstructions: "Execute only the frozen script."}}, taskLog)
	if err != nil {
		return result, err
	}
	result.WorkDir = env.WorkDir
	result.EnvRoot = env.RootDir
	env.GraphHome = filepath.Join(env.RootDir, "graph-home")
	if err = os.MkdirAll(env.GraphHome, 0700); err != nil {
		return result, err
	}
	childEnv, err := graphExecutionEnvironment("script", env, nil, task.Agent.CustomEnv)
	if err != nil {
		return result, err
	}
	// Script execution has no model account. Keep only process/platform
	// plumbing; explicit frozen environment bindings are added below.
	keep := map[string]bool{}
	for _, key := range []string{"PATH", "SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT", "TEMP", "TMP", "TMPDIR", "LANG", "LC_ALL", "LC_CTYPE", "TZ", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "SSL_CERT_FILE", "SSL_CERT_DIR", "NODE_EXTRA_CA_CERTS", "HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "APPDATA", "LOCALAPPDATA"} {
		keep[key] = true
	}
	for key := range childEnv {
		if !keep[key] {
			delete(childEnv, key)
		}
	}
	for key, value := range task.Agent.CustomEnv {
		if graphBlockedEnvironmentKey(key) || graphProviderHomeSelector(key) {
			return result, fmt.Errorf("Graph script environment key %s is reserved", key)
		}
		childEnv[key] = value
	}
	file := filepath.Join(env.RootDir, "graph-script."+extension)
	if err = os.WriteFile(file, []byte(source), 0600); err != nil {
		return result, err
	}
	if err = d.client.StartTask(prepareCtx, task.ID); err != nil {
		return result, fmt.Errorf("start task failed: %w", err)
	}
	stopLease()
	cancelPrepare()
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(script.TimeoutSeconds)*time.Second)
	defer cancel()
	output := graphScriptOutput{limit: graph.MaxJSONBytes, cancel: cancel}
	diagnostic := graphScriptOutput{limit: 64 << 10, cancel: cancel}
	cmd := exec.Command(binary, file)
	cmd.Dir = env.WorkDir
	cmd.Env = make([]string, 0, len(childEnv))
	for key, value := range childEnv {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	cmd.Stdin = bytes.NewReader(task.Graph.NodeSpec.Input)
	cmd.Stdout = &output
	cmd.Stderr = &diagnostic
	stopped = false
	err = processtree.Run(runCtx, cmd, 5*time.Second)
	var unknown *processtree.UnconfirmedStopError
	stopped = !errors.As(err, &unknown)
	if err != nil {
		result.Status = "failed"
		if ctx.Err() != nil {
			result.Status = "cancelled"
		}
		return result, fmt.Errorf("Graph script failed: %w: %s", err, strings.TrimSpace(diagnostic.buffer.String()))
	}
	candidate := graph.Result{Identity: task.Graph.Identity, Outcome: graph.OutcomePass, Output: json.RawMessage(output.buffer.Bytes())}
	if err = graph.ValidateResult(task.Graph.NodeSpec.Node, candidate); err != nil {
		return result, fmt.Errorf("Graph script output: %w", err)
	}
	if err = d.client.submitGraphScriptResult(ctx, task, candidate); err != nil {
		return result, err
	}
	result.Status = "completed"
	result.Comment = string(candidate.Output)
	return result, nil
}

func (c *Client) submitGraphScriptResult(ctx context.Context, task Task, result graph.Result) error {
	for attempt := 0; ; attempt++ {
		err := c.submitGraphScriptResultOnce(ctx, task, result)
		if err == nil || !isTransientError(err) || attempt >= len(defaultTerminalRetrySchedule) {
			return err
		}
		if retrySleep(ctx, defaultTerminalRetrySchedule[attempt]) != nil {
			return err
		}
	}
}

func (c *Client) submitGraphScriptResultOnce(ctx context.Context, task Task, result graph.Result) error {
	token, err := taskScopedAuthToken(task)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{"idempotency_key": "script-" + task.ID, "result": result})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/graph/node/submissions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Workspace-ID", task.WorkspaceID)
	c.setIdentityHeaders(req)
	req.Header.Set("X-Client-Capabilities", daemonClientCapabilities()+",graph-node-submit-v1")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &requestError{Method: http.MethodPost, Path: req.URL.Path, StatusCode: resp.StatusCode, Body: string(raw)}
	}
	_, err = io.Copy(io.Discard, resp.Body)
	return err
}
