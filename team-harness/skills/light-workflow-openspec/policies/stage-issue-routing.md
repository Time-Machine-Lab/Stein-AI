# Stage Issue 创建与路由

## 规则

- 进入或推进 `standard` 或 `complex` Change Stage 时，由 `Delivery Manager` 创建或更新对应的 Workflow Issue，并分配给固定 Role；`micro` Change 按其轻量路径执行，不要求为每个 Stage 创建独立 Issue。
- Issue 的描述或首条评论必须写明 `mode=change`、`stage`、`role`、Change 根 Issue 和当前 `ref`；触发该 Issue 时使用相同路由参数。
- 多个独立工作项分别创建子 Issue，并通过 `parent_issue_id` 关联同一 Change 根 Issue；已有对应 Issue 时继续使用，不重复创建。
- 创建 Stage Issue 前，`Delivery Manager` 必须先记录 `change_profile` 和当前 Driver。`micro` Change 不为没有独立产物的 Role 创建子 Issue；`standard` Change 先创建一个 Driver Issue，只有满足真实拆分条件时才增加子 Issue；`complex` Change 才按独立产物并行拆分。
- Stage 完成后，由 `Delivery Manager` 创建或推进下一 Stage 的 Issue。Workflow 之外的任务使用普通 Issue，不套用本路由。

## Change 根 Issue 归属

Change 根 Issue 表达产品目标、范围和最终结果，默认继续归 `Product Owner`；`Delivery Manager` 不因为负责流程协调而自动取得根 Issue 所有权。`Delivery Manager` 负责的是 Stage Issue 的创建、分配、轻量完成检查和连续推进。根 Issue 被平台唤醒时，仍通过有效 Handoff `@mention` 具体的 Delivery Manager 成员账号，由其在对应 Stage Issue 上继续路由，不需要改变根 Issue 的产品归属。

## 完成确认规则

`in_review` 不代表固定交给某个角色做全面审核。进入该状态后，按 Stage 的完成确认规则处理；确认只检查完成条件、必需产物、验证证据、当前 `ref` 和后续路由是否齐全，不重复评审执行角色已经负责的专业质量。

| Stage | 完成确认人 | 确认范围 | 通过后的动作 |
| --- | --- | --- | --- |
| `goal` | `Delivery Manager` | 产品定义是否完整、范围和验收依据是否可执行 | 收口当前 Stage，创建或触发 `proposal` |
| `proposal` | `Delivery Manager` 自检 | Proposal、Task 边界和依赖是否齐全 | 直接收口当前 Stage，创建或触发 `development` |
| `development` | `Delivery Manager` | 开发任务是否完成、worktree 是否已集成、Change `ref` 是否可供 QA | 收口当前 Stage，创建或触发 `qa` |
| `qa` | `Delivery Manager` | QA 结论、范围、环境和证据是否足以判断通过或返工 | 收口当前 Stage，创建或触发 `acceptance` |
| `acceptance` | `Product Owner` | 产品目标、范围和验收依据是否达成 | `Delivery Manager` 按结论收口并触发 `delivery` 或返工 |
| `delivery` | `Delivery Manager` 自检 | Archive、PR、合并和交付记录是否完成 | 直接收口 Change；仅明确要求人类 PR 审核时暂停 |

`Delivery Manager` 的确认是轻量完成检查，不是对专业实现、测试方法或产品判断进行重复审核。由 `Delivery Manager` 自己负责的 Stage 不得等待自己审核。

## Change 完成后的处理

- 当前 Change 完成后，由 `Delivery Manager` 根据需求声明判断是否继续。
- 用户明确声明停止边界时，达到该边界即结束本 Workflow。
- 明确只要求完成单个需求时，该需求完成即结束本 Workflow。
- 未明确声明停止边界时，默认持续创建新的 Change，直到完整产品目标完成。
- 需要继续时，创建新的 Change 根 Issue，并从 `stage=goal` 重新开始；不得继续使用已完成的 Change。

## Stage 路由表

| Stage | 创建或推进人 | 指派 Role | Issue 用途 |
| --- | --- | --- | --- |
| `goal` | `Delivery Manager` | `Product Owner` | 形成产品定义 |
| `proposal` | `Delivery Manager` | `Delivery Manager` | 生成和确认 Proposal |
| `development` | `Delivery Manager` | 当前 Driver；必要时再分配专业 Agent | 执行 Proposal 中的实现任务；仅在存在独立产物、并行价值或专业隔离时拆分多个 Issue |
| `qa` | `Delivery Manager` | `QA Engineer` | 执行 QA 测试任务，可按测试范围拆分多个 Issue |
| `acceptance` | `Delivery Manager` | `Product Owner` | 执行 Product Acceptance |
| `delivery` | `Delivery Manager` | `Delivery Manager` | 最终交付和 Archive |

标准调用格式：

```text
light-workflow-openspec mode=change stage=<Stage> role=<固定 Role>
```
