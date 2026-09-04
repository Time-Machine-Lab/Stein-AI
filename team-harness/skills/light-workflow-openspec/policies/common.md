# 公共 Workflow 规则

## 共享概念

- **Change**：一个可以独立交付的产品变化。
- **Stage**：两个已确认产物之间的一段有边界的工作。
- **Issue**：Multica 中保存一项工作的目标、讨论、负责人和最终状态的持续协作记录；同一 Issue 可以多次执行。
- **Task**：智能体对某个 Issue 的一次执行记录，记录触发来源、执行智能体、过程和结果。

本 Workflow 不要求单独维护状态机。使用当前 Task、Project Context、OpenSpec 产物、分支、Candidate Version 和新鲜证据来判断当前工作位置。

## Change 边界

- 一个 Change 只有一个产品目标、一个 Proposal、一个集成结果和一个最终 PR。
- 如果包含独立产品目标、无法作为一个整体解释或验证，或者持续造成依赖等待和合并压力，应拆分 Change。
- 交付过程中发现的新想法，如果不在已确认范围内，应记录为新的 Change。

每个 Change 使用一个根 Issue 标识。所有子 Issue 必须通过 `parent_issue_id` 关联该根 Issue，不能仅凭负责人或标题判断所属 Change。

## Change 集成分支

- 每个 Change 开始时，由 `Delivery Manager` 按 Change 名称创建唯一的 Change 集成分支。
- Change 下所有子 Issue 产生的代码、文档和其他仓库变更，都必须合入该 Change 分支。
- 需要代码工作区的 Task 创建或恢复时，必须显式指定当前 Change 分支的 `ref`；不得基于默认分支或其他子 Issue 分支 checkout。
- Agent 完成后提交自己的 commit；`Delivery Manager` 负责合并、推送，并将最新 commit 作为后续 Task 的 `ref`。

## Issue 完成与集成

- Task 完成时，如产生代码、文档或其他仓库修改，必须在当前 Task 对应的 worktree 分支完成 `commit` 并 `push`，不得只保留本地未提交修改；这样 `Delivery Manager` 才能获取并集成。
- 任一 Workflow Issue 进入 `in_review` 后，统一由当前 Change 的 `Delivery Manager`（当前 `leader`）审核，并决定通过、返工或后续路由。
- `Delivery Manager` 验收并将 Issue 标记为 `done` 前，必须检查当前 Issue 是否存在已打开的 worktree；只要存在，就必须将当前 Issue 下的全部 worktree 集成到规定分支，完成合并并推送。
- Agent 不直接合并或推送 Change 分支；需要集成时，需要 `@` `Delivery Manager`。合并冲突或业务判断应该交回对应负责人处理。
- 任何 Agent 将依赖 Issue 改为 `done` 前，必须检查该 Issue 的依赖登记；对每个登记的甲方 Issue，在其中写明结果并 `@` 对应甲方 Agent。甲方收到新 Task 后重新检查结果，满足条件后继续原目标。

## 自由协作

在明确分配的范围内，Agent 可以自行选择调查方法、工具、技术设计、测试选择和协作方式。Workflow 约束的是必须交付的结果，不是产生结果的内部路径。

询问、讨论、征求专业意见或确认信息，属于自由协作。自由协作不要求对方完成独立工作、不要求对方提供独立产物，也不创建依赖 Issue；当前负责人继续在当前 Issue 和 Stage 内推进。

自由协作不得越过当前 Stage 的完成门禁，不得自行进入下一个 Stage，也不得自行创建或执行其他 Stage 的 Workflow 任务。Stage 推进只能由 `Delivery Manager` 按路由执行。

## 依赖协作

只有以下情况才使用依赖协作：当前工作被明确问题阻塞，或当前目标必须由另一位 Agent 完成明确工作、提供明确产物或解决明确问题。单纯询问意见、确认信息或请求建议，不属于依赖协作。
甲方：需求提出者
乙方：协作者
- 甲方需要依赖协作时，甲方先在小队内选择最适合解决问题的 Agent 并直接 @；小队内无人适合时，@当前 `leader`。
- 甲方请求必须说明：阻塞原因或所需帮助、明确需要的产物或操作、完成判定。
- 甲方只有确实无法继续时，才将当前 Issue 设为 `blocked`；未阻塞的明确产物依赖仍可继续当前工作。
- 乙方寻找当前需求是否正在被解决：先按当前 Issue 的 `parent_issue_id` 和 `project_id` 定位 Change 根 Issue，再依次检查：该 Change 根 Issue 下是否已有解决同一问题的子 Issue；整个项目下是否已有正在处理同一问题的 Issue。确认没有对应 Issue 后，则自行新建 Issue 解决该需求。
- 无论复用还是新建依赖 Issue，乙方都必须在该 Issue 中追加依赖登记：甲方 Issue、甲方 Agent、依赖内容和完成条件；然后在甲方 Issue 中说明应等待哪个 Issue 完成。
