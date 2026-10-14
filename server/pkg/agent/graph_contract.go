package agent

import (
	"fmt"
	"strings"
)

// GraphExecutionSupported reports adapters with verified fresh-context and
// physical process-tree completion semantics. It is shared by admission and
// the backend factory so provider names alone cannot enable an unsafe launch.
func GraphExecutionSupported(provider string) bool {
	if descriptor, ok := BuiltinRuntimeByID(provider); ok {
		provider = descriptor.ProtocolFamily
	}
	switch provider {
	case "codex", "claude", "hermes", "kimi", "reasonix", "grok", "qwenpaw", "qwen", "pi", "openclaw", "antigravity", "codebuddy", "copilot", "opencode", "deveco", "cursor", "dsh", "kiro", "qoder", "qoderclicn", "traecli":
		return true
	default:
		return false
	}
}

// ValidateGraphLaunch protects immutable model, context and session selection.
// Wrapper subcommands and ordinary diagnostic flags remain usable. Unlike the
// normal task path, a conflicting option is rejected rather than silently lost.
func ValidateGraphLaunch(provider string, args, prefix []string) error {
	if !GraphExecutionSupported(provider) {
		return fmt.Errorf("runtime adapter %q does not provide Graph isolation and stop receipts", provider)
	}
	return validateGraphArguments(provider, args, prefix)
}

func validateGraphArguments(provider string, args, prefix []string) error {
	blocked := map[string]bool{"--model": true, "-m": true, "--resume": true, "--continue": true, "-r": true, "--session-id": true, "--fork-session": true, "--settings": true, "--setting-sources": true, "--mcp-config": true, "--append-system-prompt": true, "--system-prompt": true, "--plugin-dir": true, "--add-dir": true, "--agents": true, "--agent": true, "--effort": true, "--cwd": true, "--cd": true, "-C": true, "--profile": true, "-p": true}
	for _, key := range []string{"--session", "--conversation", "--config", "--config-file", "--config-dir", "--agent-file", "--skills-dir", "--extension", "-e", "--thinking", "--variant", "--provider", "--fallback-model", "--system-prompt-file", "--append-system-prompt-file", "--workspace", "--directory", "--dir", "--output-format", "--input-format", "--remote-control", "--print-timeout", "--log-file", "--additional-mcp-config", "--connect", "--no-mcp", "--no-extensions", "--no-skills", "--no-rules", "--no-session"} {
		blocked[key] = true
	}
	if provider == "claude" {
		blocked["-c"] = true
	}
	if provider == "codex" {
		for _, key := range []string{"-c", "--config", "--enable", "--disable"} {
			blocked[key] = true
		}
	}
	for _, region := range [][]string{args, prefix} {
		for _, arg := range region {
			arg = strings.Trim(strings.TrimSpace(arg), "\"'")
			key, _, _ := strings.Cut(arg, "=")
			if fields := strings.Fields(key); len(fields) > 0 {
				key = fields[0]
			}
			if len(key) > 2 && key[0] == '-' && key[1] != '-' && blocked[key[:2]] {
				key = key[:2]
			}
			if blocked[key] {
				return fmt.Errorf("Graph launch argument %s conflicts with frozen execution settings", key)
			}
		}
	}
	return nil
}

func (c Config) executionEnvironment() []string {
	if c.ReplaceEnvironment {
		return mergeEnv(nil, c.Env)
	}
	return buildEnv(c.Env)
}

// ValidateGraphOptions is also used before publication: a frozen setting must
// be deliverable by the selected adapter instead of being silently discarded.
func ValidateGraphOptions(provider string, opts ExecOptions) error {
	if descriptor, ok := BuiltinRuntimeByID(provider); ok {
		provider = descriptor.ProtocolFamily
	}
	if opts.ThinkingLevel != "" && !ThinkingControlSupported(provider) {
		return fmt.Errorf("Graph runtime %s cannot apply thinking_level", provider)
	}
	if opts.ServiceTier != "" && provider != "codex" {
		return fmt.Errorf("Graph runtime %s cannot apply service_tier", provider)
	}
	return validateGraphArguments(provider, opts.CustomArgs, opts.ExtraArgs)
}

// GraphRemoteMCPSupported covers adapters that deliver the scoped HTTP broker.
// ACP adapters additionally verify the actual handshake before session/new;
// a version lacking HTTP support fails rather than dropping required tools.
func GraphRemoteMCPSupported(provider string) bool {
	if !GraphExecutionSupported(provider) {
		return false
	}
	switch provider {
	case "codex", "claude", "hermes", "kimi", "reasonix", "grok", "qwenpaw", "qwen", "openclaw", "antigravity", "codebuddy", "copilot", "opencode", "deveco", "cursor", "dsh", "kiro", "qoder", "qoderclicn", "traecli", "pi", "omp":
		return true
	}
	return false
}
