package agent

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// graphProcessScope owns the processes created by one immutable attempt. The
// protocol adapters keep their existing parsing and RPC behavior; Graph adds
// environment replacement and a physical shutdown boundary around that work.
type graphProcessScope struct {
	mu       sync.Mutex
	commands []*exec.Cmd
}

func (c Config) startExecution(cmd *exec.Cmd) error {
	if c.graphProcesses == nil {
		return cmd.Start()
	}
	configureProcessGroup(cmd)
	cmd.WaitDelay = 10 * time.Second
	cmd.Cancel = func() error { signalProcessGroup(cmd, syscall.SIGKILL); return nil }
	if err := startOwnedProcessTree(cmd, c.Logger); err != nil {
		return err
	}
	c.graphProcesses.mu.Lock()
	c.graphProcesses.commands = append(c.graphProcesses.commands, cmd)
	c.graphProcesses.mu.Unlock()
	return nil
}

func (s *graphProcessScope) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cmd := range s.commands {
		signalProcessGroup(cmd, syscall.SIGKILL)
	}
}

func (s *graphProcessScope) finish() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	confirmed := len(s.commands) > 0
	for _, cmd := range s.commands {
		if !waitProcessGroupGone(cmd, 50*time.Millisecond) {
			signalProcessGroup(cmd, syscall.SIGKILL)
		}
		if cmd.ProcessState == nil || !waitProcessGroupGone(cmd, 5*time.Second) {
			confirmed = false
		}
		releaseProcessGroup(cmd)
	}
	return confirmed
}

type graphProcessBackend struct {
	backend   Backend
	processes *graphProcessScope
	provider  string
}

func (b *graphProcessBackend) applyBuiltinRuntimeOverrides(desc BuiltinRuntime) {
	if target, ok := b.backend.(backendOverrideApplicator); ok {
		target.applyBuiltinRuntimeOverrides(desc)
	}
}
func (b *graphProcessBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	if opts.Model == "" || opts.ResumeSessionID != "" {
		return nil, fmt.Errorf("Graph requires explicit model and fresh session")
	}
	if err := ValidateGraphOptions(b.provider, opts); err != nil {
		return nil, err
	}
	runCtx, cancel := runContext(ctx, opts.Timeout)
	opts.DisableWholeRunRetry = true
	session, err := b.backend.Execute(runCtx, prompt, opts)
	if err != nil {
		cancel()
		b.processes.stop()
		b.processes.finish()
		return nil, err
	}
	messages := make(chan Message, 256)
	results := make(chan Result, 1)
	drained := make(chan struct{})
	go func() {
		defer close(messages)
		defer close(drained)
		for message := range session.Messages {
			select {
			case messages <- message:
			case <-runCtx.Done():
			}
		}
	}()
	go func() {
		defer cancel()
		defer close(results)
		result, ok := <-session.Result
		if !ok {
			result = Result{Status: "failed", Error: "runtime returned no terminal result"}
		}
		// Some ACP implementations emit Result just before deferred Wait. Cancel
		// the process context, then join the closed message channel before reading
		// ProcessState, so the receipt cannot race the adapter's reap operation.
		cancel()
		b.processes.stop()
		<-drained
		stopped := b.processes.finish()
		result.ExecutionStopped = &stopped
		results <- result
	}()
	return &Session{Messages: messages, Result: results}, nil
}
