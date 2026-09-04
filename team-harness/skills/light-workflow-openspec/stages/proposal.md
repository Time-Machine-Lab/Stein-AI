# Stage：OpenSpec Proposal

## 目的

把已确认的产品定义转化为可执行、可推进的 Change Proposal，拆分团队任务并明确责任边界。

## 开始条件

- 产品定义已完成，产品目标、范围、排除项和验收依据已确认。
- `Delivery Manager` 已创建 Change 集成分支，并记录当前 `ref`。
- Change 分支名称和当前 `ref` 已在 Change 根 Issue 中声明，作为所有后续 Task 的工作基线。

## 负责人

`Delivery Manager` 负责 Proposal、本 Stage 完成判断和后续 Task 路由。产品方向发生变化时，由 `Product Owner` 确认。

## 执行

| 步骤 | 操作 | 执行人 |
| --- | --- | --- |
| 1. 生成 Proposal | 使用 `openspec-propose` 描述 Change、识别影响范围，拆分可执行 Task 并记录依赖。 | `Delivery Manager` |
| 2. 推送 Proposal | 将 Proposal 和相关 OpenSpec 产物提交并推送到当前 Change 集成分支，更新 `ref`。 | `Delivery Manager` |
| 3. 专业评估（可选） | 根据 Change 复杂度邀请相关 Agent 评估实现、接口、数据或验证影响；需要修改时更新 Proposal 并再次提交推送。 | `Delivery Manager` 组织，相关 Agent 参与 |
| 4. 确认并推进 | 确认 Proposal 与产品定义一致、Task 边界可分配、依赖可处理；满足条件后完成本 Stage 并推进 `stage=development`。 | `Delivery Manager` |

## Role 动作

`Delivery Manager` 编写和维护 Proposal，定义 Task 边界，识别依赖并推进 Stage。其他 Role 仅在被邀请评估或产品方向发生变化时参与。

## 可以自由决定的部分

`Delivery Manager` 可以决定 Task 粒度、是否组织专业评估以及评估形式。Proposal 必须说明范围和完成条件，但不规定内部实现。

## Stage Completion Point

Proposal 已推送到 Change 集成分支，Task 边界和依赖可执行，且不存在未处理的开发阻塞时，本 Stage 完成。产品方向发生变化时，须先由 `Product Owner` 确认。

## 失败路由

产品分歧返回 Product Owner。Task 边界或依赖不清返回 Delivery Manager。范围扩大按 [exceptions.md](../policies/exceptions.md) 处理。
