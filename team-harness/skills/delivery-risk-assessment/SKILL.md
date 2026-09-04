---
name: delivery-risk-assessment
description: Identify, assess, and track material delivery risks, blockers, assumptions, and mitigations. Use when work has meaningful uncertainty, dependencies, resource contention, or costly failure modes. Do not use to make product, technical, security, or risk-acceptance decisions on behalf of specialists.
license: Apache-2.0
metadata:
  source: https://github.com/anthropics/knowledge-work-plugins/blob/main/operations/skills/risk-assessment/SKILL.md
  adaptation: multica-delivery-manager
---

# Delivery Risk Assessment

Systematically identify, assess, and plan mitigations for material risks to delivery. Keep risks visible and actionable without creating a project state machine.

## Risk Assessment Matrix

| | Low Impact | Medium Impact | High Impact |
|---|---|---|---|
| **High Likelihood** | Medium | High | Critical |
| **Medium Likelihood** | Low | Medium | High |
| **Low Likelihood** | Low | Low | Medium |

Use the matrix to prioritize attention, not to create false precision.

## Delivery Risk Categories

- **Operational**: coordination failures, waiting, staffing gaps, environment outages
- **Schedule**: estimates, deadlines, critical-path slippage
- **Dependency**: upstream or downstream deliverables outside the current task
- **Scope**: ambiguous requirements, scope growth, conflicting expectations
- **Quality**: missing evidence, regression exposure, rework risk
- **External**: vendor, platform, policy, or other dependency changes
- **Security or compliance**: record the concern and route the decision to the qualified owner

## Risk Register Format

For each material risk, document:

- **Description**: What could happen
- **Likelihood**: High / Medium / Low
- **Impact**: High / Medium / Low
- **Risk Level**: Critical / High / Medium / Low
- **Trigger**: What observable signal means the risk is materializing
- **Mitigation**: What could reduce likelihood or impact
- **Contingency**: What could be done if it occurs
- **Owner**: Confirmed responsible role, or `unconfirmed`
- **Status**: Open / Mitigating / Occurred / Closed
- **Evidence**: Source observation and last-reviewed time

## Output

Produce a prioritized risk register with specific, actionable mitigations and unresolved decisions. Separate known facts, assumptions, and unknowns. Ask the relevant product, engineering, testing, security, or repository owner to decide matters outside delivery coordination.

## Boundaries

- Do not accept, close, or downgrade a risk without evidence or confirmation from the responsible authority.
- Do not choose technical mitigations, product trade-offs, security controls, or release decisions for another role.
- Do not create tasks, change status, assign agents, or advance an external workflow.
- When no meaningful risk is evidenced, say so rather than inventing a risk register.

