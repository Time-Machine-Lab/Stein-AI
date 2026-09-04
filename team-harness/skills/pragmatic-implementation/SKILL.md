---
name: pragmatic-implementation
description: Guide non-trivial code implementation and refactoring toward explicit material assumptions, the simplest sufficient design, and a narrowly scoped change. Use when requirements admit meaningful interpretations, design complexity is a concern, or implementation may expand beyond the task. Do not use as a debugging procedure or a completion-verification checklist.
license: MIT
metadata:
  source: https://github.com/multica-ai/andrej-karpathy-skills/tree/main/skills/karpathy-guidelines
  adaptation: language-and-workflow-neutral
---

# Pragmatic Implementation

Improve implementation decisions without prescribing a language, architecture, testing method, team workflow, or delivery format.

## Decide Before Editing

- Inspect the available task and project context before choosing an implementation.
- Identify only assumptions that could materially change behavior, compatibility, risk, or scope.
- Resolve discoverable facts from the repository or available resources. Ask for clarification only when a consequential choice remains and guessing could produce the wrong result.
- When several approaches are viable, compare the meaningful tradeoffs and choose the least complex option that satisfies the actual constraints.

## Define a Sufficient Outcome

- Translate the request into observable functional and quality conditions before implementing.
- Include performance, security, reliability, or compatibility constraints only when the task, project context, or evidence makes them relevant.
- Do not turn the conditions into a mandatory document, plan format, or testing methodology.

## Prefer the Simplest Sufficient Design

- Reuse established project patterns when they meet the need.
- Avoid speculative features, premature abstractions, and configurability without a current requirement.
- Introduce complexity only for a concrete constraint or demonstrated risk, and keep it proportional to the expected benefit.
- Simplicity does not justify ignoring trust boundaries, data integrity, compatibility, or realistic failure modes.

## Keep the Change Focused

- Make the smallest coherent change that fully satisfies the outcome; necessary tests, migrations, documentation, and cleanup remain in scope.
- Do not refactor, reformat, or remove unrelated code merely because it could be improved.
- Preserve existing behavior and conventions outside the requested change.
- Remove artifacts made obsolete by this change, but leave pre-existing unrelated cleanup for separate work.

## Adapt the Depth

Compress this guidance for obvious, low-risk edits. Expand the reasoning when ambiguity, blast radius, irreversibility, or failure cost is material.

For a bug or unexpected behavior, use `systematic-debugging` for root-cause investigation. Before claiming the work is complete or correct, use `verification-before-completion` for fresh evidence.
