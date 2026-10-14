package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Graph requires the documented headless stream contract. Older agy print
// mode may exit zero without a result or silently ignore an unknown model;
// that behavior cannot serve as an immutable execution receipt.
// https://www.antigravity.google/docs/cli/headless/
func (b *antigravityBackend) executeGraph(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	runCtx, cancel := runContext(ctx, opts.Timeout)
	executable := b.cfg.ExecutablePath
	if executable == "" {
		executable = "agy"
	}
	args := []string{"-p", prompt, "--output-format", "stream-json", "--dangerously-skip-permissions", "--model", opts.Model}
	args = append(args, opts.CustomArgs...)
	args = append(args, opts.ExtraArgs...)
	cmd := b.cfg.commandAt(executable).exec(runCtx, args...)
	cmd.Dir = opts.Cwd
	cmd.Env = b.cfg.executionEnvironment()
	hideAgentWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr := newStderrTail(newLogWriter(b.cfg.Logger, "[agy:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderr
	if err = b.cfg.startExecution(cmd); err != nil {
		cancel()
		return nil, fmt.Errorf("start Graph agy: %w", err)
	}
	messages := make(chan Message, 256)
	results := make(chan Result, 1)
	go func() { <-runCtx.Done(); _ = stdout.Close() }()
	go func() {
		defer cancel()
		defer close(messages)
		defer close(results)
		started := time.Now()
		result := Result{Status: "failed", Error: "Antigravity CLI did not return a structured terminal result; install a version supporting --output-format stream-json"}
		initialized, terminal := false, false
		scanner := newAgentStreamScanner(stdout)
		for scanner.Scan() {
			var event struct {
				Event string `json:"event"`
				Init  struct {
					Model string `json:"model"`
				} `json:"init"`
				Step struct {
					Text string `json:"text_delta"`
				} `json:"step_update"`
				Result struct {
					SessionID string `json:"conversation_id"`
					Status    string `json:"status"`
					Response  string `json:"response"`
					Error     string `json:"error"`
				} `json:"result"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
				result.Status = "failed"
				result.Error = "Antigravity returned an invalid headless event"
				cancel()
				break
			}
			switch event.Event {
			case "init":
				if initialized || event.Init.Model != opts.Model {
					result.Status = "failed"
					result.Error = "Antigravity did not acknowledge the frozen model"
					cancel()
					break
				}
				initialized = true
			case "step_update":
				if event.Step.Text != "" {
					trySend(messages, Message{Type: MessageText, Content: event.Step.Text})
				}
			case "result":
				if !initialized || terminal {
					result.Status = "failed"
					result.Error = "Antigravity returned an invalid terminal sequence"
					cancel()
					break
				}
				terminal = true
				result.Output = event.Result.Response
				result.SessionID = event.Result.SessionID
				if event.Result.Status == "SUCCESS" {
					result.Status = "completed"
					result.Error = ""
				} else {
					result.Status = "failed"
					result.Error = "Antigravity terminal status: " + event.Result.Status
					if event.Result.Error != "" {
						result.Error += " (" + event.Result.Error + ")"
					}
				}
			}
			if runCtx.Err() != nil {
				break
			}
		}
		scanErr := scanner.Err()
		waitErr := cmd.Wait()
		// Retain a protocol failure even when our cancellation stopped the process.
		if ctx.Err() != nil {
			result.Status = "aborted"
			result.Error = "execution cancelled"
		} else if runCtx.Err() == context.DeadlineExceeded {
			result.Status = "timeout"
			result.Error = "Antigravity Graph execution timed out"
		} else if scanErr != nil && result.Status == "completed" {
			result.Status = "failed"
			result.Error = "Antigravity stream failed: " + scanErr.Error()
		} else if waitErr != nil && result.Status == "completed" {
			result.Status = "failed"
			result.Error = "Antigravity process failed: " + waitErr.Error()
		}
		if result.Error != "" {
			result.Error = withAgentStderr(result.Error, "agy", strings.TrimSpace(stderr.Tail()))
		}
		result.DurationMs = time.Since(started).Milliseconds()
		results <- result
	}()
	return &Session{Messages: messages, Result: results}, nil
}
