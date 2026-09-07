# Stage：开发与集成

## 目的

实现 Proposal 中已确认的任务，并将完成的结果集成到当前 Change 分支，为 QA 提供统一的验证基线。

## 开始条件

- Change Proposal 已创建并确认可执行。
- Change 集成分支及当前 `ref` 已在 Change 根 Issue 中声明。

## 负责人

`Delivery Manager` 负责任务分配、Issue 创建、集成和本 Stage 的轻量完成检查；被分配的 Agent 负责各自范围内的实现。

## 执行

| 步骤 | 操作 | 执行人 |
| --- | --- | --- |
| 1. 指定 Driver | 基于 Proposal 指定一个能够端到端推进当前 Change 的 Driver。`micro` 可由 `Delivery Manager` 自己或一个具备所需权限的 Agent 执行；不得仅按固定 Role 数量拆分。 | `Delivery Manager` |
| 2. 按需拆分任务 | 只有存在独立产物、真实并行价值、专业/权限隔离或独立验证需求时，才创建并分配额外 Issue；单纯咨询由 Driver 保持当前 Issue 继续推进。 | `Delivery Manager` |
| 3. 实现开发任务 | Driver 读取当前 Workflow、Proposal 和分配的 Issue，基于 Issue 声明的 Change 分支 `ref` 工作，并使用 `openspec-apply-change` 完成负责范围内的实现；专业 Agent 仅处理明确分配的独立范围。 | Driver 或被分配的 Agent |

## Stage Completion Point

`standard` 和 `complex`：`Delivery Manager` 确认所有进入 QA 前必须完成的任务都已完成，并已将相关 worktree 集成、合并和推送到当前 Change 分支后，本 Stage 完成。

`micro`：Driver 已完成范围内工作、必要校验和提交推送，并提供完成证据后，`Delivery Manager` 可以直接收口并进入 `delivery`；只有发现产品判断、运行时风险或独立验证需求时，才补充 `qa` 或 `acceptance`。

## 失败路由

实现范围或技术冲突交给相关负责人处理；产品行为冲突交给 `Product Owner`；Change 分支或集成问题交给 `Delivery Manager`。平台断流或临时失败由原 Driver 重试，不因此重新分配角色。

## Stage 收口

完成本 Stage 不仅要求开发任务完成并集成到 Change 分支，还要求每个任务已按公共退出门禁完成 Handoff。`standard` 和 `complex` 由 `Delivery Manager` 检查任务是否完成、集成证据是否齐全以及是否具备 QA 条件，通过后实际创建或触发 `stage=qa`。`micro` 按其完成条件直接触发 `stage=delivery`，除非已记录需要补充的 QA 或 Product Acceptance。
