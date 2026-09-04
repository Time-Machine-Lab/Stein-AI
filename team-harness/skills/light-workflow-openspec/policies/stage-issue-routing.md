# Stage Issue 创建与路由

## 规则

- 进入或推进一个 Change Stage 时，由 `Delivery Manager` 创建或更新对应的 Workflow Issue，并分配给固定 Role。
- Issue 的描述或首条评论必须写明 `mode=change`、`stage`、`role`、Change 根 Issue 和当前 `ref`；触发该 Issue 时使用相同路由参数。
- 多个独立工作项分别创建子 Issue，并通过 `parent_issue_id` 关联同一 Change 根 Issue；已有对应 Issue 时继续使用，不重复创建。
- Stage 完成后，由 `Delivery Manager` 创建或推进下一 Stage 的 Issue。Workflow 之外的任务使用普通 Issue，不套用本路由。

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
| `development` | `Delivery Manager` | 被分配的 Agent | 执行 Proposal 中的实现任务，可按任务拆分多个 Issue |
| `qa` | `Delivery Manager` | `QA Engineer` | 执行 QA 测试任务，可按测试范围拆分多个 Issue |
| `acceptance` | `Delivery Manager` | `Product Owner` | 执行 Product Acceptance |
| `delivery` | `Delivery Manager` | `Delivery Manager` | 最终交付和 Archive |

标准调用格式：

```text
light-workflow-openspec mode=change stage=<Stage> role=<固定 Role>
```
