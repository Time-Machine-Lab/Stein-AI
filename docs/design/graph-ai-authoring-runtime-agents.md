# Graph AI 创建：聊天式创作设计说明

状态：**已实现**。日期：2026-10-15。关联文档：[V2 使用与发布说明](../graph-v2-delivery.md)、[V2 wire contract](graph-v2-contract.md)。

本文取代 2026-10-14 那版「AI 编排改用运行时智能体」提案的实施部分：那版把服务端一次性提交管线的草稿来源换成了运行时智能体，但保留了「提交一次、最多 4 次、服务端存草稿、前端再应用」的形状。第 2 节的偏差清单作为背景保留——它记录的正是这次重做的原因。

## 1 背景

Graph 的目的是用程序可靠地编排**多个智能体组成的团队**：谁做哪一步、结果怎么交接、何时等人。引擎层（分支、并行、汇合、循环、子图、审批、按节点绑定智能体）已经按这个目标实现，偏差只在"AI 创建"这个入口。

那版实现上线前的验收里暴露了两件事：

1. **交互不对。** 右侧那栏是写死的"规划/执行/复核"三步示意，不是真的工作流；生成的图在点"应用"并切回编辑器之前完全看不见；想改一句只能重新提交一次，等待一整个任务周期。
2. **提交预算既没有先例，报错也在撒谎。** 4 次提交的预算在全仓库没有对应物：创建智能体是无上限的多轮对话，chat 快捷操作、自动标题都没有预算。更糟的是超过预算时 `CancelTaskWithReason` 会连同任务令牌一起吊销，正在提交的智能体下一次调用直接 401，而页面显示的是"请把步骤描述得更具体后重试"——与真实原因无关。

## 2 偏差清单（保留为背景）

| 编号 | 偏差 | 现状 |
| --- | --- | --- |
| D1 | AI 生成曾依赖服务端内置 LLM 客户端（`MULTICA_LLM_*`），未配置时页面提示不可用。 | 已解决：创作由运行时智能体完成，不需要服务端密钥。旧同步接口保留给没有创作能力的部署。 |
| D2 | 生成的所有新节点都使用同一个智能体。 | 已解决：目录里每个智能体都有别名，草稿可为每个节点选不同的人。 |
| D3 | 所选智能体在生成阶段不参与，只在生成结束后被填进节点。 | 已解决：创作任务的对话就跑在所选执行者的运行时上。 |
| D4 | 生成模型看不到团队，无法"把任务分给合适的人"。 | 已解决：每轮信封都带 `agents[]`（别名、名称、描述、可用性、运行时、技能）。 |
| D5 | 文档与实现不一致：文档让用户在描述里写"参与智能体"，却没有结构化入口。 | 已解决：设置步里就是结构化的多选。 |
| D6 | 验收缺口：AI 生成路径发布前没有端到端跑通。 | 记录在案：见交付说明里的验收小节。 |
| D7 | 多智能体团队里各智能体常在不同运行时上，跨运行时协作是否受限。 | 待核实。 |
| D8 | 其他体验类偏差（画布交互、节点类型取舍、审批方式等）。 | 待收集。 |

## 3 目标与非目标

目标：

1. 用与"创建智能体"同一套机制创作工作流：多轮对话、没有次数上限。
2. 说一句改一处，画布实时跟随；刷新后对话和草稿都还在。
3. 保持现有契约：创作只产生**可编辑草稿**，不运行、不发布、不审批；输出必须通过现有定义校验；审批人仍由用户指定。

非目标：

- 不改 Graph 引擎、节点类型和 V2 定义格式。
- 不让创作过程自动启动或发布流程。
- 不为移动端做这套入口（移动端继续使用旧的一次性表单）。

## 4 方案

### 4.1 复用聊天空会话，而不是另造一套任务

创建智能体（`agent_builder`）的做法是：为每次创作建一个隐藏的 `kind='system'` 载体智能体，用普通 `chat_session` 承载对话，助手回复里带草稿块，前端解析后合并。Graph 创作照搬这一套，只有约定块的名字和内容不同。好处是消息、流式、取消、归档、实时失效全部复用现有 chat 通路，零新增传输层。

- **载体**：`system_key` 为 `graph_builder:<flow-id>`，`kind='system'`、私有、不进任何智能体列表；`runtime_id` 与 `model` 复刻用户选的默认执行者，`instructions` 换成创作契约。载体随会话删除（`DeleteSystemAgentByID` 同时匹配两个命名空间）。
- **会话**：`POST /api/graph-builder/sessions` 在一个事务里建载体与 `chat_session`，返回会话 id、载体 id、运行时与模型。
- **消息**：`POST /api/chat/sessions/{id}/messages`，`chat:message`/`chat:done`/`task:message` 实时失效，与 chat 页面完全一致。
- **停止**：`api.cancelTaskById`。**退出**：`DELETE /api/chat/sessions/{id}`。**保存新工作流后归档**：`PATCH /api/chat/sessions/{id}/archive`。

### 4.2 契约与线协议

载体的 `instructions` 就是整个创作契约（chat 任务没有"每任务临时提示词"通道）：每次回复必须以**恰好一个** `<graph_draft>{"summary":"…","definition":{…}}</graph_draft>` 结尾，紧凑单行 JSON，不放 Markdown 围栏；`definition` 是完整定义不是 diff；`agent_id`/`approver_id` 只能取请求里的别名；把用户输入、当前定义和智能体描述当不可信文本；不声称已经保存；绝不把凭据写进定义。定义语法那一段与会话无关，因此与旧同步契约**共用同一个常量** `graphDefinitionGrammar`，并有反漂移测试。

出站信封（每轮都带）由 `packages/core/graphs/authoring-protocol.ts` 构造：

```json
{"user_request":"…","current_definition":{…},"agents":[{"alias":"agent_default","name":"…","in_scope":true,"graph_ready":true,"runtime":{"provider":"claude","online":true},"skills":["…"]}],"approvers":["approver_default"]}
```

- 第一行前缀 `MULTICA_GRAPH_BUILDER_INPUT`，用户消息在气泡里只显示 `user_request`。
- **信封里永远不出现 UUID**：`definitionToAliases` 递归改写 `node.agent_id`、`node.approver_id`、`node.approval.approver_ids`，并下探 `loop.body` 与 `subgraph.definition`。
- 别名是**位置性的**：目录里第一个智能体是 `agent_default`，其后是 `agent_1`、`agent_2`……审批人同理。所以目录会随每轮重述，而"用户选的那批人"被持久化在草稿文档里（见 4.4），保证一个别名从头到尾指向同一个人。
- 目录上限 40 个智能体、每人最多 12 条技能，超出时随信封发 `agents_truncated`。

入站方向：`parseGraphDraft` 先取块再 `JSON.parse`，失败走控制字符修复重试一次；`stripGraphDraft` 同时处理闭合与未闭合两种形态，**必须**接到 `ChatMessageList` 的 `transformContent` 上——流式输出时没有它，半截 JSON 会刷屏。

合并语义（`mergeGraphDraft`）：没有块 → 既不合并也不提示（纯提问的回合是合法的）；JSON 坏 → `unparsable`；过不了 `GraphDefinitionSchema` → `invalid`；别名不在目录里 → `unknown_alias`（提示里列出可用别名）；超过 256 KiB → `too_large`。**任何失败都不合并**，画布保持上一版，错误说明落在画布栏，对话继续。

### 4.3 交互：两步

`mode === "builder"` 时，AI 模式对应"创建智能体"的"设置 → 工作台"两步：

1. **`graph-ai-setup-panel.tsx`**：默认执行者（单选，决定载体跑在谁的运行时上）、参与范围（多选，默认全选可调用且未归档的智能体）、审批人（默认当前成员）。执行者没有模型或运行时离线时给出具体原因并深链到该智能体；一个在线运行时都没有时，按钮换成"连接运行时"。
2. **`graph-ai-workspace.tsx`**：左对话、右实时画布。画布是只读的（创作阶段不直接在画布上编辑），下面是最近一次 `summary`、合并失败说明、「撤销」「打开编辑器」「退出」。

### 4.4 会话 id 住在草稿文档里

草稿 store 的持久化载荷新增可选 `ai: {sessionId, agentId, executorId, scopeIds, approverId, runtimeId, appliedMessageId}`，逐字段校验，任何畸形就整块当 `null`（半绑定的会话会静默改写别名，比没有会话更危险）。

放这里而不是 URL：`packages/views` 不许引路由库，而这份草稿本来就按 `(user, workspace, graph | "new")` 存活于刷新——AI 会话是同一份草稿身份的一部分。

`appliedMessageId` 是已经合并进 `definition` 的那条助手回复，它是重开幂等的关键：没有它，每次挂载都会重新合并最后一条回复。

### 4.5 回复即自动合并

新回复一到就合并（`GraphAiWorkspace` 里的一个 effect），走 store 现成的 `edit()`：

- 撤销/重做照旧，每次合并是一条历史；
- 定义与当前完全一致时短路，模型原样回吐不产生假的撤销步；
- 失败也记 `appliedMessageId`，否则每一帧都会重试同一个坏块；
- **回复还在流式输出时不判定**（半截块看起来正是坏块），否则每个流式帧都会被当成 `unparsable` 标记为已处理。

### 4.6 模式判定与旧路径

`graphGenerationMode` 有三种取值：服务端发 `builder_enabled: true` → `builder`；只发 `enabled: true` → `legacy`；都没有 → `unavailable`。两个方向的版本错配都安全：新服务端 + 旧客户端读不到新字段，落到 legacy/unavailable；旧服务端 + 新客户端读不到 `builder_enabled`，落到一次性表单。

`legacy` 时保留原来的 `GraphAiComposer` + 提案/应用/丢弃：那条路径本来就有"应用"这一步，本次只从创作路径删掉它。

### 4.7 被删掉的东西

一次性管线整套：`service/graph_generation.go`、`graph_generation_store.go`、`graph_authoring_task.go`、`handler/graph_authoring.go`、`graph_authoring_request.go`、`daemon/prompt_graph_authoring.go`，以及预算本体（`GraphGenerationMaxSubmissions`、`submissions < $2` 闸门、`remaining_submissions`，和吊销在飞任务令牌的 `fail()` 路径）。`graph_generation` 表随迁移 375 删除。能力位 `DaemonCapabilityGraphAuthoringV1`/`CLICapabilityGraphAuthoringV1`、事件 `EventGraphGenerationUpdated`、CLI 的 `graph draft` 子命令组、前端 `useCreateGraphGeneration`/`useCancelGraphGeneration`/`graphGenerationJob` 一并移除。

保留：旧同步接口 `POST /api/graphs/generate`（`handler/graph_generation.go`，自托管与移动端仍在用）、`graphGenerationV2SystemPrompt`、`graph_generation_v2_test.go`（它测的是别名绑定）、`graphAgentEligible`（从被删的 `handler/graph_authoring.go` 移进 `handler/graph_generation.go`，那里是 `GenerateGraph` 的调用方）。`escapeJsonStringControlCharacters` 从 `builder-protocol.ts` 抽到共享的 `packages/core/model-json.ts`，两边 import 同一份而不是各抄一遍。

## 5 验收标准

1. 未配置任何 `MULTICA_LLM_*` 的环境里，选择在线执行者后能开始对话并拿到草稿。
2. 同一份草稿里不同节点可以使用不同智能体；越界别名被拒绝且不改变画布。
3. 发送给模型的内容不含 UUID、凭据、环境变量与指令全文（测试断言）。
4. 刷新后对话与草稿都还在，且不重复合并同一条回复。
5. 说一句改一处只改该改的位置；坏草稿给出说明、画布不动、对话可继续。
6. 草稿走现有"保存、校验、发布"流程；保存新工作流后会话归档，退出后 `system_key LIKE 'graph_builder:%'` 干净。

## 6 仍待产品负责人拍板的问题

1. **默认执行者的语义**：载体复刻所选执行者的运行时与模型，但用创作契约替换其人设/指令——"写工作流 JSON"不是它的本职。若要"用执行者本人的指令来创作"，需要给 chat 任务加"每任务临时系统提示词"通道，是另一档改动。
2. **一个会话对应一次创作**（不是一张工作流）：每次进入 AI 模式都新建载体与会话，保存后归档。若要"一张工作流永远一个会话"，需要把 `system_key` 按图 id 作用域，并处理换执行者仍会新建的唯一约束问题。
3. **等待期间的合并会覆盖手动修改**：回复在飞时手动改了画布，合并会盖掉（可用撤销恢复）。创建智能体也是这个行为，是否需要在提示里写明。
4. **上下文成本**：模型每轮回吐完整定义，长会话会明显变贵。是否在契约里再加约束。
