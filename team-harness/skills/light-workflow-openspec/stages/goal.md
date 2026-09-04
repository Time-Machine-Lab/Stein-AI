# Stage：产品定义

## 目的

基于当前产品状态和用户需求，明确本次要实现的内容、目标和范围，为 Proposal 提供依据。路由 ID 仍为 `goal`。

## 开始条件

- `Product Owner` 已了解当前产品状态并获取相关产品信息。
- 本次要解决的需求或下一步目标已经明确，`Product Owner` 可以据此形成产品定义。

## 负责人

`Product Owner`

## 执行

| 步骤 | 操作 | 执行人 |
| --- | --- | --- |
| 1. 明确需求 | 基于当前产品状态和相关信息，明确本次要解决的问题或下一步目标。 | `Product Owner` |
| 2. 形成产品定义 | 至少说明问题或机会、期望目标、包含和排除范围、验收依据；其他内容按需补充。 | `Product Owner` |
| 3. 提交结果 | 将结果写入当前 Issue，并通知 `Delivery Manager`。 | `Product Owner` |
| 4. 检查需求 | 检查需求是否清晰、范围是否可拆分；不清晰时退回补充。 | `Delivery Manager` |
| 5. 准备并推进 Stage | 需求清晰时创建以 Change 名称命名的 Change 集成分支，记录当前 `ref`，在 Change 根 Issue 中声明分支和 `ref`，完成本 Stage 并推进 `stage=proposal`。 | `Delivery Manager` |

## 完成条件

产品定义已清楚说明本次要实现什么、做到什么范围，以及如何判断完成，足以让 `Delivery Manager` 形成 Proposal。

## 失败路由

产品含义或范围冲突交给 `Product Owner`。项目上下文缺失或请求过大时，按 [exceptions.md](../policies/exceptions.md) 处理。
