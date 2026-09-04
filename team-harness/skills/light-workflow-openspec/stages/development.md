# Stage：开发与集成

## 目的

实现 Proposal 中已确认的任务，并将完成的结果集成到当前 Change 分支，为 QA 提供统一的验证基线。

## 开始条件

- Change Proposal 已创建并确认可执行。
- Change 集成分支及当前 `ref` 已在 Change 根 Issue 中声明。

## 负责人

`Delivery Manager` 负责任务分配、Issue 创建、集成和本 Stage 完成判断；被分配的 Agent 负责各自范围内的实现。

## 执行

| 步骤 | 操作 | 执行人 |
| --- | --- | --- |
| 1. 分配开发任务 | 基于 Proposal 拆分并分配任务，明确负责人、任务范围和需要完成的 Proposal Task；具体如何组合任务由 `Delivery Manager` 决定。Issue 创建和路由遵循 [stage-issue-routing.md](../policies/stage-issue-routing.md)。 | `Delivery Manager` |
| 2. 实现开发任务 | 读取当前 Workflow、Proposal 和分配的 Issue，基于 Issue 声明的 Change 分支 `ref` 工作，并使用 `openspec-apply-change` 完成负责范围内的实现。 | 被分配的 Agent |

## Stage Completion Point

`Delivery Manager` 确认所有进入 QA 前必须完成的任务都已完成，并已将相关 worktree 集成、合并和推送到当前 Change 分支后，本 Stage 完成。

## 失败路由

实现范围或技术冲突交给相关负责人处理；产品行为冲突交给 `Product Owner`；Change 分支或集成问题交给 `Delivery Manager`。
