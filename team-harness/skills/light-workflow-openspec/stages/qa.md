# Stage：QA 与返工

## 目的

验证 Change 分支中的实现是否满足产品定义和 Proposal，并在发现问题时路由返工，直到全部 QA 任务通过。

## 开始条件

- QA 前的必要开发任务已全部完成，已具备充分的 QA 条件。
- Change 集成分支已包含当前需要测试的内容。

## 负责人

`Delivery Manager` 负责创建和分配 QA Issue、路由结果及本 Stage 完成判断；`QA Engineer` 负责执行测试并提供结论。

## 执行

| 步骤 | 操作 | 执行人 |
| --- | --- | --- |
| 1. 分配 QA 任务 | 基于当前 Change 明确测试范围和测试任务，分配给 `QA Engineer`；Issue 创建和路由遵循 [stage-issue-routing.md](../policies/stage-issue-routing.md)。 | `Delivery Manager` |
| 2. 执行测试任务 | 读取当前 Workflow、产品定义、Proposal 和 QA Issue，基于 Change 分支 `ref` 工作，并使用 `openspec-apply-change` 完成分配的测试任务，记录可复现证据。 | `QA Engineer` |
| 3. 提交测试结果 | 将每个测试任务的结果和证据写入 QA Issue，并通知 `Delivery Manager`。 | `QA Engineer` |
| 4. 路由失败与返工 | `Delivery Manager` 分析失败结果：优先在原 QA Issue 中 `@` 对应负责人处理；实现问题交给对应开发 Agent，产品方向问题交给 `Product Owner`，缺少协作或资源时按公共依赖规则处理。只有没有对应 Issue 时才新建 Issue。修复并重新集成后，通知 `QA Engineer` 重新在当前Issue测试受影响任务。 | `Delivery Manager` 组织，相关 Agent 执行 |
| 5. 确认完成 | 确认所有 QA 测试任务均已完成且全部通过；满足条件后完成本 Stage，并推进 `stage=acceptance`。 | `Delivery Manager` |

## 测试结果最低规范

QA 结果至少包含以下内容，其余内容由 `QA Engineer` 自行补充，推荐基于自身能力补充更加详细的内容：

| 内容 | 要求 |
| --- | --- |
| 测试范围 | 说明本次覆盖的功能、场景和 QA Task。 |
| 测试环境与版本 | 说明测试环境、Change 分支和当前 `ref`。 |
| 测试项结果 | 逐项记录预期结果、实际结果和状态：通过、失败或未执行。 |
| 失败信息 | 对失败项记录复现步骤、预期与实际差异、影响范围和相关证据。 |
| 总体结论 | 明确本次测试通过、失败或无法得出结论。 |

## Stage Completion Point

所有 QA 测试任务都已完成并通过，失败项已完成修复、重新集成和验证后，本 Stage 完成。

## 失败路由

产品行为或范围问题交给 `Product Owner`；实现问题交给对应开发 Agent；Change 分支或集成问题交给 `Delivery Manager`。
