# Stage：Delivery and Archive

## 目的

在交付证据完整后合并已验收的 Change，并 Archive 其 OpenSpec 记录。

## 开始条件

- 当前 OpenSpec Change 的所有 Task 已完成，且 Change 根 Issue 及其子 Issue 均没有正在进行的 Issue。

## 负责人

`Delivery Manager` 负责最终交付、合并和所有仓库操作。

## 执行

| 步骤 | 操作 | 执行人 |
| --- | --- | --- |
| 1. 检查交付条件 | 确认所有 Task 和 Issue 已完成，当前 Change 分支包含完整交付内容，可以开始交付。 | `Delivery Manager` |
| 2. Archive Change | 使用 `openspec-archive-change` Archive 当前 OpenSpec Change，并选择生成规范和同步。 | `Delivery Manager` |
| 3. 创建 PR | 按正常仓库流程创建最终 PR。 | `Delivery Manager` |
| 4. 等待门禁并合并 | 读取 `tml-docs/setup.yml` 的 `pr_manual_review`：缺失或为 `off` 时等待自动门禁通过后直接合并；为 `on` 时阻塞当前任务并通知人类进行 PR 审核，审核和门禁均通过后再合并。 | `Delivery Manager` |

## Stage Completion Point

OpenSpec Change 已 Archive、PR 已合并且没有正在进行的 Issue 时，Change 完成。

## 失败路由

证据缺失或结论对应不同 Candidate Version 时，先补齐 QA 或 Product Acceptance；合并暴露产品或代码冲突时，交给对应负责人处理，不得直接 Archive。
