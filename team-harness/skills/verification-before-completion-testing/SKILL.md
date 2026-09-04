---
name: verification-before-completion-testing
description: Require fresh, proportionate evidence before a test engineer claims behavior passed, failed, was verified, or is ready. Use before QA conclusions; not for repairing production code, merging, releasing, or advancing team workflow.
license: MIT
metadata:
  version: "1.0.0"
  source: "https://github.com/obra/superpowers"
---

# Verification Before Completion for Testing

Match every quality claim to fresh evidence of the same scope. Testing completion means reaching a defensible conclusion; it does not require the product to pass.

## Evidence Gate

Before stating that something passed, failed, was verified, was fixed, or is ready:

1. State the exact claim and its scope.
2. Identify the observation, test, command, or artifact that can support or refute it.
3. Run or collect that evidence freshly in the relevant environment. Use the smallest verification sufficient for the claim; run broader suites only for broader claims or risks.
4. Inspect the actual result, including exit status, failures, logs, traces, environment, data conditions, and important limitations.
5. Classify the outcome as supported, contradicted, or inconclusive.
6. Express only the conclusion the evidence supports, together with material residual risk or unknowns.

## Interpret Failures Before Acting

- Reproduce or otherwise confirm a failure with effort proportional to its impact.
- Distinguish product behavior, test defect, environment or data problem, and unresolved instability.
- A confirmed product failure is a valid QA result. Preserve its evidence and stop at the testing conclusion or remediation recommendation.
- A defective test may be corrected when test maintenance is in scope, followed by fresh verification.
- Do not modify production code, redefine expected behavior, or advance into implementation merely because verification failed.

## Claim Calibration

- A targeted check supports only the targeted behavior.
- A passing suite supports only the behaviors, environments, and quality dimensions it actually exercises.
- A bug may be called verified as fixed only when the original reproduction no longer occurs under the relevant conditions and appropriate regression checks show no contradictory evidence.
- An incomplete, blocked, or non-reproducible result must remain explicitly inconclusive.
- Previous runs, another agent's summary, coverage numbers, successful compilation, or confidence are not substitutes for current evidence.

## Prohibited Shortcuts

Never weaken assertions, ignore failures, add unconditional retries, skip checks, or alter test data solely to produce a green result. Never generalize local success into a claim that the whole product is correct or defect-free.

This skill may produce verification notes, reports, logs, traces, and defect evidence. It does not repair production code, decide requirements, route work, merge changes, release software, or set project completion gates.
