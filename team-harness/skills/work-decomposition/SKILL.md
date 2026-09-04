---
name: work-decomposition
description: Decompose a product goal into executable work, dependency relationships, parallelization opportunities, and verifiable task briefs. Use when work is too large, dependencies are unclear, or multiple specialist roles may contribute. Do not use for single obvious changes or for dispatching agents.
license: MIT
metadata:
  sources:
    - https://github.com/addyosmani/agent-skills/tree/main/skills/planning-and-task-breakdown
    - https://github.com/wshobson/agents/tree/main/plugins/agent-teams/skills/task-coordination-strategies
  adaptation: multica-delivery-manager
---

# Work Decomposition

Turn a decided product goal into small, understandable, independently verifiable work units. This skill provides analysis for an external workflow; it does not own the tracker, routing, lifecycle, or agent dispatch.

## When to Use

- A requirement contains multiple deliverables or specialist roles.
- Work feels too large or vague to start.
- Dependencies, parallel work, or the critical path are unclear.
- A task lacks acceptance criteria, interface context, or scope boundaries.
- Workload signals show an item is blocked, oversized, or unevenly distributed.

Do not use for a single-file change with obvious scope. Do not use to spawn, assign, reassign, message, merge, or advance agents.

## Decomposition Strategies

### By Layer

Split work by architectural layer when that matches the product boundary:

- Frontend components
- Backend API endpoints
- Database migrations/models
- Test suites

Use this for full-stack features and vertical slices. A layer split is an analysis option, not a mandate.

### By Component

Split work by functional component, such as authentication, profile, or notifications. This is useful for modular systems and services.

### By Concern

Split work by cross-cutting concern, such as security, performance, or architecture review. Keep the concern bounded and name the specialist who must decide it.

### By File Ownership

File or directory boundaries can reduce conflicts during parallel implementation. Treat them as proposed boundaries only; the relevant engineering role confirms actual ownership and interface contracts.

## Dependency Graph Design

1. Minimize chain depth. Prefer wide, shallow graphs when dependencies are not real.
2. Identify the critical path: the longest chain of genuinely dependent work.
3. Use dependencies sparingly. Do not add a dependency merely because work is related.
4. Avoid circular dependencies. Report them as a blocking design problem.

Common patterns:

```text
Independent:       A --+
                      B --+--> Integration
                      C --+

Sequential:        A -> B -> C

Diamond:           A -> B --+
                    \-> C --+--> D
```

## Task Brief

Every proposed task should include:

1. **Objective** - What needs to be accomplished.
2. **Owned area** - Files, services, or artifacts likely involved; confirm with the specialist before treating this as an authorization boundary.
3. **Requirements** - Specific behaviors or deliverables.
4. **Interface context** - How this work connects to other work.
5. **Acceptance criteria** - Conditions that can be checked.
6. **Scope boundaries** - What is explicitly out of scope.
7. **Verification evidence** - What observation or artifact would support completion.

Use this neutral structure when the external workflow accepts a task brief:

```markdown
## Objective
[One or two sentences]

## Proposed owned area
- [Files, service, or artifact]

## Requirements
- [Requirement]

## Interface context
- [Upstream or downstream relationship]

## Acceptance criteria
- [Specific, testable condition]

## Out of scope
- [Explicit exclusion]

## Verification evidence
- [Command, test result, review artifact, or observable behavior]
```

## Parallelization

Mark work as parallelizable only when it has no unmet sequential dependency, no unsafe shared state, and an explicit interface or handoff. When parallel work shares a contract, define that contract first. Return the reason for a sequential recommendation when work cannot safely run in parallel.

## Workload Signals

| Signal | Interpretation | Delivery-manager response |
|---|---|---|
| One role idle while another is overloaded | Uneven distribution | Report the imbalance and propose options; do not reassign automatically |
| One item remains stuck | Possible blocker or missing decision | Identify the blocker, evidence, and decision owner |
| Many items depend on one unfinished item | Critical-path concentration | Surface the critical path and alternatives |
| A task is much larger than its peers | Poor slicing or hidden scope | Propose a smaller slice or a time-boxed discovery item |

## Output Contract

Return only a bounded analysis that the outer workflow can consume:

```text
- goal_summary
- tasks[]: objective, suggested_role, dependencies, parallelizable, acceptance_conditions, required_evidence, out_of_scope
- critical_path
- blockers[]
- workload_signals[]
- open_questions[]
```

Do not write a plan file, create tracker items, change task status, assign people, call agent-team APIs, or decide technical implementation.
