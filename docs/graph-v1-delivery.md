# Graph V1 实现与验证说明

> 历史 V1 记录。当前 V2 范围、使用方式和发布验收见 [Graph V2 使用与发布说明](graph-v2-delivery.md)；本文的串行及单执行器限制不代表 V2。

日期：2026-10-13。设计依据：[Graph 模块 V1](design/graph-module-v1.md)。本文记录实现范围、兼容性检查和部署前提；代码推送不等于生产发布。

## 已实现

- Web/Desktop 侧栏 Graphs：模板、AI 草稿生成、串行流程预览与步骤编辑、完整定义 JSON 导入导出、校验、版本发布和归档。
- 固定版本、父 Issue、JSON 输入、运行时绑定和可选仓库 SHA 的串行执行。
- 运行详情、节点尝试、产物、日志、事件、暂停/恢复、有限重试、解阻、取消、人工审批及审批人替换。
- 旧 Issue 执行入口的归属保护、完成后的父 Issue 解绑、工作区删除接入。
- `multica graph` 管理命令及节点专用命令；管理写操作使用幂等键及适用的当前 revision。

节点操作示例：

```sh
multica graph node context
multica graph node comment --body "已完成第一项检查"
multica graph node submit --result-file result.json
multica graph node block --reason-file reason.json
multica attachment upload ./evidence.txt
```

`result.json` 包含 `outcome`、`output`、可选 `artifacts` 和 `diagnostics`，CLI 绑定当前尝试身份。`reason.json` 是非空诊断对象，例如 `{"reason":"缺少仓库访问凭据"}`。上传返回 `id`、`content_hash`、`size`、`media_type`，可直接填写 `attachment_ref`，并设置当前 `producer_task_id`。

## 在页面创建 Graph

1. 打开侧栏 Graph，点击创建。描述要完成的工作、步骤要求和验收标准，选择执行智能体及最终审批人，再生成草稿；也可选择手动创建。
2. 在流程预览中选择步骤，编辑智能体、执行要求和交付方式。步骤标识、重试、超时、输入映射和输出结构位于高级设置。
3. 保存草稿后进行校验，由 owner/admin 发布版本。AI 生成不会自动保存、发布或启动任务；生成失败保留当前输入和已有草稿。
4. 在已保存模板的运行入口选择父任务、输入和运行环境，启动固定版本。执行与人工审批规则保持不变。

AI 创建复用服务器已有的 LLM 配置。自托管实例在 `.env` 中设置 `MULTICA_LLM_API_KEY`，按需设置 `MULTICA_LLM_BASE_URL` 和 `MULTICA_LLM_DEFAULT_MODEL`，随后重建后端容器使配置生效；Compose 已传递这三项变量。支持无密钥的自托管兼容服务时，可只设置服务地址。未配置时页面保留手动创建，并提示 AI 暂不可用。

生成使用服务器配置的模型；页面选择的智能体是草稿中各步骤的执行者，生成后可逐步调整。仅需求文字会发送给生成模型，不发送智能体密钥、Skill 内容或运行时配置。生成接口要求工作区成员的人类身份，并检查所选智能体和审批人的访问权限。模型输出经过结构及 Graph V1 校验，分支、并行和循环仍不支持。

## 本次开放的执行范围

仅开放内置 **Codex** 适配器。Agent 必须明确配置 model；不接受自定义运行时 profile、自定义启动参数或非空 `runtime_config`。其他 provider 暂不开放 Graph。

每次尝试使用新的托管目录、会话和私有 HOME/CODEX_HOME。启动检查只允许冻结的 Skill，隔离全局 instructions、MCP、plugins、skills、memory 和模型默认配置。密钥、认证文件、代理网络和 provider 传输地址属于实时环境依赖，不承诺字节级冻结。Graph 使用平台 API 权限限制，但不是操作系统安全沙箱。

Agent Skill、显式 Agent/工作区 MCP 和适用插件 Skill 在发布时冻结；密钥只保留引用或键名，领取时解析。插件 Skill 校验 bundle 哈希，执行前复查禁用、卸载、授权、绑定及版本撤销；普通升级不改变已发布内容。**插件提供的 Remote MCP 暂不支持**，适用时明确拒绝发布，不会静默省略。

结果须经过提交、执行结束和产物交付确认。Git 校验当前输入提交、最终干净 HEAD 和本次尝试专属远程引用；验证节点必须保持候选 SHA。附件接受事务持有附件行锁，接受后阻止单独删除产物附件。

## 部署接入

1. 使用现有迁移机制应用 **332–357**；并发索引各有独立迁移，没有增加外键或级联删除。Docker / Compose / Helm 的容器启动入口，以及 `make dev` / `make start`，都会先自动运行迁移再启动 server；仅直接运行 server 二进制时需要单独执行迁移。迁移失败会阻止标准启动流程继续。
2. 一起更新 server、daemon 和 CLI。daemon 需报告 `graph-execution-v1`，CLI 需报告 `graph-node-submit-v1`；缺少能力的旧版本会被拒绝执行 Graph 或提交 Graph 结果，普通任务不受此能力门槛限制。
3. 发布模板，选择具备能力的 Codex runtime，启动运行。协调器随服务启动，从数据库恢复工作。

启动时读取的环境开关：

| 开关（设为 `true`） | 效果 |
| --- | --- |
| `MULTICA_GRAPH_DISABLE_NEW_RUNS` | 拒绝新建运行，保留已有运行控制和恢复 |
| `MULTICA_GRAPH_DISABLE_ADVANCEMENT` | 停止派发、开始及审批；继续终态、停止回执、取消和截止处理 |

关闭推进不等于杀进程。取消必须有停止证明；心跳丢失或 Task 数据库终态不能代替物理停止。存在活动或执行未知的 Graph 时，删除工作区返回冲突；其他 Graph 数据进入现有工作区删除事务。

## 原有功能兼容性

普通 Issue、任务、评论、重试、会话续接和附件沿用原有行为；新增的归属限制只作用于 Graph 管理的 Issue/Task。API 新增字段保持可选，旧 daemon 的普通任务流程不要求 Graph 能力。现有名称为 `graphs` 的工作区链接保持原来的解析规则。

推送前审查发现并修复了一个实际回归：旧任务所有权触发器已持有 Issue `KEY SHARE`，新增触发器再升级 `FOR UPDATE` 会使同一 Issue 的普通任务并发入队死锁。回归测试先稳定复现 PostgreSQL `40P01`，再将新增锁调整为兼容的 `FOR NO KEY UPDATE`，同时保留与 Graph 接管的互斥。测试使用真实旧迁移 284 的锁函数。

Linux CI 随后暴露了子任务创建和更换父任务的相关问题：旧订阅事务通过已有外键持有父任务 `KEY SHARE`，Graph 父任务保护锁会使创建流程等待，导致原有订阅并发测试超时。迁移 **357** 将这两处锁调整为 `FOR NO KEY UPDATE`，保留与 Graph 接管的互斥，同时兼容订阅事务。迁移 354 保持原样，已经应用过它的数据库可通过正常启动迁移获得修复。两个有界回归先复现阻塞，再验证修复；完整历史迁移及原 `cmd/server` 测试包在隔离 PostgreSQL 数据库通过，原订阅测试未被修改。

数据库回归覆盖两个普通任务并发入队、评论合并及登记、领取、开始、失败、重试及续接、完成、Issue 与子 Issue 编辑、普通附件删除、无 Graph Run 的工作区清理。前端全量 core 测试和相关 Issue 操作测试通过，普通 daemon 完成、失败、取消、网络重试、会话恢复和动态 Skill 流程通过。

检查未发现剩余的明确功能回归，但不是零开销保证：普通接口增加少量 Graph 归属查询，任务入队增加行锁。必须先执行迁移再启动新 server；关闭 Graph 功能开关也不能替代数据库迁移。尚未进行生产负载压测。

## 验证

创建工作台与 AI 起草的补充验证：API/core 相关 212 个测试、界面 16 个专项测试通过；core、views、Web、Desktop renderer 类型检查通过。后端 Graph handler、Graph 定义及 LLM 包测试在隔离 PostgreSQL / 模拟上游条件下通过，Go 全包编译和相关 vet 通过。实际共享组件已在 1440px、390px 及暗色模式预览中检查，覆盖生成、步骤切换、增删、保存、AI 未配置和生成失败；没有页面水平溢出或浏览器脚本错误。这里的预览采用测试数据，未调用真实付费模型，未代表生产部署验收。

已通过 Go 全包编译检查、Graph core 全部测试、Graph 及相关 Task/daemon/Codex/CLI 回归、相关包 `go vet`，以及 Web（含 Fumadocs 生成）、Desktop renderer、共享 views 类型检查和前端专项测试。

真实 PostgreSQL 17 的独立 schema 测试逐项应用 Graph 迁移，覆盖并发启动、事务回滚、暂停领取竞态、执行器重绑、迟到回执、重试预算、无效提交、截止审批、历史归属投影、附件删除竞态、工作区清理和插件授权撤销。两次独立审查发现的实际问题均补修复与回归。

验证使用假 provider、模拟产物 Git 交付和本地 PostgreSQL。未调用用户已登录的真实模型 CLI，也未实际向产物仓库推送；完整真实 provider 端到端运行仍需部署验收。

完整 Go suite 在本 Windows 环境不能标为全绿：现有 CLI profile/path 测试包含 POSIX/HOME 假设，部分会访问用户配置；service 内置 Skill frontmatter 测试对 CRLF checkout 有既有失败。没有为通过测试而放宽权限或修改用户登录配置。

推送前复核：core 125 文件、1,424 测试通过；views 4,239 测试通过，另有一个未修改文件的 Windows 路径分隔符断言失败。Web、Desktop renderer、views 类型检查通过，改动前端文件 ESLint 无错误，仅保留一条既有依赖警告。daemon/execenv 扩展检查中的 9 项 Windows 权限、junction 和文件模式失败，均在未修改的基线提交上复现。Linux CI 将继续执行全量检查，Graph PostgreSQL 测试使用隔离 schema。

首次 Linux CI 的前端构建、前端测试和 Windows 执行环境检查通过。除上述订阅并发问题外，漏洞扫描还发现原有 `golang.org/x/text v0.40.0` 命中 [GO-2026-6629](https://pkg.go.dev/vuln/GO-2026-6629)；已升级到 `v0.41.0`，并使用与 CI 相同的 Go 1.26.9 补丁版本复验 `govulncheck`，结果无可达漏洞。

## 代码位置

| 层 | 位置 |
| --- | --- |
| 定义、输入、结果、状态机 | `server/internal/graph/` |
| 持久化与协调 | `server/internal/service/graph*.go`、`server/migrations/332–357` |
| API、权限、归属与快照 | `server/internal/handler/graph*.go` |
| 执行、隔离、交付、回执 | `server/internal/daemon/graph*.go`、`execenv/graph_codex.go` |
| CLI | `server/cmd/multica/cmd_graph.go` |
| 共享数据与页面 | `packages/core/graphs/`、`packages/views/graphs/` |

运行事件、尝试时间、阻塞原因和持久操作分别保存在 Graph 数据表中。费用和日志沿用 Task 用量与日志记录；本版未新增独立 Graph 运维仪表盘。
