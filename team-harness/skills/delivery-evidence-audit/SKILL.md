---
name: delivery-evidence-audit
description: Audit whether a delivery, fix, or status claim is supported by fresh, scope-matched, traceable evidence. Use before a delivery claim is handed to the external workflow. Do not run specialist QA, merge code, release software, or advance lifecycle state.
license: MIT
metadata:
  source: https://github.com/obra/superpowers/tree/main/skills/verification-before-completion
  adaptation: multica-delivery-manager
---

# Delivery Evidence Audit

## Core Principle

**Evidence before claims, always.** No delivery manager should endorse a completion claim unless the available evidence supports the same claim and scope.

## Audit Function

Before endorsing a claim:

1. **Identify** the exact claim and its scope.
2. **Request or locate** the evidence produced by the responsible specialist or verification role.
3. **Check freshness**: evidence must come from the relevant current change and environment.
4. **Check scope**: evidence must cover the behavior, requirement, or artifact named in the claim.
5. **Check result**: inspect the actual result, failures, limitations, and environment conditions supplied with the evidence.
6. **Classify** the claim as `supported`, `contradicted`, or `inconclusive`.
7. **Report only** what the evidence supports, including material residual risk or missing evidence.

The delivery manager audits the evidence package. The responsible engineer, tester, or product role performs the professional verification appropriate to its area.

## Common Claim Requirements

| Claim | Minimum evidence | Not sufficient |
|---|---|---|
| Tests pass | Relevant fresh test result with failures and exit status | A previous run or an agent's summary |
| Build succeeds | Relevant build result with exit status | A linter result alone |
| Bug is fixed | Original symptom no longer reproduces plus relevant regression evidence | Code was changed |
| Requirements are met | Requirement-by-requirement evidence | Tests passing alone |
| Delivery is ready | Evidence package covering the agreed delivery conditions | A confident status message |

## Output Contract

```text
- claim
- evidence_found[]
- freshness_and_environment
- scope_match
- contradictions_or_limitations[]
- missing_evidence[]
- audit_result: supported | contradicted | inconclusive
```

## Boundaries

- Do not run or replace specialist QA, engineering, security, performance, or product acceptance.
- Do not modify source code, tests, requirements, task status, PRs, branches, or release state.
- Do not convert `inconclusive` into `supported` because the result is inconvenient.
- Do not infer that one targeted check proves broader correctness.
- Do not decide who receives the result next; return the audit to the external workflow.

