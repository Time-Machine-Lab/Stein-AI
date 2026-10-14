package daemon

import (
	"context"
	"encoding/json"
	"github.com/multica-ai/multica/server/internal/graph"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGraphScriptWrappersHaveExplicitJSONInputOutput(t *testing.T) {
	js, err := graphScriptSource("javascript", "return {value: input.value + 1}")
	if err != nil || !strings.Contains(js, "JSON.parse") || !strings.Contains(js, "JSON.stringify") {
		t.Fatalf("invalid JS wrapper: %s %v", js, err)
	}
	py, err := graphScriptSource("python", "output = {'value': input['value'] + 1}")
	if err != nil || !strings.Contains(py, "json.load") || !strings.Contains(py, "json.dumps") {
		t.Fatalf("invalid Python wrapper: %s %v", py, err)
	}
	if _, err := graphScriptSource("shell", "echo nope"); err == nil {
		t.Fatal("accepted unsupported interpreter")
	}
}

func TestGraphScriptRejectsInvalidOutputAndStopsTimeout(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js interpreter not installed")
	}
	for _, tc := range []struct{ name, code string }{
		{"schema", `return {wrong: true}`},
		{"output_limit", `process.stdout.write('x'.repeat(2*1024*1024)); return {summary:'too late'}`},
		{"timeout", `while(true) {}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var submissions atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/graph/node/submissions" {
					submissions.Add(1)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()
			task := graphTestTask(t)
			node := &task.Graph.NodeSpec.Node
			node.Type = graph.NodeTypeScript
			node.OutputSchema = json.RawMessage(`{"type":"object","required":["summary"],"properties":{"summary":{"type":"string"}},"additionalProperties":false}`)
			node.Instructions = ""
			node.TimeoutSeconds = 1
			node.Script = &graph.ScriptConfig{Language: "javascript", Code: tc.code, TimeoutSeconds: 1}
			manifest, err := graph.NewAttemptManifest(task.Graph.NodeSpec, task.Graph.Identity)
			if err != nil {
				t.Fatal(err)
			}
			task.Graph = &manifest
			_, task.GraphManifestHash, err = graph.CanonicalManifest(manifest)
			if err != nil {
				t.Fatal(err)
			}
			task.AuthToken = "mat_graph-script"
			d := &Daemon{cfg: Config{WorkspacesRoot: t.TempDir()}, client: NewClient(server.URL), logger: slog.Default()}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			result, err := d.runGraphScript(ctx, task, slog.Default())
			if err == nil || result.GraphExecutionStopped == nil || !*result.GraphExecutionStopped || submissions.Load() != 0 {
				t.Fatalf("unsafe script outcome: %+v %v submissions %d", result, err, submissions.Load())
			}
		})
	}
}

func TestGraphPythonWrapperExecutesStructuredInput(t *testing.T) {
	binary, err := exec.LookPath("python")
	if err != nil {
		t.Skip("Python interpreter not installed")
	}
	source, err := graphScriptSource("python", `output = {'summary': input['goal']}`)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-c", source)
	cmd.Stdin = strings.NewReader(`{"goal":"build"}`)
	data, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]string
	if json.Unmarshal(data, &value) != nil || value["summary"] != "build" {
		t.Fatalf("invalid Python result %s", data)
	}
}

func TestGraphScriptRunsLocallyAndStagesValidatedOutputUsingTaskCredential(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js interpreter not installed")
	}
	var staged graph.Result
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/graph/node/submissions" {
			if r.Header.Get("Authorization") != "Bearer mat_graph-script" {
				t.Error("script used daemon credential")
			}
			var body struct {
				Result graph.Result `json:"result"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			staged = body.Result
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	task := graphTestTask(t)
	task.Graph.NodeSpec.Node.Type = graph.NodeTypeScript
	task.Graph.NodeSpec.Node.Instructions = ""
	task.Graph.NodeSpec.Node.Script = &graph.ScriptConfig{Language: "javascript", Code: `return {summary: input.goal}`, TimeoutSeconds: 60}
	task.Graph.NodeSpec.Node.TimeoutSeconds = 60
	manifest, err := graph.NewAttemptManifest(task.Graph.NodeSpec, task.Graph.Identity)
	if err != nil {
		t.Fatal(err)
	}
	task.Graph = &manifest
	_, task.GraphManifestHash, err = graph.CanonicalManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	task.AuthToken = "mat_graph-script"
	client := NewClient(server.URL)
	client.SetToken("daemon-token")
	d := &Daemon{cfg: Config{WorkspacesRoot: t.TempDir()}, client: client, logger: slog.Default()}
	result, err := d.runGraphScript(context.Background(), task, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || result.GraphExecutionStopped == nil || !*result.GraphExecutionStopped {
		t.Fatalf("invalid result: %#v", result)
	}
	if string(staged.Output) != `{"summary":"build"}` || staged.Identity.TaskID != task.ID {
		t.Fatalf("wrong staged result: %#v", staged)
	}
}

func TestGraphScriptOutputLimitStopsBeforeUnboundedMemory(t *testing.T) {
	cancelled := false
	w := graphScriptOutput{limit: 8, cancel: func() { cancelled = true }}
	_, err := w.Write([]byte("123456789"))
	if err == nil || !cancelled || w.buffer.Len() > 8 {
		t.Fatal("script output limit was not enforced")
	}
}

func TestGraphScriptSubmissionRetriesIdenticalTaskScopedResult(t *testing.T) {
	var requests atomic.Int32
	var first string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if requests.Add(1) == 1 {
			first = string(raw)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if string(raw) != first || r.Header.Get("Authorization") != "Bearer mat_graph-script" {
			t.Error("retry changed idempotent body or credential")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	task := graphTestTask(t)
	task.AuthToken = "mat_graph-script"
	client := NewClient(server.URL)
	client.SetToken("daemon-token")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := client.submitGraphScriptResult(ctx, task, graph.Result{Identity: task.Graph.Identity, Outcome: graph.OutcomePass, Output: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests=%d", requests.Load())
	}
}
