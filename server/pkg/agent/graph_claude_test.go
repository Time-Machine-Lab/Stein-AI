package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"testing"
	"time"
)

func runGraphClaudeHelper(mode string) {
	if mode == "graph_descendant" {
		time.Sleep(time.Minute)
		return
	}
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		os.Exit(3)
	}
	if mode == "graph_timeout" {
		self, _ := os.Executable()
		child := exec.Command(self)
		child.Env = []string{"CLAUDE_FAKE_MODE=graph_descendant"}
		hideAgentWindow(child)
		if err := child.Start(); err != nil {
			os.Exit(4)
		}
		fmt.Println(`{"type":"system","session_id":"graph-timeout"}`)
		time.Sleep(time.Minute)
		return
	}
	if os.Getenv("GRAPH_TEST_AMBIENT") != "" {
		fmt.Fprintln(os.Stderr, "inherited ambient secret")
		os.Exit(5)
	}
	for _, flag := range []string{"--bare", "--strict-mcp-config", "--setting-sources", "--no-session-persistence"} {
		if !slices.Contains(os.Args, flag) {
			os.Exit(6)
		}
	}
	result, _ := json.Marshal(map[string]any{"type": "result", "subtype": "success", "is_error": false, "session_id": "graph-fake", "result": "isolated"})
	fmt.Println(string(result))
}

func TestGraphClaudeDoesNotInheritAmbientEnvironment(t *testing.T) {
	t.Setenv("GRAPH_TEST_AMBIENT", "must-not-leak")
	self, _ := os.Executable()
	backend, err := New("claude", Config{ExecutablePath: self, ReplaceEnvironment: true, LaunchPrefix: []string{"start", "deepseek"}, Env: map[string]string{"CLAUDE_FAKE_MODE": "graph_contract", "IS_SANDBOX": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(context.Background(), "frozen", ExecOptions{Model: "deepseek", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "completed" || result.ExecutionStopped == nil || !*result.ExecutionStopped {
		t.Fatalf("isolation failed: %#v", result)
	}
}

func TestGraphClaudeCancellationHasPhysicalTreeReceipt(t *testing.T) {
	self, _ := os.Executable()
	backend, err := New("claude", Config{ExecutablePath: self, ReplaceEnvironment: true, Env: map[string]string{"CLAUDE_FAKE_MODE": "graph_timeout", "IS_SANDBOX": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := backend.Execute(ctx, "frozen", ExecOptions{Model: "deepseek", Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for message := range session.Messages {
			if message.Type == MessageStatus {
				cancel()
			}
		}
	}()
	select {
	case result := <-session.Result:
		if result.Status == "completed" || result.ExecutionStopped == nil || !*result.ExecutionStopped {
			t.Fatalf("unconfirmed cancel: %#v", result)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("cancellation did not return")
	}
}

func TestGraphClaudeReportsStoppedWithIsolatedEnvironment(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	backend, err := New("claude", Config{ExecutablePath: self, ReplaceEnvironment: true, Env: map[string]string{"CLAUDE_FAKE_MODE": "control_request", "IS_SANDBOX": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	session, err := backend.Execute(context.Background(), "only the frozen task", ExecOptions{Model: "deepseek-test", Timeout: 10 * time.Second, DisableWholeRunRetry: true})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("provider failed: %#v", result)
	}
	if result.ExecutionStopped == nil || !*result.ExecutionStopped {
		t.Fatalf("missing process tree evidence: %#v", result)
	}
}

func TestGraphClaudeCannotOverrideFrozenLaunchContract(t *testing.T) {
	for _, args := range [][]string{{"--model", "another-model"}, {"--settings", "outside.json"}, {"--resume", "old-session"}, {"--mcp-config", "outside.json"}, {"--append-system-prompt", "mutable"}} {
		if err := ValidateGraphLaunch("claude", args, nil); err == nil {
			t.Fatalf("accepted semantic override: %v", args)
		}
	}
	if err := ValidateGraphLaunch("claude", []string{"--verbose"}, []string{"start", "deepseek"}); err != nil {
		t.Fatal(err)
	}
}
