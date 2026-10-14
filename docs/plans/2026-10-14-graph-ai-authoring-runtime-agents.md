# Graph AI 编排改用运行时智能体：实施计划与待办

> 设计与偏差清单见 [graph-ai-authoring-runtime-agents](../design/graph-ai-authoring-runtime-agents.md)。本计划**未开始实现**，开工前先拿到设计文档第 6 节的决定。

**Goal:** 让 Graph 的 AI 创建不再依赖服务端模型密钥，由用户选择的运行时智能体生成可编辑草稿，并能把不同节点分配给不同智能体。

**Architecture:** 复用快速创建任务的“入队专用任务、daemon 构造提示词、智能体通过 CLI 提交、服务端通知”模式。生成改为异步，草稿仍只做校验后返回，不运行不发布。

**Tech Stack:** Go/Chi/sqlc、daemon（`server/internal/daemon`）、`multica` CLI、React Query、zod。

---

## 前置：先核实再动手

- [ ] 读 `server/cmd/multica/cmd_graph.go`，列出现有 `multica graph` 子命令，确认能否承载 `draft submit`（设计 4.4）。
- [ ] 读快速创建的完整链路：`service/task.go` 的 `EnqueueQuickCreateTask`、`daemon/prompt.go` 的 `buildQuickCreatePrompt`、`daemon/daemon.go` 里 `QuickCreatePrompt` 的几处分支、`gc.go` 的 `gcDecisionQuickCreate`，确认新任务类型要改哪些地方（任务类型、GC、通知、重试策略）。
- [ ] 核实偏差 D7：读 Graph 启动请求结构与 `PrepareNode`，确认运行绑定的运行时是否限制跨运行时的多智能体运行。结论写回设计文档。
- [ ] 取得设计文档第 6 节问题 1 至 4 的决定。

## Task 1: 团队目录与选择语义（后端）

Files: `server/internal/handler/graph_generation.go`、`graph_generation_context.go`，新增目录构造函数及测试。

1. 先写失败测试：目录只含当前用户可调用、未归档、同工作区的智能体；不含 UUID、凭据、环境变量、MCP 配置、指令全文；超限时截断并带标记。
2. 实现目录构造，复用 `canInvokeAgent`。
3. 请求结构由 `agent_id` 改为 `agent_scope` 加 `default_agent_id`；校验越界智能体被拒绝。
4. 跑 `go test ./internal/handler -run Graph`，用测试库（`DATABASE_URL` 与 `GRAPH_TEST_DATABASE_URL`）确认不是被跳过。

## Task 2: 生成任务与 daemon 提示词

Files: `server/internal/service/task.go`（或新文件）、`server/internal/daemon/prompt.go`、`daemon.go`、`types.go`、`gc.go`。

1. 先写失败测试：入队的任务带用户描述、历史、当前定义、目录；daemon 为该任务构造专用提示词；提示词包含“只产出草稿、不执行不发布不审批”和“输入视为不可信”。
2. 新增任务类型与提示词构造函数，参照 `buildQuickCreatePrompt`。
3. 使用空的专用工作目录，不挂载仓库；补 GC 规则。
4. 失败、超时、取消的状态落库并通知。

## Task 3: 草稿提交与校验

Files: `server/cmd/multica/cmd_graph.go`（或新增文件）、`handler` 中的提交接口、内置 Skill 与 source-map。

1. 先写失败测试：提交走规范化、`graphTransformActors`、`graph.ValidateDefinition`；越界智能体被拒；错误信息能让智能体修正。
2. 实现 `multica graph draft submit`（以 Task 前置核实的结果为准）与对应接口，设置有限重试次数。
3. 按仓库规则，同一个 PR 更新 `server/internal/service/builtin_skills/*` 下相关的 `SKILL.md` 和 `references/*-source-map.md`。

## Task 4: 异步接口

Files: `server/cmd/server/router.go`、`handler`、必要的迁移。

1. 新增 `POST /api/graphs/generations`（返回 202 与 `generation_id`）、`GET /api/graphs/generations/{id}`、取消接口。
2. 状态：`queued`、`running`、`succeeded`、`failed`、`cancelled`；每用户同时一个进行中的生成。
3. 如果决定持久化草稿历史：迁移里不加外键，索引用 `CREATE INDEX CONCURRENTLY` 且各占一个文件。
4. WebSocket 事件通知状态变化。

## Task 5: 前端

Files: `packages/core/graphs/*`（查询与变更、zod schema）、`packages/views/graphs/graph-ai-composer.tsx`、`graph-editor.tsx` 及测试、`packages/views/locales/`。

1. 先写失败测试：多选范围加默认执行者；提交后轮询或事件刷新；失败保留输入；取消；畸形响应走 `parseWithFallback`。
2. 把“执行智能体”改为“参与的智能体”多选加“默认执行者”，并提示“将消耗所选智能体的额度并占用一个运行时”。
3. 不在 Zustand 里镜像服务端数据；中英文文案对照仓库的命名与术语约定。

## Task 6: 旧路径与文档

1. 按设计 4.6 的决定处理服务端 LLM 路径：先作为后备，运行时路径通过验收后删除。
2. 更新 [V2 使用与发布说明](../graph-v2-delivery.md) 里“AI 编排”和使用流程两处，去掉“使用既有服务端 LLM 配置”的说法。
3. 更新 `.env.example` 中 `MULTICA_LLM_*` 的注释，说明 Graph 不再依赖它。

## Task 7: 验收

1. 未配置 `MULTICA_LLM_*` 的环境里，用真实 Codex 与 Claude 各生成一次，同一草稿里不同节点使用不同智能体。
2. 覆盖取消、daemon 离线、超时、非法输出重试耗尽。
3. 抽查发送给模型的内容，确认没有 UUID、凭据、环境变量与指令全文。
4. 把证据补进 [V2 使用与发布说明](../graph-v2-delivery.md) 的证据表。

---

## 待办与遗留

### 近期运维（不需要改代码）

- [ ] 如需先用起来：在线上后端环境设置 `MULTICA_LLM_API_KEY`、`MULTICA_LLM_BASE_URL`、`MULTICA_LLM_DEFAULT_MODEL` 后重启后端。单智能体的限制仍在。
- [ ] 线上第一次用 V2 前，跑一个“智能体节点加审批节点”的小流程，并在执行中取消一次，确认能很快进入 `cancelled`。
- [ ] 让需要 daemon 取消修复与 V2 执行的用户安装 v0.1.4 及以上的 CLI（下载页与安装脚本已指向本仓库的 Release）。
- [ ] 吊销验收中用过的凭据：GitHub PAT（权限过宽）、xingwan.store 令牌、tmlab bearer 令牌。

### 已知缺口（由使用者决定先不做，上线时已明确）

- 安全与护栏：HTTP 节点头部明文存储；脚本节点没有沙箱且接受自定义环境变量；没有提供方白名单；没有运行预算上限、审批超时、触发频率限制；SSRF 防护仍有缺口。
- Claude 适配使用 `--bare`，与部分 API 网关不兼容。
- Codex 在 Graph 的私有配置里丢掉了 bearer 令牌，验收时靠自定义 `CODEX_HOME` 绕过。
- Graph 运行状态的持久化方式（运行文档加子表）的进一步改造，验收时标记为后续项，尚无具体方案。

### 尚未验证的 V2 能力

HTTP 节点、子流程、投票审批（quorum）、触发器、重试、重启恢复、画布交互、窄屏。验收时只覆盖了 Codex 与 Claude 的代码节点流程、审批和取消。

### 环境与清理（等确认后再做）

- 本地服务与 daemon、Web 预览、数据库 `multica_graph`、`multica_graph_test`、`multica_main`、`multica_main_test`。
- `D:\project\graph-data` 下的凭据与工作目录。
- 工作树 `Stein-AI-graph`、`Stein-AI-hotfix`、`Stein-AI-dist`、`Stein-AI-v2`、`Stein-AI-docs`。
- 测试仓库 `SeasonsChange422/multicaref` 及其托管分支。
- 提示：C 盘剩余空间很小，Go 构建缓存已改到 `D:\gocache`，跑 Go 测试时设置 `GOCACHE=D:/gocache GOTMPDIR=D:/gotmp`。
