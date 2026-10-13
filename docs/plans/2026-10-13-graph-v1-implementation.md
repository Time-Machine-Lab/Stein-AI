# Graph V1 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use `executing-plans` to implement this plan task-by-task. This implementation is authorized in the current session and uses subagent-driven development for independent ownership areas.

**Goal:** Implement the Graph V1 contract in `docs/design/graph-module-v1.md`, with published serial workflows, durable node execution, scoped task control, artifacts, human approval, CLI and shared Web/Desktop views.

**Architecture:** Keep graph definition validation and transition decisions in `server/internal/graph`. Persist runs and operations in PostgreSQL, reuse TaskService/daemon execution, and gate all graph execution with a shared ownership policy. Existing normal task behavior stays covered by regression checks.

**Tech Stack:** Go, pgx/sqlc, PostgreSQL, Cobra, TypeScript, TanStack Query, React, existing shared UI packages.

## Workspace baseline

- Repository: `D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI`, initial commit `5b47a78f6`.
- No CodeGraph index exists; use targeted source searches.
- Preserve existing edits to `.codex/skills/openspec-*` and untracked `openspec/config.yaml`.
- The design document is the agreed scope. Do not claim a milestone or V1 complete while its required integration remains absent.
- No real authenticated agent CLI invocation, publication, deployment or external Git push is part of implementation verification.

## Shared contracts

- Public JSON uses snake_case; frontend internal fields use camelCase.
- Definition includes `schema_version`, `name`, `nodes`, `edges`, `input_schema`. Node keys are stable; kinds are start, agent, approval, end. Exactly one approval is immediately before end.
- Agent nodes declare agent ID, instructions, input bindings, output schema, execution mode (none/produce_commit/verify_commit), max attempts (1..3), timeout. Approval declares a member ID.
- Core schema and domain types are owned by the core implementation task; communicate exact exported signatures before downstream wiring.
- NodeRun is a business activation; each attempt has its own Task and immutable manifest. Result acceptance never depends solely on Issue status or Task completed.
- Graph tasks fail closed when daemon/CLI capability, input identity, ownership or authorization is missing. Normal tasks retain their current lifecycle.
- Task identity is derived from trusted mat_ middleware. Agent tokens cannot invoke human run-control or graph publication APIs.

## Task 1 Core definition and state contracts

Create `server/internal/graph/{definition,state,result}.go` and colocated tests. First write failing validation/transition tests for serial topology, attempt budgets, result outcomes, pause/cancel, stale identity and approval digest. Implement bounded schema validation, canonical hashing and pure transition predicates. Run `go test ./internal/graph` from server.

## Task 2 Persistence and transactional operations

Create migrations after 331, each concurrent index in its own up/down pair. Add Graph query source and generated query code using sqlc; extend Task and Issue ownership storage. First cover uniqueness, transaction rollback and concurrent start/attempt creation with DB integration tests. Follow existing migration and test helpers. No foreign keys or cascades.

## Task 3 Graph service and recovery worker

Create `server/internal/service/graph*.go`. Tests precede draft revision/publish, run start, fixed snapshots, submissions, acceptance, retry budget, approval, pause/cancel and durable reconcile behavior. Reuse one TaskService instance, support transaction-bound task creation, and persist reconciliation work with Task terminal writes. Run focused service tests plus race-sensitive DB cases.

## Task 4 HTTP and authorization

Create `server/internal/handler/graph*.go`, wire Handler/router/main. Test workspace isolation, human-only mutations, task-token node scoping, stale revisions, idempotency, and runtime authorization. Implement graph management, run controls, current-node context/submission/blocker APIs, plus the periodic worker lifecycle.

## Task 5 Existing dispatch ownership gates

Add regression tests before changing issue create/update/move/delete, comment routing/merging/replay, child-done, PR status synchronization, manual rerun, failure retry/reset and claim/start behavior. Share admission rules across HTTP and WS; exclude graph tasks from old input merging and transparent redispatch. Confirm ordinary issue paths still execute.

## Task 6 Daemon execution and artifact contract

Add graph capability/manifest/result protocol and tests using fake executable sessions. Use fresh managed environment, fixed skill/input snapshots, immutable repository refs, pass-only finalization, upload authorization, delivery receipts and stopped-execution receipts. Test missing capabilities, output/HEAD mismatch, failed delivery, cancellation and late completion. Never invoke installed real agents in default tests.

## Task 7 CLI and built-in guidance

Create `server/cmd/multica/cmd_graph.go` and HTTP-backed command tests. Add lifecycle commands plus node context/submit/block. Update built-in platform skill instructions and source maps for graph-specific operation; do not inject ordinary Squad delegation/status protocols into Graph execution.

## Task 8 Shared frontend

Create Graph API schemas/types/queries/mutations in core and step editor/run/approval views in views. Test malformed network responses and key control states. Wire both Web and Desktop routes and issue integration. Use existing platform adapters and semantic UI tokens, and Query for server state.

## Task 9 Verification and delivery

Run focused tests after each red/green step, then Go package tests, frontend tests/typecheck, and relevant integration checks. Review implementation against every V1 acceptance row, then perform a separate code-quality review. Fix failures and record any environment blockers precisely. No automatic commit, push or release is required by this request.

## Progress

- [x] Inspect current repository, preserve pre-existing edits, record implementation plan.
- [x] Core model and validators.
- [x] Persistence and transactional Graph service.
- [x] HTTP, authorization and durable worker.
- [x] Existing execution ownership gates.
- [x] Daemon execution snapshots, artifacts and lifecycle (built-in Codex adapter).
- [x] CLI and built-in guidance.
- [x] Shared frontend and platform wiring, including definition JSON import/export.
- [x] Scoped verification, full-package compilation, independent spec and quality reviews.

## Delivery evidence and boundaries

See [Graph V1 delivery](../graph-v1-delivery.md) for exact supported runtime scope, deployment switches and validation limits. All concrete findings from the two reviews were fixed: skill identity, current Git input SHA, claim rebinding, unclaimed cancellation, historical projections, expired approvals, workspace deletion, attachment retention race, node CLI metadata/comments, definition-save idempotency and JSON import/export.

Graph and relevant existing regressions passed against an isolated PostgreSQL 17 database. Core, relevant views, Web (after required Fumadocs generation) and Desktop checks passed; Go packages compile and relevant vet checks pass. The full Go suite has pre-existing Windows profile/path and CRLF frontmatter failures, recorded separately. Implementation verification did not invoke a real authenticated provider or deploy the feature.

## Authorized pre-push compatibility follow-up

The user subsequently authorized pushing after checking compatibility. The additional review reproduced and fixed a legacy concurrent-enqueue deadlock in migration 354, replaced the integration fixture's lock stub with the real migration 284 owner-lock function, added ordinary task lifecycle/database regressions, and corrected Graph diagnostic route coverage. Existing links for a workspace named `graphs` remain unchanged. Core tests (1,424), relevant views tests, frontend typechecks and scoped lint passed. Full views has one pre-existing Windows path-separator assertion failure; nine broader daemon/execenv Windows failures were reproduced on the untouched baseline. See the delivery document for rollout prerequisites and test limits.
