# Graph AI 编排改用运行时智能体：实施计划与待办

> 设计与偏差清单见 [graph-ai-authoring-runtime-agents](../design/graph-ai-authoring-runtime-agents.md)。设计文档第 6 节的决定已拍板，见该文档的状态行。
>
> **进度**：Task 1–4（多智能体目录、专用任务类型与 daemon 提示词、`multica graph draft` 子命令与提交校验、三个异步接口与迁移 372–374）、Task 5（前端）与 Task 6（文档与内置 Skill）均已完成并提交；Task 7 真机验收**部分完成**：真实 Claude 生成通过、同一草稿跨两个智能体、取消与 daemon 离线/队列超时、并发 429 均在本机真实环境验证，但真实 Codex 因账号配额耗尽（2026-10-20 恢复）未能完成，非法输出重试耗尽只由 Go 测试覆盖。因此"验收通过后删除旧路径"仍未执行——`POST /api/graphs/generate` 与同步服务端 LLM 路径仍在，作为后备。
>
> 另需知悉的两处落地偏差：迁移范围守卫之外，`server/internal/service/graph_integration_test.go` 的私有测试 schema 迁移上界已从 371 提到 374，并新增 `TestGraphIntegrationCoversGraphMigrations` 防止下次漏加。前端错误呈现与计划略有不同：任务级与请求级失败都经 `GraphAiComposer` 的 `role="alert"` 呈现（按错误码本地化），编辑器的 `GraphError` 只负责编辑操作自身的失败，避免同一个错误渲染两次。

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

2026-10-14 在本机全新环境（库迁移至 374、服务端 8090、独立 `acceptance` profile 的 daemon 与 CLI、两个各带显式模型的智能体）执行，未配置任何 `MULTICA_LLM_*`。

1. [x] 真实 Claude 生成一次通过，同一草稿里不同节点使用不同智能体（`start → review_code(Claude) → write_tests(Codex) → end`）。
   [ ] 真实 Codex 一次**未完成**：修好 daemon 的代理后任务已到达模型，但本机唯一 Codex 账号配额耗尽，2026-10-20 恢复；失败被正确上报且未留草稿。配额恢复后需补做。
2. [x] 取消（真实在飞任务，daemon 已确认进程停止）、[x] daemon 离线与队列超时（3 分钟预算，落到 `generation_timeout`）、[x] 并发 429。
   [ ] 非法输出重试耗尽**只由 Go 测试覆盖**：用真实 Claude 诱发时智能体把指令判为提示注入并拒绝执行，改交了合法草稿。
3. [x] 抽查：提示词是固定模板、任务 `context` 只有三个字段、下发内容里智能体与审批人全是别名，`request` 无 UUID。
4. [x] 证据已补进 [V2 使用与发布说明](../graph-v2-delivery.md) 的"运行时智能体编排真机验收"小节。

**结论**：验收未全部通过，因此"删除旧路径"与"`apps/mobile` 切到运行时路径"都还没有解冻——前者按约定必须等验收通过。

---

## 待办与遗留

### 近期运维（不需要改代码）

- [ ] 如需先用起来：在线上后端环境设置 `MULTICA_LLM_API_KEY`、`MULTICA_LLM_BASE_URL`、`MULTICA_LLM_DEFAULT_MODEL` 后重启后端。单智能体的限制仍在。
- [ ] 线上第一次用 V2 前，跑一个“智能体节点加审批节点”的小流程，并在执行中取消一次，确认能很快进入 `cancelled`。
- [ ] 让需要 daemon 取消修复与 V2 执行的用户安装 v0.1.4 及以上的 CLI（下载页与安装脚本已指向本仓库的 Release）。
- [ ] **配额恢复后补做 Codex 那一半验收**：本机唯一 Codex 账号的用量限制到 2026-10-20 12:58 才恢复。恢复后重跑一次 Codex 执行者的生成，把结果补进 [V2 使用与发布说明](../graph-v2-delivery.md) 的运行时智能体编排真机验收小节。验收全部通过后，才轮到删除旧路径。
- [ ] 吊销验收中用过的凭据：GitHub PAT（权限过宽）、xingwan.store 令牌、tmlab bearer 令牌、**Codex 代理密码**（`~/.codex/.env` 里的 `HTTPS_PROXY` 内嵌 `proxyuser:<密码>`，验收期间曾被打印到终端一次，必须轮换）。

### 已知缺口（由使用者决定先不做，上线时已明确）

- 安全与护栏：HTTP 节点头部明文存储；脚本节点没有沙箱且接受自定义环境变量；没有提供方白名单；没有运行预算上限、审批超时、触发频率限制；SSRF 防护仍有缺口。
- Claude 适配使用 `--bare`，与部分 API 网关不兼容。
- Codex 在 Graph 的私有配置里丢掉了 bearer 令牌，验收时靠自定义 `CODEX_HOME` 绕过。
- Graph 运行状态的持久化方式（运行文档加子表）的进一步改造，验收时标记为后续项，尚无具体方案。
- **`apps/mobile` 仍走旧的同步服务端 LLM 路径**（2026-10-14 决定先不改）。已勘察清楚，结论如下，不必重新探查：
  - 唯一的实际缺陷是两处读 `generation.data?.enabled`——[editor-screen.tsx:291](../../apps/mobile/components/graphs/editor-screen.tsx) 的按钮闸门与 [:335](../../apps/mobile/components/graphs/editor-screen.tsx) 的文案。在没有 `MULTICA_LLM_*` 的服务器上接口返回 `{enabled:false, runtime_enabled:true}`，于是手机显示"AI generation is unavailable on this server"，**这句话是假的**：生成可用，只是不在手机上。
  - `runtimeEnabled` 已经由现有的 `GraphGenerationOptionsSchema` 解析到同一个对象上（`apps/mobile/data/api.ts` 用的就是 core 的 schema），所以**改文案不需要动数据层**。移动端没有 i18n，文案是硬编码英文。
  - 要真正切到运行时路径，需要：`data/api.ts` 加 `createGraphGeneration`/`getGraphGeneration`/`cancelGraphGeneration`、`data/queries/graphs.ts` 加 `generationJob`、`data/mutations/graphs.ts` 加两个 hook、`editor-screen.tsx` 消费终态。可复用的现成件：`graphRunOptions` 已示范"终态即停轮询"；`use-graphs-realtime.ts` 已按 `graph:*` 前缀失效整个命名空间，所以放在 `graphKeys.all(wsId)` 下的 job key 自动被覆盖。
  - 真正的缺口是**手机没有任何多选组件**（`shared.tsx` 的 `GraphPick` 只支持单值），`agent_scope` 没有 UI。因此要么做成"单选范围"（草稿只能用一个智能体，与手机现状一致），要么让模型在全部可调用智能体里分配（用户失去范围控制，与 web 语义有差异），要么新造一个多选交互。
  - 本机没有 iOS 模拟器，任何移动端改动都只能做类型检查与 lint，无法实机验证。

### 尚未验证的 V2 能力

HTTP 节点、子流程、投票审批（quorum）、触发器、重试、重启恢复、画布交互、窄屏。验收时只覆盖了 Codex 与 Claude 的代码节点流程、审批和取消。

### 环境与清理（等确认后再做）

- **2026-10-14 验收新建的本机环境仍在运行**，供配额恢复后补做 Codex 那一次：数据库 `multica`（已迁移到 374）、服务端监听 8090、CLI profile `acceptance` 的 daemon（`--workspace-id d69d25e2-2983-48eb-b488-d508514c2621`）。该 daemon 启动时导入了 `~/.codex/.env` 里的代理（Codex 需要，注意这个代理密码要轮换）。验收用的临时文件在 `D:\gotmp\multica-acceptance\`，JWT 与工作区/智能体 ID 都记在那里。
- 本地服务与 daemon、Web 预览、数据库 `multica_graph`、`multica_graph_test`、`multica_main`、`multica_main_test`。
- `D:\project\graph-data` 下的凭据与工作目录。
- 工作树 `Stein-AI-graph`、`Stein-AI-hotfix`、`Stein-AI-dist`、`Stein-AI-v2`、`Stein-AI-docs`。
- 测试仓库 `SeasonsChange422/multicaref` 及其托管分支。
- 提示：C 盘剩余空间很小，Go 构建缓存已改到 `D:\gocache`，跑 Go 测试时设置 `GOCACHE=D:/gocache GOTMPDIR=D:/gotmp`。
