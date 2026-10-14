package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

func hasGraphMCPServers(raw json.RawMessage) bool {
	var servers map[string]json.RawMessage
	if json.Unmarshal(raw, &servers) != nil {
		return false
	}
	if nested, ok := servers["mcpServers"]; ok {
		if json.Unmarshal(nested, &servers) != nil {
			return false
		}
	}
	return len(servers) > 0
}

type graphProbeOutput struct{ bytes.Buffer }

func (w *graphProbeOutput) Write(data []byte) (int, error) {
	n := len(data)
	remaining := (64 << 10) - w.Len()
	if remaining > len(data) {
		remaining = len(data)
	}
	if remaining > 0 {
		_, _ = w.Buffer.Write(data[:remaining])
	}
	return n, nil
}

// The native shell command validates all configured entries, without running a
// model or loading extensions. A help probe rejects old versions before an
// unrecognized subcommand could be interpreted as a prompt.
func (b *piBackend) validateGraphPiMCP(ctx context.Context, execName, lookedUp, cwd string) error {
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	run := func(args []string) (string, error) {
		cmd, _, _ := b.cfg.commandAt(execName).execVia(probeCtx, choosePiInvocation, lookedUp, args, b.cfg.Logger)
		cmd.Env = b.cfg.executionEnvironment()
		cmd.Dir = cwd
		hideAgentWindow(cmd)
		var output graphProbeOutput
		cmd.Stdout = &output
		cmd.Stderr = io.Discard
		if err := b.cfg.startExecution(cmd); err != nil {
			return "", err
		}
		err := cmd.Wait()
		return output.String(), err
	}
	help, err := run([]string{"--no-extensions", "--no-approve", "--help"})
	if err != nil || !strings.Contains(strings.ToLower(help), "mcp") {
		return fmt.Errorf("Graph Pi managed MCP requires a CLI with native mcp shell commands; upgrade this runtime")
	}
	if _, err := run([]string{"mcp", "list", "--no-approve"}); err != nil {
		return fmt.Errorf("Graph Pi required MCP servers are not ready; check the scoped plugin server and native MCP support")
	}
	return nil
}
