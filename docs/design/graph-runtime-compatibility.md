# Graph runtime compatibility and verification

Updated: 2026-10-13. This document describes the implemented admission and execution contract and the evidence collected for it. Support in the adapter matrix is not a claim that every third-party CLI has passed a live model deployment test.

## Existing runtime integration

Graph reuses existing workspace Runtime, Agent, runtime profiles, the shared `agent.ResolveBackend` factory, task queue, task credentials, plugin manifests, and credential broker. It does not maintain a second Codex-only runtime registry. A runtime profile remains the selected command plus fixed launch prefix; arbitrary wrappers must preserve the selected provider protocol.

Publication freezes the effective provider/model, agent instructions, bound skills, runtime profile command and arguments, task settings, and plugin contribution entries. OpenClaw catalogs retain both the native agent ID and its actual effective model; Graph refuses an unresolved model instead of freezing the agent ID as if it were an LLM. Claim checks the runtime identity/profile and non-secret custom environment values against the snapshot. Credentials remain live references and can rotate without publishing their values into Graph JSON. Host-native credential and endpoint files are selectively projected at attempt preparation; these local files are not serialized as a server-side publication snapshot. Change them deliberately and refresh/re-publish when changing effective model routing.

Daemon capability `graph_execution_v2` is required for non-Codex providers, custom runtime profiles, and scripts. Legacy `graph_execution_v1` remains compatible only with the original built-in Codex path. Declared support is checked by shared adapter admission and then by the provider's native launch/handshake. Unsupported thinking controls, non-Codex service tiers, resume flags, model/config overrides, and unscoped MCP overrides fail explicitly. Ordinary non-Graph task dispatch retains its prior behavior.

## Isolation and stopping

Each attempt has a private HOME, provider configuration and session state, a frozen context, and a process-tree owner. Windows uses an owned Job Object; Unix uses the existing process-group implementation. Completion waits for process exit and descendants before issuing an execution-stopped receipt. Cancellation and timeout use the same boundary. Script nodes run Node.js or Python directly, without invoking a model; their outputs are bounded and schema checked. Ambient account variables are limited to the selected provider's documented channels. Scripts inherit platform/proxy variables and explicit frozen bindings only.

This is configuration and session isolation, not an operating-system security sandbox. Agent tools and scripts still execute with the runtime account's host permissions. Code should run on an appropriately isolated runtime host. A local CLI PID cannot prove that a remote gateway's worker stopped. Therefore Graph's OpenClaw gateway mode is rejected with instructions to register the existing Multica daemon on that host and select its OpenClaw runtime in local mode. Ordinary OpenClaw gateway tasks remain supported.

Nested/loop attempts use unique node-run IDs in artifact refs. Existing V1 manifests without an instance key retain their original artifact-ref layout. Graph never merges these refs into the source branch automatically.

## Provider matrix

All rows reuse the registered provider's existing execution protocol and have deterministic adapter/protocol tests, environment projection tests, and process cancellation tests. “Native smoke” means the installed vendor executable made an actual model request; it is listed separately from protocol fixtures.

| Runtime | Graph configuration/auth source | MCP delivery and required native behavior | Live validation |
|---|---|---|---|
| Codex | Private CODEX_HOME; frozen effective model and runtime settings; existing native auth and selected provider env keys | Existing native Codex Graph isolation and scoped MCP config | Existing adapter regression suite; no new paid native smoke in this work |
| Claude Code | Private `.claude`; selective auth/transport settings; explicit frozen model; bare mode, strict MCP, no session persistence | Native strict MCP config; requires flags documented by current CLI | **Claude Code 2.1.278 + DeepSeek `deepseek-v4-flash` passed native Graph adapter smoke**: completed and physical-stop receipt; full Vega workflow tracked separately |
| CodeBuddy | Private config directory; account/transport-only settings.env; explicit custom bindings win | Existing native MCP argument/config | Protocol/config fixtures |
| GitHub Copilot CLI | Private home/cache/providers registry; GitHub environment token or BYOK registry | `--disable-builtin-mcps` and a private `--additional-mcp-config` file | Protocol/config fixtures; opaque browser OAuth config is not copied |
| Cursor | Private config directory and native HTTP1 transport setting; `CURSOR_API_KEY` required | Existing native private project MCP file | Protocol/config fixtures; browser token store is not imported |
| OpenCode / DevEco | Private selected provider config + native API/OAuth auth store; JSONC/inline/file/env references projected; incompatible managed config rejected | Native remote MCP configuration via isolated inline config | Protocol/config fixtures; custom provider credential variable names must be explicitly bound where native projection does not resolve them |
| Hermes | Private selected profile, provider transport and credential stores; no shared memory | ACP handshake must acknowledge required HTTP MCP and frozen settings | Protocol/config fixtures |
| Kimi | Private native model/provider/service TOML and credentials | ACP handshake, explicit model and thinking acknowledgement | Protocol/config fixtures |
| Kiro | Private home; official API key required | ACP handshake must accept required MCP | Protocol/config fixtures; opaque browser/SQLite token store is not copied |
| Qoder / Qoder CN | Private `.qoder` / `.qoder-cn`; PAT and native VPC setting | ACP handshake; frozen model | Protocol/config fixtures; modelConfigs is not treated as transport/auth |
| Qwen Code | Private modelProviders/auth settings and OAuth credential file | Existing native MCP config | Protocol/config fixtures |
| QwenPaw | Private root registry, selected private agent.json/workspace, credential-only provider stores; fallback/routing disabled | ACP with private registry and required MCP acknowledgement | Protocol/config fixtures |
| Grok | Private native auth/transport config | ACP with frozen model and HTTP MCP acknowledgement | Protocol/config fixtures |
| Reasonix | Private providers/credential config and state | ACP with required model/thinking/MCP acknowledgement | Protocol/config fixtures |
| Pi | Private native auth/models, no discovered extensions/skills/context/templates or project approval | Only native built-in MCP is explicitly loaded; frozen servers use direct exposure; native `mcp list` validates readiness before the model starts | Protocol/config fixtures; CLI must expose native MCP commands |
| OMP | Private modern YAML model/config plus active credential-only projection into a new native SQLite store; explicit profile sources supported | Native mcp.json; project MCP discovery disabled; forced positive MCP timeout and required-ready barrier; explicit private config | Protocol/config/real Node SQLite fixtures; installed OMP model invocation not performed |
| DeepSeek Harness | Private credential YAML and agent/session roots | Existing native stdio protocol with scoped MCP | Protocol/config fixtures |
| OpenClaw local | Private agent registry/config/workspace; frozen native agent ID + effective LLM; only auth records imported, never shared sessions | Native private MCP; local execution only | Protocol/config/real SQLite projection fixtures; local installed 2026.2.26 is below required 2026.5.5, so no native smoke |
| Antigravity CLI | Private model-provider settings; native OS account or Gemini API account | Native private MCP schema (`serverUrl`); structured stream must acknowledge the frozen model and terminal status | Protocol/config fixtures; `agy` is not installed on the test host |

Minimum versions already used by ordinary runtime discovery do **not** imply that a CLI has the newer Graph-specific features. For example, the existing Claude discovery minimum predates `--bare`. OpenClaw has an explicit Graph minimum of 2026.5.5. ACP adapters negotiate actual MCP capabilities and reject unsupported settings; Antigravity requires init/model and structured terminal events; Pi checks for native MCP commands before executing its no-model validation. Other native flags fail at launch if unsupported. OMP credential-store projection, when using native SQLite auth rather than a configured auth broker, requires Node.js 22 with `node:sqlite` and a recognized native schema (3–8); command-generated credentials and automatic model-routing settings are rejected instead of imported. Exact oldest working versions for every external CLI have not been established by this work. Deploy supported current vendor CLIs and run the targeted native smoke before adding an adapter to an installation's production acceptance matrix.

## Remote MCP revocation and cleanup

Graph publishes exact skill and Remote MCP contribution entries from the existing plugin system, including release/config/schema pins, tools, failure policy and auth requirements. Migration 358 composes a Graph task's plugin manifest from its engine-owned immutable attempt, not arbitrary task context. Claim verifies its entries match the frozen Graph snapshot. The broker resolves credentials through the existing task authorization endpoint and rechecks live Graph install/release/grant/binding state on every request, including `auth=none`. Cancellation, attempt replacement, release revocation, or grant withdrawal deny subsequent calls. Tokens are not placed in Graph definitions or browser-visible snapshots. Broker lifetimes and provider files follow task cleanup.

Evidence includes PostgreSQL migration/ordinary-task preservation tests; Graph manifest identity/entry checks; live revocation/attempt fencing tests; and an HTTP proxy test proving that revoked anonymous requests never reach upstream.

## Verification boundaries

Portable Go helper executables exercise each adapter's actual stream/ACP interface and descendant cancellation, without depending on vendor accounts or POSIX-only test scripts. They prove Multica's contract handling; they do not prove a vendor's latest binary implements undocumented behavior. Native configuration tests use actual parsers and, where needed, Node's SQLite API, with source credential stores left unchanged. The Claude native smoke used 1,437 input / 9 output tokens and returned `GRAPH_NATIVE_SMOKE_OK`; no production issue or source repository was changed by that smoke.

The end-to-end acceptance still needs the registered Vega runtime, published Graph, real task completion/approval and isolated artifact ref. A successful unit suite or native one-turn smoke must not be reported as that full workflow. Linux CI is the full repository regression/race gate; Windows exposes pre-existing POSIX-shell fixture limitations that must be classified separately from Graph regressions.

## Native references

- [Claude CLI flags](https://code.claude.com/docs/en/cli-reference)
- [Copilot CLI flags](https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-command-reference) and [configuration directory](https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-config-dir-reference)
- [Cursor configuration](https://cursor.com/docs/cli/reference/configuration)
- [CodeBuddy settings](https://www.codebuddy.ai/docs/cli/settings)
- [OpenCode managed settings](https://opencode.ai/docs/config/#managed-settings)
- [Qoder authentication](https://docs.qoder.com/cli/authentication), [custom models](https://docs.qoder.com/cli/custom-models), [Kiro authentication](https://kiro.dev/docs/getting-started/authentication/)
- [Pi CLI](https://raw.githubusercontent.com/badlogic/pi-mono/main/packages/coding-agent/docs/cli.md) and [MCP](https://raw.githubusercontent.com/badlogic/pi-mono/main/packages/coding-agent/docs/mcp.md)
- [OMP CLI](https://omp.sh/docs/cli), [MCP readiness](https://raw.githubusercontent.com/can1357/oh-my-pi/main/docs/mcp-config.md), [credential-store schema](https://raw.githubusercontent.com/can1357/oh-my-pi/main/packages/ai/src/auth/sqlite-credential-store.ts)
- [Antigravity headless protocol](https://www.antigravity.google/docs/cli/headless/) and [MCP schema](https://www.antigravity.google/docs/mcp)
- [DeepSeek Harness local credentials](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/credentials/credentials-local/README.md)
