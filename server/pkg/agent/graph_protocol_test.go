package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func runGraphProtocolHelper(provider string) {
	if provider == "openclaw" && slices.Contains(os.Args, "--version") {
		fmt.Println("2026.10.1")
		return
	}
	if provider == "pi" && slices.Contains(os.Args, "--help") {
		fmt.Println("mcp list - validate configured servers")
		return
	}
	if provider == "pi" && slices.Contains(os.Args, "mcp") {
		if os.Getenv("GRAPH_FAKE_MCP_FAIL") != "" {
			os.Exit(1)
		}
		raw, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".pi", "agent", "mcp.json"))
		if err != nil || !strings.Contains(string(raw), "task-token") {
			os.Exit(52)
		}
		fmt.Println("frozen connected")
		return
	}
	if provider == "descendant" {
		time.Sleep(time.Minute)
		return
	}
	if os.Getenv("GRAPH_TEST_AMBIENT") != "" {
		os.Exit(41)
	}
	if os.Getenv("GRAPH_FAKE_WAIT") != "" {
		self, _ := os.Executable()
		child := exec.Command(self)
		child.Env = []string{"GRAPH_FAKE_PROTOCOL=descendant"}
		hideAgentWindow(child)
		if err := child.Start(); err != nil {
			os.Exit(44)
		}
		time.Sleep(time.Minute)
		return
	}
	switch provider {
	case "codebuddy", "qwen", "cursor":
		if provider != "qwen" {
			_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		}
		if !slices.Contains(os.Args, "frozen-model") {
			os.Exit(43)
		}
		fmt.Println(`{"type":"result","subtype":"success","is_error":false,"session_id":"graph-stream","result":"done","duration_ms":1}`)
		return
	case "copilot":
		if os.Getenv("GRAPH_FAKE_EXPECT_MCP") != "" {
			index := slices.Index(os.Args, "--additional-mcp-config")
			if index < 0 || index+1 >= len(os.Args) || !slices.Contains(os.Args, "--disable-builtin-mcps") {
				os.Exit(47)
			}
			raw, err := os.ReadFile(strings.TrimPrefix(os.Args[index+1], "@"))
			if err != nil || !strings.Contains(string(raw), "task-token") {
				os.Exit(48)
			}
		}
		fmt.Println(fixtureAssistantMessage)
		fmt.Println(fixtureResult)
		return
	case "opencode", "deveco":
		if provider == "deveco" && os.Getenv("GRAPH_FAKE_EXPECT_MCP") != "" && !strings.Contains(os.Getenv("DEVECO_CONFIG_CONTENT"), "https://scoped.example/mcp") {
			os.Exit(49)
		}
		fmt.Println(`{"type":"text","sessionID":"graph-stream","part":{"type":"text","text":"done"}}`)
		fmt.Println(`{"type":"step_finish","sessionID":"graph-stream","part":{"type":"step-finish","reason":"stop","tokens":{"input":1,"output":1}}}`)
		return
	case "pi", "omp":
		if os.Getenv("GRAPH_FAKE_EXPECT_MCP") != "" {
			for _, flag := range []string{"--no-extensions", "--no-skills"} {
				if !slices.Contains(os.Args, flag) {
					os.Exit(54)
				}
			}
		}
		if provider == "pi" && os.Getenv("GRAPH_FAKE_EXPECT_MCP") != "" {
			for _, flag := range []string{"--no-approve", "--no-context-files", "builtin:mcp"} {
				if !slices.Contains(os.Args, flag) {
					os.Exit(55)
				}
			}
		}
		if provider == "omp" && os.Getenv("GRAPH_FAKE_EXPECT_MCP") != "" {
			index := slices.Index(os.Args, "--config")
			if index < 0 || index+1 >= len(os.Args) || os.Args[index+1] != filepath.Join(os.Getenv("HOME"), ".omp", "agent", "config.yml") || os.Getenv("OMP_MCP_REQUIRE_READY") != "1" || os.Getenv("OMP_MCP_TIMEOUT_MS") == "0" {
				os.Exit(53)
			}
		}
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		fmt.Println(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`)
		fmt.Println(`{"type":"turn_end","message":{"role":"assistant","content":[{"type":"text","text":"done"}],"model":"frozen-model","stopReason":"stop","usage":{"input":1,"output":1}}}`)
		fmt.Println(`{"type":"agent_end","willRetry":false}`)
		return
	case "openclaw":
		fmt.Println(`{"payloads":[{"text":"done"}],"meta":{"durationMs":1,"agentMeta":{"sessionId":"graph-openclaw","model":"frozen-model"}}}`)
		return
	case "antigravity":
		if !slices.Contains(os.Args, "stream-json") || !slices.Contains(os.Args, "frozen-model") {
			os.Exit(43)
		}
		if os.Getenv("GRAPH_FAKE_AGY") == "legacy" {
			fmt.Println("unstructured success")
			return
		}
		model := "frozen-model"
		if os.Getenv("GRAPH_FAKE_AGY") == "wrong-model" {
			model = "ambient-model"
		}
		fmt.Printf("{\"event\":\"init\",\"init\":{\"model\":%q}}\n", model)
		if os.Getenv("GRAPH_FAKE_AGY") == "missing-result" {
			return
		}
		status := "SUCCESS"
		if os.Getenv("GRAPH_FAKE_AGY") == "provider-failure" {
			status = "ERROR"
		}
		fmt.Printf("{\"event\":\"result\",\"result\":{\"conversation_id\":\"graph-agy\",\"status\":%q,\"response\":\"done\"}}\n", status)
		return
	case "dsh":
		fmt.Println(`{"v":1,"type":"ready","runtime":"dsh","capabilities":{}}`)
		var req map[string]any
		_ = json.NewDecoder(os.Stdin).Decode(&req)
		data, _ := json.Marshal(map[string]any{"v": 1, "type": "result", "request_id": req["request_id"], "status": "completed", "output": "done"})
		fmt.Println(string(data))
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(42)
		}
		var result any = map[string]any{}
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{"mcpCapabilities": map[string]bool{"http": true, "sse": true}}, "authMethods": []any{map[string]string{"id": "xai.api_key", "name": "API key"}}}
		case "session/new":
			result = map[string]any{"sessionId": "graph-session", "models": map[string]any{"currentModelId": "frozen-model", "availableModels": []any{map[string]string{"modelId": "frozen-model", "name": "Frozen"}}}}
		case "session/prompt":
			fmt.Println(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"graph-session","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"done"}}}}`)
			result = map[string]string{"stopReason": "end_turn"}
		}
		if request.Method == "initialize" && os.Getenv("GRAPH_FAKE_NO_MCP") != "" {
			result = map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{"mcpCapabilities": map[string]bool{"http": false}}, "authMethods": []any{map[string]string{"id": "xai.api_key"}}}
		}
		if len(request.ID) > 0 {
			raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
			fmt.Println(string(raw))
		}
	}
}

func TestGraphLocalAdaptersCancellationStopsDescendants(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"hermes", "kimi", "kiro", "qoder", "qoderclicn", "traecli", "grok", "reasonix", "qwenpaw", "codebuddy", "qwen", "cursor", "copilot", "opencode", "deveco", "pi", "omp", "dsh", "openclaw", "antigravity"} {
		t.Run(provider, func(t *testing.T) {
			backend, err := ResolveBackend(provider, Config{ExecutablePath: self, ReplaceEnvironment: true, TaskID: "graph-cancel", Env: map[string]string{"GRAPH_FAKE_PROTOCOL": provider, "GRAPH_FAKE_WAIT": "1", "HOME": t.TempDir()}})
			if err != nil {
				t.Fatal(err)
			}
			model := "frozen-model"
			if provider == "dsh" {
				model = "deepseek/frozen-model"
			}
			session, err := backend.Execute(context.Background(), "frozen work", ExecOptions{Model: model, Cwd: t.TempDir(), Timeout: 250 * time.Millisecond})
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				for range session.Messages {
				}
			}()
			select {
			case result := <-session.Result:
				if result.Status == "completed" || result.ExecutionStopped == nil || !*result.ExecutionStopped {
					t.Fatalf("unconfirmed cancel: %+v", result)
				}
			case <-time.After(6 * time.Second):
				t.Fatal("owned descendant still holds attempt open")
			}
		})
	}
}

func TestGraphStreamAdaptersUseOwnedProcessBoundary(t *testing.T) {
	t.Setenv("GRAPH_TEST_AMBIENT", "not-visible")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"codebuddy", "qwen", "cursor", "copilot", "opencode", "deveco", "pi", "omp", "dsh", "openclaw", "antigravity"} {
		t.Run(provider, func(t *testing.T) {
			backend, err := ResolveBackend(provider, Config{ExecutablePath: self, ReplaceEnvironment: true, TaskID: "graph-stream", Env: map[string]string{"GRAPH_FAKE_PROTOCOL": provider, "HOME": t.TempDir()}})
			if err != nil {
				t.Fatal(err)
			}
			model := "frozen-model"
			if provider == "dsh" {
				model = "deepseek/frozen-model"
			}
			session, err := backend.Execute(context.Background(), "frozen work", ExecOptions{Model: model, Cwd: t.TempDir(), Timeout: 3 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				for range session.Messages {
				}
			}()
			select {
			case result := <-session.Result:
				if result.Status != "completed" || result.ExecutionStopped == nil || !*result.ExecutionStopped {
					t.Fatalf("invalid process receipt: %+v", result)
				}
			case <-time.After(6 * time.Second):
				t.Fatal("adapter failed to complete")
			}
		})
	}
}

func TestGraphACPAdaptersUseIsolatedEnvironmentAndPhysicalStopBoundary(t *testing.T) {
	t.Setenv("GRAPH_TEST_AMBIENT", "not-visible")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"hermes", "kimi", "kiro", "qoder", "qoderclicn", "traecli", "grok", "reasonix", "qwenpaw"} {
		t.Run(provider, func(t *testing.T) {
			private := t.TempDir()
			backend, err := ResolveBackend(provider, Config{ExecutablePath: self, ReplaceEnvironment: true, Env: map[string]string{"GRAPH_FAKE_PROTOCOL": provider, "XAI_API_KEY": "fake-test-key", "QWENPAW_WORKING_DIR": private}})
			if err != nil {
				t.Fatal(err)
			}
			session, err := backend.Execute(context.Background(), "frozen work", ExecOptions{Model: "frozen-model", QwenpawWorkspace: filepath.Join(private, "workspace"), Cwd: t.TempDir(), Timeout: 5 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				for range session.Messages {
				}
			}()
			select {
			case result := <-session.Result:
				if result.Status != "completed" || result.ExecutionStopped == nil || !*result.ExecutionStopped {
					t.Fatalf("bad Graph execution: %#v", result)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("adapter did not stop")
			}
		})
	}
}

func TestGraphACPRejectsMissingFrozenRemoteMCPCapability(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	backend, err := ResolveBackend("kimi", Config{ExecutablePath: self, ReplaceEnvironment: true, Env: map[string]string{"GRAPH_FAKE_PROTOCOL": "kimi", "GRAPH_FAKE_NO_MCP": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(context.Background(), "work", ExecOptions{Model: "frozen-model", Timeout: time.Second, McpConfig: json.RawMessage(`{"mcpServers":{"frozen":{"type":"http","url":"http://127.0.0.1:9999/mcp"}}}`)})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "failed" {
		t.Fatalf("Graph silently dropped frozen tool capability: %+v", result)
	}
}

func TestGraphRegisteredAdaptersAreAdmitted(t *testing.T) {
	for _, provider := range []string{"codex", "claude", "hermes", "kimi", "reasonix", "grok", "qwenpaw", "qwen", "pi", "omp", "openclaw", "antigravity", "codebuddy", "copilot", "opencode", "deveco", "cursor", "dsh", "kiro", "qoder", "qoderclicn", "traecli"} {
		if !GraphExecutionSupported(provider) {
			t.Errorf("verified native adapter %s is not admitted", provider)
		}
	}
}

func TestGraphRejectsUndeliverableFrozenSettingsBeforeLaunch(t *testing.T) {
	for _, tc := range []struct {
		provider string
		opts     ExecOptions
	}{
		{"pi", ExecOptions{Model: "frozen-model", CustomArgs: []string{"--no-mcp"}}},
		{"qwen", ExecOptions{Model: "frozen-model", ThinkingLevel: "high"}},
		{"kimi", ExecOptions{Model: "frozen-model", ServiceTier: "fast"}},
		{"pi", ExecOptions{Model: "frozen-model", CustomArgs: []string{"--session=ambient"}}},
	} {
		t.Run(tc.provider+tc.opts.ThinkingLevel+tc.opts.ServiceTier, func(t *testing.T) {
			backend, err := ResolveBackend(tc.provider, Config{ExecutablePath: "must-never-launch", ReplaceEnvironment: true})
			if err != nil {
				t.Fatal(err)
			}
			_, err = backend.Execute(context.Background(), "work", tc.opts)
			if err == nil || !strings.Contains(err.Error(), "Graph") {
				t.Fatalf("frozen setting was ignored before executable lookup: %v", err)
			}
		})
	}
}

func TestGraphOpenclawCatalogCarriesEffectiveModelSeparatelyFromAgentID(t *testing.T) {
	models, ok := parseOpenclawAgentsJSON([]byte(`[{"id":"worker","name":"Worker","model":"deepseek/deepseek-chat"}]`))
	if !ok {
		t.Fatal("invalid catalog")
	}
	raw, _ := json.Marshal(models)
	if !strings.Contains(string(raw), `"effective_model":"deepseek/deepseek-chat"`) {
		t.Fatalf("actual model lost in display label: %s", raw)
	}
}

func TestGraphAntigravityRequiresStructuredResultAndEffectiveModel(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"", "legacy", "wrong-model", "missing-result", "provider-failure"} {
		t.Run(mode, func(t *testing.T) {
			backend, err := ResolveBackend("antigravity", Config{ExecutablePath: self, ReplaceEnvironment: true, Env: map[string]string{"GRAPH_FAKE_PROTOCOL": "antigravity", "GRAPH_FAKE_AGY": mode, "HOME": t.TempDir()}})
			if err != nil {
				t.Fatal(err)
			}
			session, err := backend.Execute(context.Background(), "work", ExecOptions{Model: "frozen-model", Cwd: t.TempDir(), Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				for range session.Messages {
				}
			}()
			result := <-session.Result
			if (result.Status == "completed") != (mode == "") || result.ExecutionStopped == nil || !*result.ExecutionStopped {
				t.Fatalf("incorrect Graph result for %s: %+v", mode, result)
			}
		})
	}
}

func TestGraphManagedMCPIsDeliveredToCopilotAndDevEco(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"copilot", "deveco"} {
		t.Run(provider, func(t *testing.T) {
			backend, err := ResolveBackend(provider, Config{ExecutablePath: self, ReplaceEnvironment: true, Env: map[string]string{"GRAPH_FAKE_PROTOCOL": provider, "GRAPH_FAKE_EXPECT_MCP": "1", "HOME": t.TempDir()}})
			if err != nil {
				t.Fatal(err)
			}
			session, err := backend.Execute(context.Background(), "work", ExecOptions{Model: "frozen-model", Timeout: time.Second, McpConfig: json.RawMessage(`{"mcpServers":{"pinned":{"type":"http","url":"https://scoped.example/mcp","headers":{"Authorization":"Bearer task-token"}}}}`)})
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				for range session.Messages {
				}
			}()
			result := <-session.Result
			if result.Status != "completed" {
				t.Fatalf("native MCP missing: %+v", result)
			}
		})
	}
}

func TestGraphPiFamilyManagedMCPReadiness(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"pi", "omp"} {
		t.Run(provider, func(t *testing.T) {
			private := t.TempDir()
			folder := filepath.Join(private, "."+provider, "agent")
			if err := os.MkdirAll(folder, 0700); err != nil {
				t.Fatal(err)
			}
			config := json.RawMessage(`{"mcpServers":{"frozen":{"type":"http","url":"https://scoped.example/mcp","headers":{"Authorization":"Bearer task-token"}}}}`)
			if err := os.WriteFile(filepath.Join(folder, "mcp.json"), config, 0600); err != nil {
				t.Fatal(err)
			}
			for _, failed := range []bool{false, true} {
				if failed && provider == "omp" {
					continue
				}
				t.Run(fmt.Sprint(failed), func(t *testing.T) {
					env := map[string]string{"GRAPH_FAKE_PROTOCOL": provider, "GRAPH_FAKE_EXPECT_MCP": "1", "HOME": private, "OMP_MCP_REQUIRE_READY": "0", "OMP_MCP_TIMEOUT_MS": "0"}
					if failed {
						env["GRAPH_FAKE_MCP_FAIL"] = "1"
					}
					backend, err := ResolveBackend(provider, Config{ExecutablePath: self, ReplaceEnvironment: true, Env: env})
					if err != nil {
						t.Fatal(err)
					}
					session, err := backend.Execute(context.Background(), "prompt", ExecOptions{Model: "frozen-model", Timeout: 5 * time.Second, McpConfig: config})
					if failed {
						if err == nil {
							for range session.Messages {
							}
							<-session.Result
							t.Fatal("unready MCP accepted")
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					for range session.Messages {
					}
					result := <-session.Result
					if result.Status != "completed" || result.ExecutionStopped == nil || !*result.ExecutionStopped {
						t.Fatalf("result: %+v", result)
					}
				})
			}
		})
	}
}
