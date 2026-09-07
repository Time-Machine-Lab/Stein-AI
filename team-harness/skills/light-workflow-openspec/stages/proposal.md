# Stage：OpenSpec Proposal

## 目的

把已确认的产品定义转化为可执行、可推进的 Change Proposal，按实际需要拆分任务并明确责任边界。

## 开始条件

- `standard` 和 `complex`：产品定义已完成，产品目标、范围、排除项和验收依据已确认。
- `micro`：可以在同一 Task 中用精简记录同时说明目标、范围、排除项和完成条件，不要求单独创建 `goal` Stage Issue。
- `Delivery Manager` 已创建 Change 集成分支，并记录当前 `ref`。
- Change 分支名称和当前 `ref` 已在 Change 根 Issue 中声明，作为所有后续 Task 的工作基线。

## 负责人

`Delivery Manager` 负责 Proposal、本 Stage 的轻量完成检查和后续 Task 路由。产品方向发生变化时，由 `Product Owner` 确认。

## 执行

| 步骤 | 操作 | 执行人 |
| --- | --- | --- |
| 1. 判断复杂度并指定 Driver | 记录 `change_profile`（`micro`、`standard` 或 `complex`）和一个 Driver。先判断是否可以由一个 Agent 连续完成；只有存在独立产物、并行价值、专业/权限隔离或独立验证需求时才拆分 Task。 | `Delivery Manager` |
| 2. 生成 Proposal | 使用 `openspec-propose` 描述 Change、识别影响范围；`micro` 可在同一 Task 中使用精简方案，不创建没有独立产物的子 Issue。 | `Delivery Manager` |
| 3. 检查基线文档影响 | 判断本 Change 是否改变 `tml-docs/` 中的长期产品、工程、设计、测试或 Workflow 约束；记录 `docs_impact: none` 或 `docs_impact: update`。 | `Delivery Manager` |
| 4. 创建文档维护任务 | `docs_impact=update` 时列出文件、原因和完成条件，创建或复用对应 Role 的文档维护任务；`docs_impact=none` 时记录判断理由，不创建任务。 | `Delivery Manager` |
| 5. 推送 Proposal | 将 Proposal、文档影响判断和相关 OpenSpec 产物提交并推送到当前 Change 集成分支，更新 `ref`。 | `Delivery Manager` |
| 6. 专业评估（按需） | 仅当 `change_profile=complex`，或 `standard` Change 存在明确专业风险时邀请相关 Agent；咨询不会自动转移 Driver 所有权。 | `Delivery Manager` 组织，相关 Agent 参与 |
| 7. 确认并推进 | 确认 Proposal 与产品定义一致、Driver、Task 边界和依赖可执行、文档影响已记录且维护任务已纳入后续计划；满足条件后完成本 Stage 并推进 `stage=development`。 | `Delivery Manager` |

## Role 动作

`Delivery Manager` 编写和维护 Proposal，识别基线文档影响，定义 Task 边界，识别依赖并推进 Stage。其他 Role 仅在被邀请评估、确认文档影响或执行文档维护任务时参与。

## 文档影响记录格式

在 Proposal 或 Change 根 Issue 中记录一次判断，使用以下最小格式：

```text
docs_impact: none
reason: 本次仅修改单个页面的局部交互，不改变项目长期设计或工程约束。
```

需要更新时：

```text
docs_impact: update
docs:
  - tml-docs/frontend-development.md
reason: 新增全局路由约束，影响后续页面入口和导航组织。
owner: Frontend Engineer
done_when: 文档已更新并与代码在同一 Change 分支提交。
```

## 可以自由决定的部分

`Delivery Manager` 可以决定 Task 粒度、是否组织专业评估以及评估形式。Proposal 必须说明范围和完成条件，但不规定内部实现。

## Stage Completion Point

Proposal 已推送到 Change 集成分支，`docs_impact` 已记录，文档维护任务（如有）已明确负责人和完成条件，Task 边界和依赖可执行，且不存在未处理的开发阻塞时，本 Stage 完成。产品方向发生变化时，须先由 `Product Owner` 确认。

## 失败路由

产品分歧返回 Product Owner。Task 边界或依赖不清返回 Delivery Manager。范围扩大按 [exceptions.md](../policies/exceptions.md) 处理。

## Stage 收口

完成本 Stage 不仅要求 Proposal 满足完成条件，还要求已按公共退出门禁完成 Handoff。若 `docs_impact=update`，对应文档维护任务必须纳入当前 Change，并在 Delivery 前完成；若为 `none`，必须保留判断理由。由于本 Stage 由 `Delivery Manager` 负责，满足条件后直接收口并实际创建或触发 `stage=development`，不进入等待自己审核的 `in_review`。
