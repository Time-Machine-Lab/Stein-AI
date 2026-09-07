# 公共 Workflow 规则

## 共享概念

- **Change**：一个可以独立交付的产品变化。
- **Stage**：两个已确认产物之间的一段有边界的工作。
- **Issue**：Multica 中保存一项工作的目标、讨论、负责人和最终状态的持续协作记录；同一 Issue 可以多次执行。
- **Task**：智能体对某个 Issue 的一次执行记录，记录触发来源、执行智能体、过程和结果。

本 Workflow 不要求单独维护状态机。使用当前 Task、Project Context、OpenSpec 产物、分支、Candidate Version 和新鲜证据来判断当前工作位置。

## 项目基线文档

项目通过 `tml-docs/` 保存会被后续 Agent 持续读取的长期基线文档，例如产品概念、前后端开发规范、设计规范、QA 测试环境和 `setup.yml`。Skill 目录下的 `document-standards/` 是这些文档的模板和编写规则，不是每个 Change 都要修改的项目文档。

基线文档的路径和职责保持稳定，但内容不是永久不可变。只有当 Change 改变了产品目标或流程、技术栈或工程边界、设计系统、测试环境，或项目级 Workflow 配置时，才需要更新对应文档；一次性实现细节、局部 Bug 修复和 Change 专属信息留在 Proposal、Issue、OpenSpec 或 PR 中。

每个 Change 都必须记录一次文档影响判断：

- `docs_impact: none`：说明没有改变长期基线，并写明判断理由。
- `docs_impact: update`：列出需要更新的 `tml-docs/` 文件、更新原因和完成条件，并创建或复用对应 Role 的文档维护任务。
- 判断不清时，不得默认为 `none`；交给相关专业 Role 确认。

文档维护任务由对应 Role 执行：Product Owner 负责产品文档，Backend Engineer 负责后端文档，Frontend Engineer 负责前端和设计文档，QA Engineer 负责测试文档，Delivery Manager 负责 `setup.yml` 和 Workflow 配置。文档更新必须与本 Change 使用同一分支和交付记录，并在后续 QA、Acceptance 和 Delivery 前完成集成。

## Change 边界

- 一个 Change 只有一个产品目标、一个 Proposal、一个集成结果和一个最终 PR。
- 如果包含独立产品目标、无法作为一个整体解释或验证，或者持续造成依赖等待和合并压力，应拆分 Change。
- 交付过程中发现的新想法，如果不在已确认范围内，应记录为新的 Change。

## Change 复杂度与 Driver

- 每个 Change 进入执行前，由 `Delivery Manager` 记录 `change_profile`：`micro`、`standard` 或 `complex`。分级依据是任务结构和风险，不是固定 Role 的数量。
- `micro` 适用于边界清晰的 Skill、文档、配置和小范围维护；保留目标、完成条件和必要校验，但不创建没有独立产物的产品、开发或 QA 子 Issue。
- `standard` 使用现有 Stage 顺序，但先指定一个 Driver 保持任务连续，再根据实际需要引入其他 Role。
- `complex` 适用于真实并行、专业或权限隔离、独立验证，或单一上下文不足的任务；只有此类条件成立时才按独立产物拆分多个 Agent。
- 无法判断时按 `standard` 处理，并由 `Delivery Manager` 在 Proposal 中补充判断；不得仅因任务涉及代码仓库、Git 或某个专业领域就判为 `complex`。
- 每个 Change 默认只有一个 Driver。Driver 对当前目标、上下文、产物和下一步负责；其他 Agent 的咨询不会改变 Driver。只有另一 Agent 必须独立完成明确产物、操作或验证时，才创建正式依赖或交接。
- `micro` Change 默认不超过一次正式专业交接；超过后由 `Delivery Manager` 重新评估是否应由原 Driver 继续或改走 `standard`/`complex` 路径。

每个 Change 使用一个根 Issue 标识。所有子 Issue 必须通过 `parent_issue_id` 关联该根 Issue，不能仅凭负责人或标题判断所属 Change。

## Change 集成分支

- 每个 Change 开始时，由 `Delivery Manager` 按 Change 名称创建唯一的 Change 集成分支。
- Change 下所有子 Issue 产生的代码、文档和其他仓库变更，都必须合入该 Change 分支。
- 需要代码工作区的 Task 创建或恢复时，必须显式指定当前 Change 分支的 `ref`；不得基于默认分支或其他子 Issue 分支 checkout。
- Agent 完成后提交自己的 commit；`Delivery Manager` 负责合并、推送，并将最新 commit 作为后续 Task 的 `ref`。

## Issue 完成与集成

- Task 完成时，如产生代码、文档或其他仓库修改，必须在当前 Task 对应的 worktree 分支完成 `commit` 并 `push`，不得只保留本地未提交修改；这样 `Delivery Manager` 才能获取并集成。
- 任一 Workflow Issue 进入 `in_review` 后，按当前 Stage 文件定义的完成确认人和完成条件处理；`in_review` 是团队内部的完成确认状态，不等于等待人类审核，也不要求 Delivery Manager 重做专业质量审核。
- `Delivery Manager` 验收并将 Issue 标记为 `done` 前，必须检查当前 Issue 是否存在已打开的 worktree；只要存在，就必须将当前 Issue 下的全部 worktree 集成到规定分支，完成合并并推送。
- Agent 不直接合并或推送 Change 分支；需要集成时，需要 `@` `Delivery Manager`。合并冲突或业务判断应该交回对应负责人处理。
- 任何 Agent 将依赖 Issue 改为 `done` 前，必须检查该 Issue 的依赖登记；对每个登记的甲方 Issue，在其中写明结果并 `@` 对应甲方 Agent。甲方收到新 Task 后重新检查结果，满足条件后继续原目标。

## Workflow 连续推进

- 本 Workflow 默认持续自动推进。除明确需要人类输入、权限、资源或外部决策外，任何问题都必须转为重新分配、依赖协作或返工，不得让流程停在当前 Task。
- Task 在 Change 目标完成前不得自行结束。评论中写出角色名称、姓名或“请某某处理”不等于触发协作；只有在同一条退出评论中使用平台可解析的具体 Agent `@mention`，下一项工作才算真正交给责任人。
- 所有非终态退出（`in_progress`、`in_review`、`backlog` 或 `blocked`）必须指定且实际 `@mention` 一个主要下一责任人，并写明唯一下一动作和完成条件。需要多个角色时只 `@mention` 一个负责继续路由的 Agent，由其再分配，避免一次退出产生多头唤醒。
- `@mention` 必须指向小队中可被平台解析的具体成员账号，不能只写 `@Delivery Manager`、`@Backend Engineer` 等 Role 名称。若无法解析目标成员，不得结束 Task；应继续当前 Issue，或先联系能够确定成员账号的 `Delivery Manager`。
- 终态 `done` 仅适用于当前目标已完全完成且没有后续动作；此时可以不再 `@mention`。只要评论中出现“等待、继续、审核、集成或下一步”，就不能在缺少具体 `@mention` 的情况下结束 Task。
- 完成或阻塞评论应至少按以下顺序出现：`@具体成员账号`、结果或阻塞原因、产物/证据、唯一下一动作、完成条件。先写角色名再补普通文字不算触发；必须确保平台实际识别出该 mention。
- 需要集成、合并或仓库操作时，必须 `@mention` 具体的 Delivery Manager 成员账号；只写“请 Delivery Manager 处理”不构成有效交接。
- `in_review` 只代表当前 Stage 已提交完成确认。确认范围限于完成条件、必需产物、验证证据、当前 `ref` 和后续路由是否齐全，不重复评审专业实现质量；专业质量由执行该工作的 Role 自己负责。
- 各 Stage 的完成确认人由 Stage 文件定义：需要 Delivery Manager 时只做轻量完成检查；由 Delivery Manager 自己负责的 Stage，满足完成条件后可直接将 Issue 设为 `done`，不得等待自己审核。
- Agent 每次退出当前 Task 前，必须完成以下一种有效 Handoff：
  1. 工作完成：写明结果、产物、验证证据和当前 `ref`，将 Issue 设为 `in_review`（若当前 Stage 规定直接收口则设为 `done`）；若仍有后续 Stage，必须在同一条评论中 `@mention` 一个具体下一责任人并明确要求审核、收口或推进。
  2. 依赖团队 Agent：写明问题、所需产物、完成条件，并在同一条评论中 `@mention` 一个具体协作者或负责路由的 Agent；当前 Issue 保持 `in_progress`，不得仅因等待 Agent 而设为 `blocked`。
  3. 需要人类介入：写明具体需要的人类输入、权限、资源或外部决策，以及缺少它为何无法继续；在平台支持时 `@mention` 具体处理人，否则 `@mention` 负责恢复流程的具体 Agent，只有此时才将 Issue 设为 `blocked`。
- 仅发表评论说“无法执行”“缺少条件”“发现问题”“请 Delivery Manager 处理”或“下一步再处理”，不构成有效 Handoff；必须实际 `@mention` 下一责任人并说明可验收的完成条件。
- Agent 发现角色不匹配、被错误分配、路由参数不一致或任务应由其他 Role 处理时，不得结束 Issue、标记 `blocked` 或终止 Workflow。
- Agent 必须在原 Issue 说明原因、所需固定 Role 和待处理范围，并 `@mention` 当前 `Delivery Manager` 的具体成员账号；`Delivery Manager` 修正负责人或路由后，继续原 Issue。

有效 Handoff 至少包含：结果（完成、部分完成或无法继续）、产物位置、验证证据、当前 `ref`、遗留项，以及明确的下一责任人和动作。

## Delivery Manager 收口与续推

- Delivery Manager 收到有效 Handoff、Stage 完成通知或异常通知后，只做轻量完成检查：确认完成条件、必需产物、验证证据、当前 `ref` 和责任路由是否齐全，不重复执行或复核专业工作的全部细节。
- 检查通过时，完成必要的集成或状态更新，将当前 Stage Issue 收口，并在同一次 Task 中实际创建、激活或触发下一 Stage；检查不通过时，说明缺项、负责人和完成条件，直接路由返工。
- Delivery Manager 结束 Task 前，必须确认下一项工作已经被实际创建、分配或触发。只写计划、不产生下一项可执行工作，不算完成。
- Delivery Manager 自己负责的 Stage 不得停在等待自己审核的 `in_review`；完成轻量自检后直接收口并继续路由。

## `backlog` 与自动执行

- `backlog` 是 Workflow 未来工作或未满足依赖的停车位，不代表当前可执行。
- 当前 Stage 完成后，Delivery Manager 必须检查下一 Stage 的依赖；依赖满足的 Workflow Issue 提升为 `todo` 并触发负责人，依赖未满足的继续保持 `backlog`。
- 不得因为启用本 Workflow 就把普通 `backlog` Issue 全部自动执行；只有带有本 Workflow 路由且完成条件已满足的 Issue 才能自动提升。

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
- 甲方只有明确需要人类输入、权限、资源或外部决策且当前目标无法继续时，才将当前 Issue 设为 `blocked`；等待其他 Agent 时不得以此终止 Workflow，应登记依赖并继续可执行工作。
- 乙方寻找当前需求是否正在被解决：先按当前 Issue 的 `parent_issue_id` 和 `project_id` 定位 Change 根 Issue，再依次检查：该 Change 根 Issue 下是否已有解决同一问题的子 Issue；整个项目下是否已有正在处理同一问题的 Issue。确认没有对应 Issue 后，则自行新建 Issue 解决该需求。
- 无论复用还是新建依赖 Issue，乙方都必须在该 Issue 中追加依赖登记：甲方 Issue、甲方 Agent、依赖内容和完成条件；然后在甲方 Issue 中说明应等待哪个 Issue 完成。
