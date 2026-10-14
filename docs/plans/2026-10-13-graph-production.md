# Graph Production Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use `executing-plans` to implement this plan task-by-task.

**Goal:** Complete the Graph product gaps identified in the audit, excluding complex delivery (multi-repository handoff, checkpoint forks, automatic PR merge/deployment and external compensation).

**Architecture:** Keep the existing Agent, Runtime, Task queue, daemon, plugin and permission infrastructure. Extend versioned graph definitions and durable execution instances for dependency scheduling, branching, parallel joins and bounded nested execution. Shared Web/Desktop views must expose the same validated contract and preserve drafts through the platform storage adapter.

**Tech Stack:** Go, PostgreSQL, React, TypeScript, TanStack Query, Zustand, existing runtime adapters and plugin brokers.

## Delivery constitution

- Success: every acceptance item below has implementation, meaningful tests and review evidence; real provider acceptance is a separate required gate, never substituted with mocks.
- No deadline was supplied. Do not trade correctness or declared scope for a quick completion claim.
- Red lines: preserve ordinary tasks/chats/automation/runtime behavior and existing published v1 graphs; no discarded user edits; no foreign keys; concurrent indexes each get their own migration; no unverified provider enablement or client-only enforcement; no network/script side effects in the coordinator transaction; no fabricated successful checks.
- Weights: correctness and isolation 30; compatibility 20; product usability 20; maintainability 15; verifiability 15.
- Convergence: at most two architecture comparison rounds before choosing the documented contract. A failed verification leads to a concrete repair, never a weaker acceptance criterion.
- Workspace: reuse the current checkout and its unpublished Graph work on `codex/graph-production`. Unrelated `.codex/skills/openspec-*` and `openspec/` edits are outside scope. No full-tree staging.
- Ownership: workers write only explicitly assigned files. Integration, shared wire contracts, migrations and final delivery are supervised. Report changed files, exact tests/results, and remaining risks.

## Acceptance matrix

| ID | Required behavior | Status |
| --- | --- | --- |
| G01 | Conditional paths record their decisions; unselected paths are skipped and cannot accidentally execute | implemented; local regression passed; release acceptance pending |
| G02 | Ready branches execute concurrently; joins honor dependency outcomes; failures/cancellation stop and account for active work | implemented; local regression passed; release acceptance pending |
| G03 | Bounded loops have separate iteration instances and outputs, exit conditions and enforced run budgets; retry remains an attempt within an iteration | implemented; local regression passed; release acceptance pending |
| G04 | Canvas supports adding/connecting/moving nodes, zoom/pan, keyboard operation and undo/redo; saves topology and layout | implemented; browser/review refinements in progress |
| G05 | Visual input/output field mapping, schema-driven run input forms and understandable validation errors | implemented; browser/review refinements in progress |
| G06 | AI uses current graph and conversation to propose changes; diff/accept/reject and undo preserve user edits; all supported topology is generatable | implemented; browser/review refinements in progress |
| G07 | Intermediate/multiple approval gates and multi-person approval policies; readable immutable candidate/evidence review | implemented; local regression passed; release acceptance pending |
| G08 | HTTP, script, delay and subflow nodes execute through bounded, authorized infrastructure and recover correctly | implemented; local regression passed; release acceptance pending |
| G09 | Durable schedules, authenticated/deduplicated webhooks and supported task-event triggers; permissions and revoked access are rechecked | implemented; local regression passed; release acceptance pending |
| G10 | Workspace/user scoped draft persistence, refresh recovery, conflict detection and explicit discard | implemented; browser/review refinements in progress |
| G11 | Existing runtime/provider/profile/config capabilities are integrated through the shared runtime system; frozen config, cancellation and submissions remain enforced | verified provider group integrated; remaining native adapters in progress |
| G12 | Plugin Remote MCP reuses the existing task manifest, authorization and credential broker lifecycle | implemented; local regression passed; release acceptance pending |
| G13 | Search/select task, repository and commit; version and runtime eligibility visible before start | implemented; local regression passed; release acceptance pending |
| G14 | Readable run topology, logs, inputs/outputs/artifacts, approval evidence and linked rework; no raw IDs/JSON required for normal operation | implemented; browser/review refinements in progress |
| G15 | Search/filter/pagination and graph/run duration, usage and failure/blocked statistics; realtime events wired end to end | implemented; local regression passed; release acceptance pending |
| G16 | Dedicated mobile Graph access consistent with permissions and server semantics | native interaction/parity plan agreed in authorized scope; implementation next |
| G17 | Regression, migration/concurrency/recovery tests, Web/Desktop/mobile checks and visual/browser acceptance | local backend suites passed; frontend/mobile/CI review in progress |
| G18 | Real model authoring, real runtime execution, test-repository artifact push and approval acceptance; staged release/rollback evidence and runtime/CLI upgrade compatibility | pending |

## Work packages

1. **Versioned contract and planner** — `server/internal/graph/definition.go`, new topology/expression/planning files and tests. First add failing branch/join/loop and compatibility cases, implement strict validation and deterministic readiness/expansion, then run the graph package tests. Freeze the wire contract before editor/engine integration.
2. **Runtime and plugin integration** — `server/internal/daemon/graph*.go`, `server/internal/daemon/execenv/`, `server/internal/handler/graph_snapshot.go`, `graph_claim.go`, `server/internal/service/graph_plugin.go`, existing provider/plugin tests. Reproduce rejected compatible runtimes/configs and missing plugin manifests, extend shared adapters, verify isolation, credentials, lifecycle and ordinary-task regression.
3. **Durable engine** — `server/internal/service/graph*.go`, new migrations following the repository's latest number, Graph API handlers. Test readiness, selected edges, joins, iterations, nested scopes, budgets, approvals, restart/cancel races and v1 compatibility against isolated PostgreSQL before integrating new scheduling.
4. **Special nodes and triggers** — new Graph node/trigger service files, existing daemon execution adapters, scheduler/event hooks and API. Use durable intents/outbox and idempotency; execute effects outside database transactions. Test SSRF/permissions/timeouts, webhook authentication/replay, cron/event deduplication and restart recovery.
5. **Shared product UI and AI** — `packages/core/graphs/`, `packages/views/graphs/`, Graph locales, API client and `graph_generation.go`. Compare existing creation, automation, runtime, task/attachment and approval components before building. Implement canvas, mappings, persisted drafts, conversational proposals, run launch/inspection/rework, filtering/statistics and realtime. Test behaviors through actual shared components and browser interactions.
6. **Mobile integration** — follow `apps/mobile/CLAUDE.md`; implement Graph discovery, authoring/control and accessible review using mobile patterns and the same server semantics. Verify offline/error and response compatibility.
7. **Acceptance and delivery** — regression and migration suites; independent specification and quality reviews; desktop/narrow/mobile/browser verification; real provider/test repository acceptance; full CI and documented release with runtime/CLI compatibility. Do not mark pending real acceptance as complete.

## Verification commands and evidence

- Go uses `server/tmp/graph-prepush/toolchains/golang.org/toolchain@v0.0.1-go1.26.9.windows-amd64/bin/go.exe`; use ignored D-drive caches and isolated PostgreSQL.
- Core: `node node_modules/vitest/vitest.mjs run --config packages/core/vitest.config.ts <target files>`.
- Views: run the installed Vitest entry from `packages/views`, then root TypeScript with the affected package/application configs.
- Standard build/test scripts and Linux CI remain authoritative. Existing Windows-only baseline failures must be distinguished by reproduction, not ignored broadly.
- Each functional slice records the initial failing behavior, passing commands, review findings and fixes in the progress log below.

## Progress log

- 2026-10-13: scope locked from the user's explicit request; unpublished AI creation work retained. Test workspace/runtime/repository details requested for real acceptance. Implementation not yet claimed complete.
- User authorized real acceptance in `vega-z85q` (织女星, `06c89e9a-f311-4324-b5d9-9acd3114228b`), using local Claude Code with DeepSeek and the production repository's main baseline. Acceptance uses a frozen main SHA, dedicated tasks and isolated Graph artifact refs; it does not merge test artifacts into main. Read-only preflight confirmed no agents/runtimes in that workspace, the existing CLI is authenticated to the requested server, and the default local daemon is stopped.
- Engine and pure-contract PostgreSQL regressions cover branch skips, parallel joins, bounded nested loop instances, script queue/submission/stop receipts, multiple approval gates/quorum/replacement, durable delay, HTTP ambiguity/leases/late callbacks, state budgets and dependency-ancestor commit selection. Cross-review repaired trigger workspace lock ordering, concurrent dispatch, disable/revocation before root creation, and committed-run recovery after coordinator crashes.
- Root review reproduced then fixed task-event capture crossing workspace deletion (`55P03` NOWAIT fence avoids reverse-lock deadlocks), large run-list execution snapshots, and HTTP JSON-encoding expansion beyond output limits. The new regression cases are green against PostgreSQL and the bounded HTTP executor. Graph trigger tables now participate in workspace deletion.
- Existing GitHub App and workspace GitLab/Forgejo/Gitea connections support ref/commit browsing, pagination and project/workspace repository scope. Tests verify matching credential destinations, redirect rejection, invalid SHAs and unauthorized repository/member reads. API advertises `commits_supported`; statistics explicitly count unpriced tasks.
- Full local handler suite passed with PostgreSQL: 23.220s. Full service suite with Graph PostgreSQL fixtures passed: 15.636s. Pure graph, bundled-plugin signatures, bounded HTTP, VCS and GitHub snapshot suites also passed. Graph CLI regression passed: 0.146s. These results precede the last optional display-label addition; affected checks will rerun. `-race` is not claimed locally because CGO is unavailable; Linux CI remains required.
- Full regression exposed existing Windows issues: signed embedded release payloads changed under Git CRLF conversion, builtin frontmatter assumed LF, portable archive validation accepted Unix absolute paths on Windows, and equal-clock local skill imports lost FIFO order. Fixes preserve exact signed bytes, normalize embedded text checkout, reject both absolute-path conventions, and tie-break imports by enqueue sequence. No signature bypass or test skip was added.
- Actual shared Web components were exercised in a local browser using fixture APIs: AI Apply/Discard, real ReactFlow dragging/connection/undo, nested-loop script editing, refresh draft recovery, launch/approval and one-time webhook secret display. Desktop and 375px checks found no horizontal overflow or page errors. This is UI acceptance only, not real model/runtime acceptance. Screenshots are in ignored `server/tmp/graph-ui-preview/accept-*.png`.
- Native mobile plan follows existing More-menu, native Stack/formSheet and field/list patterns. State, permissions, candidate/revision checks, multi-gate approvals, counts and navigation identity must agree with Web. Native authoring, monitoring and control remain required; no simulator/device verification has been claimed.
- Standard migration runner passed a fresh 1–371 installation and no-op rerun. A separate PostgreSQL database was migrated to 357, seeded with V1 run/approval records, upgraded to 371, checked for unchanged legacy records and valid multi-gate indexes, rolled back only through the new 358–371 files in quiescence, then upgraded again successfully. New registry tests cover invalid concurrent Graph index cleanup, including rollback 361.
- Independent runtime review identified overbroad ambient provider credential inheritance and anonymous Remote MCP revocation checks; runtime worker is adding focused regression tests and corrections. Compose now exposes both existing Graph rollout controls. Full Windows Go execution remains non-green due to documented Unix shell, permissions and HOME assumptions; Linux CI remains required.
- Real native adapter smoke passed on local Claude Code 2.1.278 with DeepSeek `deepseek-v4-flash`, using `execenv.Prepare`, private Graph home and `agent.New` with environment replacement. It returned the exact requested text without tools, `completed`, physical stop confirmed, usage 1,437 input / 9 output tokens. The generated private task directory was removed after retaining non-secret outcome evidence. This verifies actual provider protocol/auth integration only; Vega production E2E and deployment remain pending.
- Final authoring review reproduced AI rejecting disconnected or actor-less local drafts; a bounded draft traversal now checks nested actors without requiring a publishable topology. The model proposal still undergoes complete validation. Focused regression passed. Production Web webpack build passed using the specified proxy and system CA, and `govulncheck ./...` found no reachable vulnerabilities. Backend CI explicitly selects Node 22 for native SQLite credential projection fixtures.
