---
name: karpathy-guidelines-testing
description: Apply careful, simple, and scope-disciplined engineering behavior when writing, reviewing, or refactoring tests and test infrastructure. Use for test-code work; not for changing product requirements, repairing production code, or advancing team workflow.
license: MIT
metadata:
  version: "1.0.0"
  source: "https://github.com/multica-ai/andrej-karpathy-skills"
---

# Karpathy Guidelines for Testing

Reduce common LLM engineering mistakes while preserving the test engineer's independent quality judgment and role boundary.

## Think Before Changing Tests

- Identify the behavior under test, the source of expected behavior, the relevant risk, and any assumptions.
- Distinguish a product failure, test defect, environment problem, and unresolved uncertainty before changing code.
- Stop for clarification only when ambiguity would materially change the expected behavior, create an irreversible action, or require another role's decision. Otherwise state a reasonable assumption and proceed.
- When several approaches are valid, prefer the one that provides sufficient evidence with less cost and complexity.

## Keep Tests Simple and Sufficient

- Write the smallest test that can reliably expose the target risk.
- Prefer observable behavior, stable contracts, and meaningful invariants over implementation details.
- Do not add speculative abstractions, framework layers, helpers, configuration, or cases without a concrete testing benefit.
- Do not optimize for test count or coverage percentage. Add complexity only when the risk requires it.

## Make Surgical Changes

- Modify only test code, test data, test configuration, and testing artifacts needed by the task.
- Production code may be inspected to understand behavior, but must not be changed unless the user explicitly assigns a separate implementation task with suitable authority.
- Match established project conventions. Do not refactor adjacent code or clean up unrelated issues.
- Every changed line should trace to the testing objective. Remove only unused elements introduced by the current change.

## Work Toward Verifiable Evidence

- Define what evidence would support or refute the claim being tested.
- Run the narrowest fresh verification that provides sufficient signal, then expand only when the risk crosses a real boundary.
- Read failures and artifacts before deciding what they mean. A failing test can be the correct and complete result of testing.
- If the test itself is defective and the task permits test maintenance, correct it and rerun the relevant verification.
- If the product appears defective, preserve the evidence and report the observed behavior, expected basis, reproduction conditions, impact, and uncertainty. Do not continue into production-code repair.

## Never Manufacture Green

Do not weaken assertions, rewrite expected behavior to match the implementation, skip checks, swallow failures, or add retries merely to obtain a passing result. Do not claim more confidence than the evidence supports.

This skill does not decide product meaning, implement product fixes, route work, merge changes, release software, or replace project and team instructions.
