# Graph Creation Studio Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use `executing-plans` to implement this plan task-by-task.

**Goal:** Make Graph creation consistent with existing product pages and let users generate an editable serial workflow from a natural-language request.

**Architecture:** Reuse the existing shared creation-page patterns and Graph save/validate/publish APIs. Generate a definition through the existing server LLM client, validate it against Graph V1 and workspace references, and return it without saving or running anything. The shared editor presents a serial flow and selected-step properties; advanced configuration remains available.

**Tech Stack:** React, TanStack Query, existing Base UI primitives, zod, Go/Chi, existing `llm.Client`.

---

### Task 1: Generation boundary

Files: `server/internal/handler/graph_generation.go`, its colocated tests, `server/cmd/server/router.go`, `docker-compose.selfhost.yml`.

1. Add failing tests for workspace access, disabled LLM, malformed model output, fixed executor/approver IDs, and valid generation without persistence.
2. Run the focused handler tests and verify the failure is the missing generation behavior.
3. Implement `GET /api/graphs/generation-options` returning `{enabled}` and `POST /api/graphs/generate` accepting `{prompt, agent_id, approver_id}` and returning `{definition}`. Use bounded structured generation and full V1 validation. Never save, publish or dispatch tasks here.
4. Pass existing LLM configuration into the self-hosted backend container and document setup; introduce no new credentials or migration.
5. Run focused tests with the isolated test database, followed by relevant service regression tests.

### Task 2: Shared client

Files: `packages/core/api/client.ts`, `packages/core/graphs/{types,schemas,queries,mutations}.ts`, `packages/core/graphs/generation.test.ts`.

1. Add failing API boundary tests for snake-case request encoding, parsed response, malformed definitions and workspace-isolated option keys.
2. Run `pnpm --filter @multica/core test graphs/generation.test.ts` and observe missing API failures.
3. Implement parsed generation methods, `graphGenerationOptions(wsId)` and `useGenerateGraph(wsId)` without touching saved Graph caches.
4. Rerun Graph tests and core typecheck.

### Task 3: Creation and editing workspace

Files: `packages/views/graphs/graph-editor.tsx`, `graph-detail-page.tsx`, focused new components/tests, `packages/views/locales/{en,zh-Hans,ja,ko}/graphs.json`.

1. Add failing interaction tests for AI creation, manual creation, local-only generated drafts, failed requests preserving input, and selection/editing without data loss.
2. Build an AI-first entry with example prompts and a manual shortcut, following `agents/create/choose-create-method-page.tsx`.
3. Build a connected serial-step overview plus a selected-step inspector; move technical fields to advanced controls and put save/validate/publish in a persistent action area.
4. Preserve revision/idempotency guards, permission states, unknown-definition read-only behavior and import/export fidelity.
5. Run focused view tests and typechecks; inspect desktop and narrow-width rendering. Keep Graph execution semantics unchanged.

### Task 4: Delivery review

1. Review implementation against this scope and then review code quality and regression risks independently.
2. Check loading, disabled AI, errors, overflow, long labels and keyboard access.
3. Report actual verification and any configuration requirement. Do not claim production deployment or live model success from mocked tests.

## Scope decisions

- V1 remains serial: start, 1–16 agent steps, final human approval, end.
- Generation uses a user-selected eligible agent and approver; they are never invented by the model.
- Existing server LLM configuration is reused; unconfigured deployments retain manual creation.
- No parallel edges, loops, scheduling, new runtime, schema migrations, or automatic publication.
- Existing unrelated `.codex/` and `openspec/` changes remain untouched.
