# Stage：Product Acceptance

## 目的

让 `Product Owner` 体验当前开发完成的效果，判断是否达到自己定义的产品目标和产品品味，以及已确认的成品范围是否完成。`acceptance_strength` 决定本 Stage 的处理方式：`low` 直接跳过，`medium` 快速体验确认可用，`high` 严格对照产品定义和产品品味验收。

## 开始条件

- QA 任务已全部完成。

## 负责人

`Product Owner` 负责 Product Acceptance 结论；`Delivery Manager` 负责是否跳过、Issue 路由、结果处理和本 Stage 完成判断。

## 执行

| 步骤 | 操作 | 执行人 |
| --- | --- | --- |
| 1. 读取验收强度 | 读取 `tml-docs/setup.yml` 的 `acceptance_strength`：缺失、为空或为 `low` 时跳过 Product Acceptance；为 `medium` 或 `high` 时进入验收。 | `Delivery Manager` |
| 2. 体验与判断 | `medium` 时快速体验当前 Change 版本并确认可用；`high` 时读取产品定义、Proposal 和验收依据，严格判断产品目标、产品品味和已确认范围是否达成。 | `Product Owner` |
| 3. 提交结论 | 在当前 Product Acceptance Issue 中写明通过、原范围不通过或新增范围，并通知 `Delivery Manager`。 | `Product Owner` |
| 4. 处理结论 | 原范围不通过时，回到原 Issue `@` 对应负责人返工；发现新增范围时记录为新的 Change；通过时推进 `stage=delivery`。 | `Delivery Manager` |

## Stage Completion Point

`Product Owner` 明确接受已确认范围，或 `Delivery Manager` 已明确记录跳过 Product Acceptance，且已完成结论路由时，本 Stage 完成。

## 验收强度

| 值 | 要求 |
| --- | --- |
| `low` | 直接跳过 Product Acceptance。 |
| `medium` | 快速体验，确认当前范围可用；允许一般性小差错。 |
| `high` | 严格对照产品定义和产品品味验收；不符合要求时返工。 |

## 失败路由

原范围未满足时返回对应的开发和 QA 路径；出现新增产品范围时创建新的 Change，不扩大当前 Change。
