# Multica Graph 模块设计方案

> 本文保留 V1 设计基线。V2 的分支、并行、循环与扩展节点契约见 [Graph V2 wire contract](graph-v2-contract.md)，当前交付状态见 [V2 使用与发布说明](../graph-v2-delivery.md)。

状态：V1 设计基线。日期：2026-10-13。代码基线：`5b47a78f6`。本次实现范围、执行环境限制和验证结果见 [实现与验证说明](../graph-v1-delivery.md)。

Graph 是 Multica 的执行编排模块。它将已发布的流程转换为可追踪、可恢复的节点执行，复用现有智能体、Task 队列和守护进程。流程定义何时开始工作、接受哪些结果、何时等待人类；智能体在节点限定的目标内自主执行。

第一版交付固定版本的串行流程，包含 Agent 节点、末尾人工验收、结构化结果、有限技术重试、暂停、阻塞恢复和取消。编辑采用步骤表单和定义导入，运行采用步骤时间线。后续再加入条件分支、并行汇合、有限返工和画布。

本文描述设计契约；实际接口和运行时支持以实现与验证说明及代码为准。本次先开放内置 Codex 执行适配器，插件 Remote MCP 尚未开放。

## 1 目标与成功标准

目标是让多智能体协作的交接由程序可靠执行，同时保留智能体在节点内部使用工具、读取源码、修改文件和验证结果的能力。

第一版的成功标准是：前置节点未通过时后继不启动；一个节点的技术重试始终属于同一次业务执行；断线、重复回调和服务器重启不会漏推进或重复创建同一次尝试；只有针对当前产物的有效人工审批能使流程成功。

Graph 保证平台内的派发、结果接纳和状态转换。它不保证模型输出逐字可重现，也不将结构化的“测试通过”声明等同于真实业务正确。业务正确性通过独立验证节点和人工验收建立。执行机器上的 shell、外部工具和已有凭据仍受现有执行环境约束；Graph 本身不是操作系统沙箱。

## 2 第一版范围

| 领域 | 第一版纳入 | 后续版本 |
| --- | --- | --- |
| 流程拓扑 | 一个 Start，1 至 16 个串行 Agent 节点，一个末尾 Approval，一个 End | Switch、Parallel、Join、循环、子图、动态增删节点 |
| 定义 | 工作区内模板，串行流程预览与步骤配置，JSON 导入导出，校验，发布不可变版本 | 自由拖拽 DAG 画布 |
| AI 辅助编排 | 页面描述需求生成可编辑草稿，也可导入外部 AI 生成的定义；走相同校验和发布流程 | 运行中 AI 改图、自动发布 |
| 执行者 | 每个 Agent 节点直接绑定一个智能体；启动时确定运行时 | 按角色动态选人、自动迁移运行时、Squad 节点 |
| 运行 | 人工启动，单父任务同时最多一个非终态 GraphRun | 自动化、定时计划、Webhook 启动 Graph |
| 重试 | 输入不变的技术重试，默认最多 3 次 Task 尝试，可在 1 至 3 次内配置 | 原地业务返工、多节点回退、检查点分叉 |
| 人工控制 | 暂停后续调度、恢复、解除阻塞、有限重试、取消、批准、拒绝 | 在线修改输入、任意跳过节点、强制成功 |
| 工作目录 | 每次尝试使用独立托管目录和新会话 | 本地原目录、local worktree、本地未提交修改、目录接续 |
| 代码输入 | 每个运行可选一个仓库，使用远程可获取的固定提交 | 多仓库、Git bundle、本地路径交接 |
| 产物 | 结构化 JSON、Git 提交、基于现有存储的附件 | 通用外部产物连接器 |
| 产品界面 | Web 和 Desktop 共用编辑、运行、结果、审批页面 | Mobile 专用 Graph 页面 |
| 交付动作 | 人工批准候选产物，完成流程 | 自动合并 PR、部署、发布、外部副作用补偿 |

第一版必须对不支持的拓扑和执行模式返回校验错误，不能静默转成串行执行。未关联 Graph 的普通任务沿用现有产品语义。

## 3 第一张验收流程

```mermaid
flowchart LR
    S[开始并冻结输入] --> D[实现节点]
    D --> V[独立验证节点]
    V --> A[人工验收]
    A --> E[流程成功]
```

每条前进边只在当前节点被引擎接受后生效。实现节点交付候选提交和说明；验证节点检查同一个候选提交并交付证据；指定成员针对候选提交、验证结果和输入摘要审批。

Agent 节点统一返回以下结果：

| outcome | Graph 行为 |
| --- | --- |
| `pass` | 完成格式、产物和版本校验后接受输出，继续下一节点 |
| `environment_blocked` | 保存诊断并阻塞；由人处理环境后请求重试 |
| `needs_changes` | 当前运行以 `rejected` 结束；不自动回跳 |

闸门节点的结论属于 `output`，不属于 `outcome`。输出被后续条件、分支或循环读取的节点（走查、评分、验收）是在报告判断结果，不是在自己的执行上失败：它交 `pass` 并把结论写进输出，由读取它的条件决定下一步——循环就是靠这个决定要不要再来一轮。交 `needs_changes` 会让节点被拒绝、整条运行当场以 `rejected` 结束，循环的迭代预算一次都用不上，运行也无法恢复。`needs_changes` 留给「必须由人改定义、改需求或改实现」的运行，以及确实无法产出输出 schema 接受的输出的节点；`environment_blocked` 仍用于环境阻塞。执行侧的说法（内置技能与 daemon 提示）与创作契约的 `graphDefinitionGrammar` 都写明了这条规则。

人工拒绝同样使运行进入 `rejected`。返工创建新的 GraphRun，记录 `previous_run_id`，显式选择新基准和反馈输入，重新执行整条链。第一版不复用上一运行的节点成功状态和审批；上一运行的产物可以成为新运行的输入。

## 4 对象关系与状态所有权

| 对象 | 职责 | 第一版关系 |
| --- | --- | --- |
| Issue | 人类工作目标、讨论、展示和交付入口 | 一个父 Issue 关联历次 GraphRun |
| GraphDefinition | 可编辑的模板身份和草稿 | 属于一个工作区，具有多个发布版本 |
| GraphVersion | 不可变的流程和执行契约 | 一个 GraphRun 只绑定一个版本 |
| GraphRun | 一次完整流程，固定启动输入和授权主体 | 一个父 Issue 最多一个非终态运行 |
| NodeRun | 某节点在本次运行中的一次业务激活 | 第一版每个定义节点对应一个 NodeRun |
| Task | 智能体的一次实际执行尝试 | Agent NodeRun 对应 0 至 N 个 Task |
| Submission | 某次 Task 提交的不可变候选结果 | 只有被终态回调引用且验收通过的记录可成为输出 |
| Artifact | 可获取、可核对版本的产物引用 | 记录生产尝试和内容标识 |
| Approval | 人对固定候选的决定 | 绑定候选摘要和当前审批请求 |

每个 Agent 节点创建一个独立子 Issue。同一 NodeRun 的重试沿用该子 Issue；新 GraphRun 创建新的节点子 Issue。Start、Approval、End 不创建子 Issue。父 Issue 原有的普通子 Issue 不自动成为图节点。

该选择适配现有按 `issue_id + agent_id` 限制待派发 Task 唯一性的实现，也避免多个步骤争用同一 Issue 的会话恢复上下文。Task 仍不是工作完成的判定者：GraphRun 和 NodeRun 拥有编排状态，Issue 状态只是它们的产品展示。

父、子 Issue 均有明确的 `execution_mode = graph`。子 Issue 关联所属 GraphRun 和 NodeRun；父 Issue 记录当前运行和历次关联。新建子 Issue 时必须原子写入执行归属，不能先按普通任务创建再补标记。

## 5 定义格式与发布

定义包含 `schema_version`、名称、输入契约、节点、顺序边、输出契约、预算、审批人和所需协议能力。节点使用稳定的 `node_key`；标题可调整，身份不依赖标题。

Agent 节点的必要配置如下。

| 配置 | 契约 |
| --- | --- |
| 执行者 | 工作区内明确的 `agent_id`，不使用自由文本角色匹配 |
| 工作说明 | 目标、范围、验收要求；Graph 指令明确禁止自行派发后继 |
| 输入映射 | 运行输入或已经接受的上游字段；不支持运行任意代码的表达式 |
| 输出契约 | 有界的 JSON Schema、固定 outcome，以及需要的产物类型 |
| 指令与 skill | 固定内容或可校验的不可变 bundle 引用 |
| 代码策略 | 无仓库、产出新提交，或只验证输入候选；每次尝试均为新会话 |
| 预算 | 最大尝试数、单次超时、排队期限；运行可另设总截止时间 |

发布校验至少检查：唯一入口出口、串行连通性、节点数量、无环且无分叉、Approval 位于末尾、引用存在、输入不指向未来节点、上下游字段和类型可解析、输出大小限制、审批人存在、执行模式受支持。Agent 权限和协议能力在发布时预检，执行时仍重新校验。

第一版输入表达式限于声明的字段引用、常量和简单对象组装；不支持任意脚本、自定义 reducer 或用户上传代码在服务端执行。JSON Schema 限定支持的关键字，不解析远程 schema 引用，并限制深度与大小。

发布在同一事务中锁定草稿修订号、保存规范化定义及内容哈希、生成单调递增版本号。修改已发布版本必须生成新版本；已绑定运行不随草稿变化。草稿保存使用修订号避免多人覆盖。

## 6 冻结哪些内容

2026-10-17 修复：运行启动时冻结父任务正文附件的 ID、文件名、媒体类型和大小，
通过现有认证下载 API 获取有效地址，不将签名 URL 固定到快照。正文之外的评论附件、
后来上传的附件和其他任务附件不会自动加入输入；被运行引用的输入附件受删除保护。

文件型实现使用 `execution_mode: produce_files`，通过
`multica graph node deliver <directory>` 上传带路径、大小、逐文件哈希清单的 ZIP。
服务端核验当前 Task 上传收据及包内容；缺少已验证文件包时拒绝 pass，不能用文字、
本机目录或 localhost 地址代替交付。普通文字节点保持 `none`，Git 交付模式保留。

已接受的祖先产物随下游 NodeSpec 冻结；节点使用
`multica graph node download-inputs -o ./graph-inputs` 下载原图和校验解包后的文件，
在自己的工作目录启动预览服务，再执行页面检查和截图。普通 ZIP 输入按文件保存，
只有 `bundle_format: multica-graph-files-v1` 且通过服务端校验的产物才按交付包解开。
包限制为 2048 文件、20 MiB 压缩体积、32 MiB 解压体积；拒绝链接、跨目录路径、
Windows 不兼容路径冲突和明确的凭据文件路径。

该协议要求所有执行节点的 daemon 广告 `graph-file-delivery-v1`；旧版本在运行启动前
明确提示更新，避免旧 CLI 执行不了下载命令或无法验证新增 manifest 字段。

| 时点 | 固定内容 | 仍按当前状态检查 |
| --- | --- | --- |
| GraphVersion 发布 | 拓扑、Agent 绑定、平台管理的 Agent 指令与模型配置、skill bundle、输入输出契约、审批规则、协议版本 | 引用是否仍存在、成员资格、调用权限 |
| GraphRun 启动 | 父任务需求快照、显式选入的评论和附件、项目及工作区注入文本、仓库和基准 SHA、运行发起人、选定运行时、截止时间 | 凭据有效性、运行时在线状态、权限撤销 |
| NodeRun 激活 | 指定上游已接受输出及摘要、输入映射结果、不可变节点执行规格、业务输入哈希 | 当前尝试的执行资格和预算 |

NodeRun 保存跨尝试不变的业务输入和节点执行规格。每次创建 Task 时单独生成不可变 attempt manifest，引用该规格并绑定 GraphRun、NodeRun、Task、执行代次和输入哈希；历史 manifest 不被重试覆盖。业务输入哈希不包含 Task ID，因此同一节点各次重试可核对输入一致；attempt manifest 自身另有内容哈希。

固定的 Agent 配置包括指令、模型、custom args、非秘密环境配置、runtime config、MCP 和工具允许范围及凭据引用。节点重试不重新读取最新 Issue 评论、Agent 配置或 skill 内容；执行时只在既定工具范围内重新解析有效凭据和短期令牌，并复查授权。

快照保存凭据引用，不保存明文密钥。平台内置 Graph 指令和提示模板也需要协议版本或内容哈希；执行记录保存守护进程、CLI 和 provider 版本。这里只冻结平台控制的输入，无法据此承诺操作系统、外部服务或模型的完全可重现性。

skill 下载必须按 manifest 中的精确哈希解析。原版本缺失时阻塞，不能替换成最新内容。可复用现有 plugin execution manifest 的不可变 artifact 和 bundle 校验模式。

## 7 启动与任务接管

启动前，服务端以真实人类发起人的身份检查父任务编辑权限、工作区成员资格、每个 Agent 的调用权限、审批人资格、运行时能力，以及仓库资源访问条件。归因字段中的负责人不能替代实际授权主体。

起跑事务锁定父 Issue，检查没有其他非终态 GraphRun，也没有旧的非终态 Task。检查必须包括 `deferred`、`queued`、`dispatched`、`running` 和 `waiting_local_directory`；不能直接使用当前遗漏 `deferred` 的活动任务查询。仍关联进行中 AutopilotRun 的父任务不允许接管。

V1 只允许接管顶层父 Issue；如果它的既有普通子 Issue 仍有非终态执行，也拒绝起跑，要求先处理这些工作。接管时不自动取消旧执行。所有与接管竞争的旧入队操作都必须在事务内重新检查父、子任务的执行归属，避免检查后又插入普通 Task。

同一事务保存执行归属、运行快照、节点记录、子任务和首个调度意图。新的运行通过显式 start 命令启动，不由父任务 assignee 或 status 变化触发。

普通评论继续保存为讨论，但不改变运行输入，不合并到待派发 Task，不追加到活动 Agent 会话，也不触发 @mention。调整需求时先取消或结束当前运行，再基于新输入启动；暂停期间也不能修改快照。

## 8 一次节点执行的完整链路

1. 协调器读取已提交状态，确认前置节点成功、运行允许调度、预算和权限有效，创建该 NodeRun 的唯一 Task 尝试。
2. 守护进程领取 Task；服务端再次检查 Graph 资格和协议能力。准备完成后，启动实际 Agent 之前再检查 start 资格，关闭暂停或取消期间的竞争窗口。
3. 守护进程从 manifest 生成独立运行指令、安装固定 skill、准备托管工作目录和指定代码提交。不得进入普通 Issue 的历史会话、目录复用和评论读取流程。
4. Agent 通过 Graph CLI 读取固定输入，完成工作，上传证据，并暂存结构化结果。此时流程不推进。
5. Agent 进程结束后，守护进程在有限交付期限内执行适用的 Graph finalizer，并上报明确的 Task 终态、进程退出证据、最终 submission ID、输入哈希和交付状态。provider 成功但交付失败仍将 Task 记为 `completed`，由 Graph 将节点阻塞；不能无限留在 running。
6. 协调器检查 Task 终态、当前执行身份、输出契约、产物引用和 outcome。全部满足后，原子接受输出并释放后继节点；不满足则按明确原因阻塞、拒绝或失败。

Submission 可以追加修订，每条记录不可变。重复请求以幂等键和内容哈希返回同一记录；同键不同内容返回冲突。完成回调明确引用本次 Task 的最终 submission ID，不能按“节点最新结果”猜测。终态提交后不得替换最终引用。

每个提交和完成回调必须绑定当前 Task、NodeRun、运行、执行代次和输入哈希。取消、过期或已经被替代的尝试不能产生可接受输出。业务结果接纳与物理执行对账分开：迟到输出不推进图，但可信守护进程对原 Task、执行代次和执行器实例的退出或停止确认仍持久化，用于完成取消或解除 execution_unknown；不能覆盖另一个尝试的状态。Task 成功但没有合法提交不是业务成功，进入 `output_invalid` 阻塞。

Agent 报告 `environment_blocked` 或 `needs_changes` 时，允许只交诊断和已存在的部分产物，不要求满足 `pass` 的全部产物字段；它们永不释放成功边。Task 本身失败时，已暂存结果只供诊断。

## 9 状态与人工控制

GraphRun 的生命周期为 `active`、`waiting_approval`、`blocked`、`succeeded`、`rejected`、`failed`、`cancelled`。另存 `control_state = run / pause / cancel_requested`，避免暂停覆盖正在审批或阻塞的原状态。运行未终结时，界面按两者组合显示“暂停中”“已暂停”“取消中”。

`cancel_requested` 不只是人工取消：运行以任何非成功的终态结束时，引擎也用它停下推进（`graphTerminateV2`），随后一直留在运行上。终态运行的 `control_state` 因此**不予显示**——终态本身就是全部信息，否则一条 `rejected` 运行会永远显示“正在取消”，而页面上已没有任何能把它取消掉的入口。

NodeRun 的状态为 `pending`、`ready`、`active`、`validating`、`waiting_approval`、`blocked`、`succeeded`、`rejected`、`failed`、`cancelled`。Task 保留现有物理执行状态；NodeRun 不复制 queued/dispatched/running 的全部细分状态。

| 操作或事件 | 精确语义 |
| --- | --- |
| 暂停 | 禁止新建尝试、claim、重新派发和 start；已确认 running 的尝试可以完成并保存结果。可验收当前输出，但不启动后继、不提交人工审批，也不沿成功路径结束运行；当前执行的拒绝、不可恢复失败、取消和 deadline 仍可使运行终结 |
| 暂停时已领取但未启动 | 返回明确的 hold 原因；停止准备，保留同一 Task 身份并回到可恢复队列，不算执行失败、不消费新尝试 |
| 恢复 | 只解除 pause；重查权限与预算，处理持久调度意图，不重新执行已经成功的节点 |
| 解除阻塞 | 人声明环境已修复，引擎重新检查对应条件；无执行的权限/资源阻塞可继续，已结束但需重新执行的节点转为一次受预算约束的显式 retry |
| retry | 旧尝试已确认结束、输出未被节点接纳、输入不变且总预算未耗尽时创建新 Task；可接受旧 Task 为 failed，或 completed 但输出无效、环境受阻、交付失败。新会话、新目录，不覆盖旧记录 |
| 修改输入或业务返工 | 结束旧运行，创建新 GraphRun；不通过 resume 或 retry 修改输入 |
| 取消 | 立即关闭调度与结果接纳资格，取消排队尝试，通知活动尝试停止；确认所有活动执行结束后才写 `cancelled` |
| 执行失联 | 使用 `execution_unknown` 阻塞原因，等待进程结束或可靠隔离的证据；心跳过期不能证明外部进程已停止 |
| 管理员停止某个 Agent 或单独取消 Task | 关闭该尝试的业务结果接纳，NodeRun 以 `external_interrupt` 阻塞；确认进程停止后可显式 retry。仅 Graph 取消命令表示取消整个运行 |
| 审批期间暂停 | 审批内容仍可查看，批准和拒绝暂不提交；恢复后审批同一个摘要 |
| 到达运行截止时间 | 设置 cancel_requested 并记录 termination_reason 为 deadline，关闭新调度，停止仍活动的执行；确认停止后以 `failed` 结束 |

`succeeded/rejected/failed/cancelled` 是不可恢复的运行终态。终态后重新工作创建新运行。`cancel_requested` 和 `execution_unknown` 都仍占用父 Issue 的活动运行名额，不能通过开启新运行绕过旧执行未停止的问题。

“暂停”明确表示暂停后续调度；界面显示当前 Agent 是否仍在运行，并提供取消操作。V1 不提供冻结正在执行的模型进程后原地继续的能力。

新增可持久化的执行停止回执，至少绑定 Task、执行代次、守护进程实例和停止时间。数据库中的 Task cancelled/failed 状态、租约到期或失联本身都不是停止证据。V1 不提供忽略未知执行的强制恢复按钮；执行器重新连线并确认原进程已结束后才允许重试或完成取消。

## 10 技术重试与持久恢复

重试只有一个策略所有者。Graph 任务的重试决定由 Graph 协调器作出；复用 TaskService 的失败分类、退避算法、队列写入和 retry lineage。普通 Task 保持现有自动重试策略。

Graph Task 不再由 `FailTask` 或 `MaybeRetryFailedTask` 无条件创建 retry child。它们必须记录失败与待处理原因，由 Graph 的资格检查决定是否创建下一尝试。现有状态回收、stale claim、人工 rerun 和 orphan 恢复也不能绕过此检查。

每个 NodeRun 最多存在一个未结束的执行尝试；输入变化、权限撤销、暂停、取消、运行终态、预算耗尽或执行未知都阻止新尝试。默认最多 3 次 Task 尝试，创建尝试时占用预算，hold 后重新领取同一个 Task 不再占用；任何失败分类都不能扩大已发布的上限。Graph 模式禁用守护进程或适配层在同一个 Task 内透明重启整轮 Agent 的行为，整轮重跑统一创建新尝试；同一会话中的 API 传输重发不算新的 Agent 执行。

Graph retry 的事务适配需要支持已结束但未被节点接纳的 completed Task，不能照搬普通失败重试的前置条件。新 Task 重建自身的 attempt manifest 和身份绑定，只复用节点规格与固定输入；不能直接复制旧 manifest 中的 Task ID。

重试只自动处理明确终止且被允许的技术故障。结构化输出无效、Git 交付失败和环境阻塞需要显式修复或重试，不通过自动重复 Agent 工作来掩盖协议问题。预算耗尽后失败；人为 retry 也不能无限扩大该预算。

采用数据库中的状态、操作意图和事务作为恢复基础，Event Bus 只用于降低响应延迟：

| 事务边界 | 必须共同提交的内容 |
| --- | --- |
| 启动 | 执行归属、GraphRun、NodeRun、子 Issue、首个调度意图 |
| 创建尝试 | 运行资格检查、尝试序号、Task、manifest 绑定、操作消费标记 |
| Task 终态 | Task 成功或失败、最终 submission 引用、可重放的 Graph 对账意图 |
| 接受节点输出 | 当前尝试确认、验收结果、NodeRun 成功、后继激活或审批请求、审计事件 |
| 人工审批 | 审批人资格、候选摘要、决定、运行终态、父 Issue 状态投影 |
| 控制命令 | 预期修订号、pause/cancel 标记、所需取消或恢复意图、审计记录 |

多个服务实例可运行协调器，通过行锁或修订号 CAS 串行处理同一个运行；调度意图有唯一 operation key，Task 有唯一 `(node_run_id, graph_attempt_no)`。锁顺序必须与现有 claim 的运行时容量锁统一，不能在不同路径反向取得 run、node、task 和 runtime 锁。

启动时和周期任务扫描非终态运行、待消费操作和未对账 Task。完成事务后通知丢失可通过扫描恢复；Task 已创建但唤醒失败只补唤醒；重复终态回调不重复接受结果。无需实现完整事件溯源或通用分布式工作流平台。

该机制保证单次状态接纳和幂等派发，不承诺 Git、外部 API 等副作用严格只发生一次。取消和新运行不会回滚已经发生的外部行为。

## 11 工作目录与产物协议

每次 Graph Task 使用全新的托管环境和 Agent 会话。第一版排除 `local_directory`、`in_place`、`local_worktree` 和自动带入本地未提交改动。这样基准不随宿主机用户操作漂移，也不继承旧节点的隐藏聊天历史。

有仓库时，守护进程在启动 Agent 前通过现有 repo cache 检出 manifest 指定的 SHA，并验证实际 HEAD。CLI 的显式 `--ref` 不能覆盖 Graph 允许的仓库和输入版本。`graph node context` 返回原始需求快照和声明的上游结果，普通 Issue 工作流提示不注入。

代码节点分两种模式：`produce_commit` 允许从固定输入提交生成候选；`verify_commit` 要求退出时 HEAD 仍为输入候选且代码区干净，只交验证报告。验证证据绑定 `tested_commit_sha`，引擎检查它与候选一致；报告内容的真实性由独立验证过程和人类验收判断。

对 `pass` 且需要新 Git 产物的节点，代码交付顺序为：Agent 显式提交代码并 submit 声明 SHA → provider 正常退出 → 清理平台辅助文件 → 守护进程验证 HEAD 和代码区无未提交修改 → 使用本次运行被授权的 Git 凭据发布受管 ref → 验证远程 ref 指向声明 SHA → 上报交付回执。平台不自动把剩余代码改动提交成另一个 SHA。合法的 environment_blocked/needs_changes 可以不带 SHA，只记录诊断和退出证据，不强迫提交或发布失败工作的代码。

受管 ref 按运行、节点和尝试隔离，例如 `refs/heads/multica/graphs/<run>/<node>/<attempt>`。同一 ref、同一 SHA 的重试幂等；已有 ref 指向其他 SHA 时冲突，不强推覆盖。验证节点复用输入候选的已有产物引用，不生成虚假的新代码产物。

| 产物类型 | 保存内容 | 接纳条件 |
| --- | --- | --- |
| `git_commit` | 仓库身份、base SHA、commit SHA、受管 ref、生产 Task 和交付回执 | 新产物属于当前尝试，声明与实际一致，远程 ref 可核验；既有产物须属于声明输入，保留原生产者 |
| `attachment_ref` | 附件 ID、内容哈希、大小、媒体类型、生产 Task | 新附件完成上传且属于当前尝试；透传附件须属于声明输入；均检查工作区、对象存在和保留引用 |
| JSON 输出 | 内容、schema 版本和摘要 | 有界、格式有效、字段和上游引用合法 |

下游准备时还必须 fetch 并检查指定 SHA；生产方可推送不代表所有消费方权限永远有效。无法获取时阻塞，不退回最新分支、本地路径或缺失内容。无 Git 发布权限或不支持受管分支的仓库，不能运行产出代码的节点。

附件复用现有存储、对象记录和下载机制，但需要适配上传授权：当前 `task_id` 上传仅允许 chat Task，Graph 节点不能直接原样复用。扩展 task-scoped 上传以绑定当前 Graph 尝试，保存独立 Graph artifact 保留引用；下载按附件 ID 生成当前地址，不固定过期签名 URL。

当 provider 成功而 `pass` 所需的 Git 发布、附件交付或输出校验失败时，Task 终态固定为 `completed`，附带 `provider_exit=success` 与 `delivery_status=failed` 或输出错误；NodeRun 进入 blocked。provider 执行失败则 Task 为 failed，主动取消则为 cancelled。若 Task 已被回收器置为失败，迟到停止回执只补物理对账，不改写旧终态或恢复其业务输出。

finalizer 必须有有限的传输重试期限，只重传同一 SHA、附件或回调。若最终交付仍失败，V1 通过修复后受预算限制的新 Task 重试继续，不恢复旧 Agent 会话。失败尝试的部分产物留作诊断，不自动成为下游输入。合法的非 pass outcome 走自身 blocked/rejected 语义，不因缺少 pass 产物而被覆盖为交付失败。

所有被运行引用的输出和 skill 快照必须受保留策略保护。V1 提供归档，不自动删除受管 refs 或 Graph 引用的附件；后续再引入有引用检查的清理策略。

## 12 人工审批与任务展示

第一版只支持一名明确指定的审批人。进入 Approval 时创建请求，绑定 GraphVersion、运行输入摘要、候选产物摘要、所有前置节点已接受输出摘要和验证证据摘要。

批准和拒绝必须来自人类身份，即使 Task Token 内含人类 `user_id` 也不能审批。服务端同时校验当前审批人资格、审批请求仍待处理、候选摘要未变、运行未暂停或取消。重复相同决定幂等；相反决定或旧摘要返回冲突。

审批人离开工作区时运行阻塞。owner/admin 可以通过审计命令更换审批人，创建新的待审批请求并使旧请求失效，候选内容保持不变。不能通过该操作变更输入或直接批准。

父 Issue 的状态投影固定如下：运行期间为 `in_progress`，等待审批为 `in_review`，运行阻塞或执行失败为 `blocked`，审批通过为 `done`，拒绝为 `todo`，取消确认为 `cancelled`。暂停只显示独立 Graph 标识，不伪造业务完成状态。

Agent 节点子 Issue 按 NodeRun 完整投影：pending/ready 为 todo，active/validating 为 in_progress，blocked/failed 为 blocked，rejected 为 todo，cancelled 为 cancelled，succeeded 为 done。整个运行终止时，将未执行节点记为 cancelled，保留已经成功节点的 done。Approval 没有子 Issue，不参与该映射。

状态投影由引擎写入，不触发旧的 assignment、child-done 或 Squad 唤醒。PR 合并只更新关联证据，不代替 Graph 审批。自动化不会根据 Graph 的 `in_review` 提前判定本流程完成。

Graph 管理期间，普通任务更新允许标签和排序；需求、状态、负责人、项目、父子关系和 stage 不能被旧 API 改动。评论可继续讨论。V1 拒绝通过普通删除接口删除 Graph 父子任务，提供取消和归档。

父 Issue 在终态后仍保持 Graph 模式，避免旧评论意外启动新 Agent。人类可以在没有活动运行、未结束或执行未知 Task 时显式解除父任务的 Graph 管理；历史节点子 Issue 仍保留历史归属和保护。解除操作不删除运行记录。

## 13 权限与协议兼容

| 主体 | 第一版允许的操作 |
| --- | --- |
| 工作区成员 | 在现有访问权限下查看运行；创建和修改自己的草稿 |
| owner/admin | 发布和归档模板、管理草稿；更换失效审批人 |
| 运行发起人 | 有父任务编辑权且可调用全部所需 Agent 时启动；控制自己发起的运行 |
| owner/admin | 在现有资源权限内管理运行；不能自动继承他人的外部凭据 |
| 指定审批人 | 对当前候选批准或拒绝 |
| Graph Task Token | 读取本节点固定上下文、允许的输入产物、提交本次结果、上传本次证据、报告进度和阻塞 |
| 守护进程身份 | 领取授权 Task、确认实际执行生命周期、上报交付回执 |

Graph Task Token 采用明确的操作允许清单。禁止发布或启动 Graph、审批、改图、改 Issue 执行字段、旧 rerun，以及通过 issue、chat、quick-create、autopilot 等入口继续派发其他 Agent。必要的运行进度进入专用节点记录；不依靠 Agent 自己修改 Issue 状态。

所有 Graph 记录按 `workspace_id` 检查归属。服务端从可信 Token 反查 Task 和当前节点身份，不信任客户端自报的 node ID。人类控制命令验证真实身份来源，幂等键不能代替权限验证。成员资格、Agent 调用权和资源凭据每次派发、重试和审批时复查，撤权后阻塞。

新增能力标识 `graph-execution-v1` 和 `graph-node-submit-v1`。发布或启动时检查运行时元数据，实际 HTTP/WS claim 时复查实时能力，守护进程 spawn 前检查实际使用的 multica CLI。缺能力时返回可操作的升级原因，不降级成普通 Issue Task。Web/Desktop 使用有默认分支的 schema 解析；旧客户端对受保护字段的写入也由服务端拒绝。

## 14 逻辑数据设计

下表描述存储职责，字段以实施时的 sqlc 设计为准；不要求为每个 JSON 子对象单独建表。

| 拟议表 | 核心字段或内容 |
| --- | --- |
| `graph_definition` | workspace、名称、草稿内容、草稿修订号、创建者、归档时间 |
| `graph_version` | definition、版本号、规范化定义、契约哈希、不可变 bundle 引用、发布者 |
| `graph_run` | workspace、root issue、version、previous run、发起人、生命周期、控制状态、修订号、输入快照、deadline、阻塞原因 |
| `graph_node_run` | run、node key、child issue、状态、固定输入和节点执行规格、业务输入哈希、当前 Task、尝试预算、已接受 submission |
| `graph_submission` | node run、task、执行代次、输入哈希、内容、内容哈希、幂等键 |
| `graph_artifact` | run/node/task、类型、不可变内容引用、交付回执、保留信息 |
| `graph_approval` | run/node、候选摘要、审批人、请求修订、决定、理由和时间 |
| `graph_operation` | run/node、操作类型、唯一操作键、待执行时间、消费状态和最后错误 |
| `graph_run_event` | run、递增序号、操作者、事件类型、状态变更和诊断摘要 |

扩展 Issue 的执行归属及 Graph 关联字段；扩展 `agent_task_queue` 的 `graph_node_run_id`、`graph_attempt_no`、执行代次、不可变 attempt manifest 或其存储引用及哈希、最终 submission 引用、交付状态和物理停止回执。尝试明细直接使用已有 Task，不再建一套 Task 状态表。

约束至少包括：同父 Issue 最多一个非终态运行；`(run_id, node_key)` 唯一；`(node_run_id, graph_attempt_no)` 唯一；每个 NodeRun 的当前尝试通过事务维护；operation key 唯一；审批请求的当前版本唯一。终态和控制标记的组合检查不能只放前端。

遵守仓库规则：表名使用单数 snake_case，不创建数据库外键或级联操作；引用校验和依赖清理由应用事务负责。所有新增索引使用 `CREATE [UNIQUE] INDEX CONCURRENTLY`，每条 concurrent index 使用独立迁移文件，并提供对应回退迁移。

## 15 拟议 API 与 CLI

下面的接口是设计契约，路径沿现有已认证工作区 API 分组组织。管理接口要求人类身份；节点提交接口要求当前 Graph Task Token；执行终态沿用 daemon API。

| 接口组 | 拟议操作 |
| --- | --- |
| `/api/graphs` | 列表、创建草稿、读取、按修订号更新、归档 |
| `/api/graphs/{id}/validate` | 返回结构、权限预检和兼容性诊断 |
| `/api/graphs/{id}/publish` | 发布不可变版本 |
| `/api/graph-runs` | 指定父任务、版本和输入启动；查询运行 |
| `/api/graph-runs/{id}` | 详情、节点、尝试、审计事件、产物和阻塞原因 |
| `/api/graph-runs/{id}/pause`、`resume`、`cancel` | 控制调度；不隐含修改输入 |
| `/api/graph-runs/{id}/retry`、`resolve-block` | 指定受阻节点，在预算和终止证据约束下恢复 |
| `/api/graph-runs/{id}/approval` | 对指定请求和候选摘要批准或拒绝 |
| `/api/graph-runs/{id}/approver` | owner/admin 更换失效审批人，重建请求 |
| `/api/graph-runs/{id}/archive` | 归档终态运行，保留审计和产物引用 |
| `/api/issues/{id}/graph/detach` | 无活动或未知执行时解除父任务管理 |
| `/api/graph-node/context`、`submissions`、`blockers` | 从 Task Token 定位当前节点，读取固定输入、暂存结果、报告阻塞 |
| 现有附件上传 API | 增加当前 Graph Task 范围内的授权与保留关联 |

所有创建和控制命令接受幂等键；更新和审批命令带预期修订号或摘要。校验失败返回具体字段、节点和原因。使用稳定错误码区分 `graph_managed`、`run_paused`、`stale_attempt`、`input_mismatch`、`budget_exhausted`、`execution_unknown`、`capability_missing`、`artifact_unavailable`。

CLI 管理命令为 `multica graph validate/publish/run/get/pause/resume/cancel/retry/resolve-block/approve/reject`，分别映射上述人类 API，不通过修改 Issue 状态模拟。Agent 命令收敛为 `multica graph node context --output json`、`multica graph node submit --result-file <path>`、`multica graph node block --reason-file <path>`，附件上传沿用已有命令并扩展 Graph 授权。

`node block` 记录当前尝试的诊断，并可生成 `environment_blocked` 候选提交；当前 Agent 应退出。只有执行器确认结束后才进入可重新执行的阻塞状态，不能在旧进程仍运行时启动替代 Task。

## 16 界面与模块组织

第一版有三类界面：模板的步骤编辑与发布页面；运行的节点时间线、尝试记录和固定输入输出查看页面；候选产物与证据并列展示的审批页面。父任务详情增加 Graph 运行入口和状态卡片，节点子任务标明所属运行，旧 rerun/改状态动作显示受控原因。

运行视图必须能回答：当前停在哪里、哪个 Task 正在执行、尝试预算剩余多少、等待哪个人或资源、哪些结果已接受、下一步允许什么操作。复用已有 Task transcript 显示工具调用和输出，不重新实现日志浏览器。

| 拟议位置 | 职责 |
| --- | --- |
| `server/internal/graph/` | 纯模型、定义校验、输入映射、状态转换判断；不依赖 HTTP、TaskService 或运行进程 |
| `server/internal/service/graph.go` 及按职责拆分文件 | 工作区授权衔接、数据库事务、NodeRun 与 Task 适配、产物接纳、人工控制、恢复协调 |
| `server/internal/handler/graph.go` | HTTP 身份校验、请求解析、调用 GraphService |
| `server/pkg/db/queries/graph.sql` 与生成代码 | Graph 存储、锁和唯一性查询 |
| `server/cmd/multica/cmd_graph.go` | Graph 管理和当前节点命令 |
| `packages/core/graphs/` | Graph 类型、schema、查询、变更、query keys、客户端草稿状态 |
| `packages/views/graphs/` | Web/Desktop 共用模板、运行和审批页面 |

TaskService 通过一个窄的 Graph 执行资格接口检查 claim/start/retry，不反向依赖 HTTP Handler。GraphService 复用支持事务查询对象的 Task 创建能力；不要直接调用一个会自行提交并广播的普通 Enqueue 函数来模拟原子调度。

服务构造复用现有 Handler 中的 TaskService，保持 wakeup、EmptyClaim cache 和事件总线为同一套实例。Graph 协调器在服务器后台生命周期启动，正常关闭时停止扫描；多个实例通过数据库竞争处理运行。

服务端数据放 TanStack Query，草稿和视图筛选才放 Zustand；query keys 包含工作区。WebSocket 更新或失效 Query 缓存，不把运行状态复制到客户端 store。共享视图用 NavigationAdapter，Web 和 Desktop 分别补路由；Mobile V1 不提供专用编辑和控制页，已有客户端仍须遵守服务端执行边界。

## 17 与现有代码的接入点

以下位置是已核实的现有入口。实现应围绕共享规则修改这些入口，不能只在新增 Graph Handler 中做检查。

| 现有位置 | 当前行为 | Graph 接入要求 |
| --- | --- | --- |
| [IssueService.Create 与 maybeEnqueueOnAssign](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/service/issue.go:195) | 创建任务后可按负责人派发 | 新增内部 graph-owned 子任务创建路径；普通创建不能向运行中图插入节点 |
| [WillEnqueueRun](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/service/issue_trigger.go:96) | assignment 和状态变化派发，兼供 preview | Graph 归属不触发；preview 显示由 Graph 控制 |
| [UpdateIssue 与批量更新](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/handler/issue.go:2877) | 修改状态、负责人、parent、stage | 写前检查字段保护，批量预检；MoveIssue 复用同一规则 |
| [评论路由](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/handler/comment.go:2626) | mention、回复、assignee fallback、worker 唤醒 leader | Graph 任务只讨论；禁所有评论派发；QuickAction 同步处理 |
| [评论合并](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/handler/comment.go:2378) | 合并待派发 Task，修改归因和 MCP overlay，追加活动输入 | 查询排除 Graph Task，避免已冻结输入被更改 |
| [完成后评论对账](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/handler/daemon.go:3677) | 重新计算评论并补派发 | Graph Task 跳过普通评论补发流程 |
| [child-done 派发](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/handler/issue_child_done.go:586) | stage 屏障后直接唤醒父负责人 | Graph 父子不走此路径；不能把 done/cancelled 屏障当节点成功 |
| [Squad briefing](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/handler/squad_briefing.go:25) | 要求 leader 委派并管理父任务状态 | Graph 模式不注入旧协调协议，使用节点执行指令 |
| [TaskService 入队](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/service/task.go:1072) | 普通任务创建与归因 | 提取事务内创建能力；合法 NodeRun 身份、workspace 和调用授权不能省略 |
| [Task claim](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/service/task.go:2919) | 容量控制、领取、回收 stale dispatch | 加入 Graph 调度资格；覆盖 HTTP、WS、batch 和重新派发 |
| [Task start](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/service/task.go:3444) | dispatched 转 running | 原子复查 run 控制状态和执行代次；暂停返回 hold |
| [CompleteTask](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/service/task.go:3558) | Task 终态，后续广播 | 原子保存最终提交与 Graph 对账意图；不直接将 Task completed 当节点通过 |
| [FailTask 技术重试](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/service/task.go:3963) | 失败事务内创建 retry child | Graph Task 改交统一重试策略，保留分类和 lineage |
| [MaybeRetryFailedTask](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/service/task.go:4396) 与 [HandleFailedTasks](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/service/task.go:4730) | 回收失败任务、重试、重置 Issue 为 todo | Graph 进入持久对账；禁旧状态回写，执行未知不得自动替代 |
| [旧 rerun](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/handler/task_lifecycle.go:165) | 根据 Issue 或历史 Task 重跑 | Graph 身份拒绝旧路径，转专用 retry |
| [Issue 删除](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/handler/issue.go:3367) 与各 Task cancel 入口 | 删除任务或直接取消执行 | Graph 任务禁止单独删除；取消转为 Graph 命令或记录外部中断 |
| [GitHub 与 VCS 完结入口](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/handler/github.go:1894) | PR merge 直接设置 done，再通知父任务 | Graph 只接纳关联证据，不跳过审批 |
| [Autopilot 状态同步](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/service/autopilot.go:959) | in_review/done 等映射自动化终态 | V1 不接管活跃自动化任务，不新增自动启动 Graph 模式 |
| [Task Token 中间件](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/middleware/auth.go:87) 与 [Agent 调用权限](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/handler/agent_access.go:48) | 身份绑定；调用权主要由 handler 判断 | 提取共享调用授权；Graph Task 操作允许清单，当前尝试校验 |
| [buildClaimedTaskResponse](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/handler/daemon.go:1857) | 热读 Agent 配置、skill，选择旧 session/workdir | Graph 按固定 manifest 构造；不生成历史恢复指针 |
| [Skill bundle 下载](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/handler/daemon.go:3260) | 可重新读取当前 workspace skill | Graph 严格读取固定哈希；借鉴 [plugin manifest](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/service/task.go:4923) |
| [buildPromptBody](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/daemon/prompt.go:143) 与 [环境准备](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/daemon/execenv/execenv.go:327) | 普通 Issue 提示、目录和上下文注入 | Graph 分支优先；固定输入，新目录、新会话；不注入旧派发/改状态规则 |
| [repo checkout](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/daemon/health.go:270) 与 [repo cache](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/daemon/repocache/cache.go:814) | 支持指定 ref，但显式 ref 可覆盖默认值 | 启动前固定检出，Graph 校验允许的仓库与 SHA |
| [daemon 完结处理](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/daemon/daemon.go:6581) 与 [TaskCompleteRequest](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/handler/daemon.go:3476) | 清理、自动提交某些 worktree、上报终态 | 独立 Graph finalizer；校验声明 SHA，提供 submission 与产物回执 |
| [附件上传](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/handler/file.go:505) | task_id 上传限定 chat Task | 扩展当前 Graph 尝试授权，增加产物保留关联 |
| [daemon capabilities](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/daemon/client.go:183) 与 [CLI capabilities](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/cli/client.go:164) | HTTP/WS 和 CLI 分别报告能力 | 新协议能力，claim 与 spawn 前检查，禁止旧协议降级 |
| [Handler 服务构造](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/internal/handler/handler.go:375)、[router](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/cmd/server/router.go:1547)、[后台生命周期](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/cmd/server/main.go:451) | 共享服务、工作区路由和 worker | 挂载 GraphService 和管理/节点 API；复用共享 TaskService，启动恢复协调器 |
| [CLI 注册](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/server/cmd/multica/main.go:34) | 注册现有 multica 命令 | 增加 cmd_graph；同步内置平台 skill 的 CLI 和行为说明 |
| [前端 API schema](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/packages/core/api/schema.ts)、[实时同步](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/packages/core/realtime/use-realtime-sync.ts) | 解析网络数据并更新 Query | Graph schema、工作区 query keys、运行事件失效和重连全量校准 |
| [Desktop 路由](D:/DEVTOOL/project/IDEA_PROJECT/multica/Stein-AI/apps/desktop/src/renderer/src/routes.tsx) 与 Web workspace dashboard 路由 | 平台页面接入 | 两端挂同一共享 Graph 页面和父任务状态卡片 |

现有任务并非全部经过同一个 Enqueue 方法。例如 Autopilot 的 run_only 会直接创建 Task。执行归属保护必须覆盖真正的写入路径、claim 和输入更新路径，不能假设加一个入口检查即可覆盖全局。

## 18 验收与交付顺序

| 验收场景 | 必须观察到的结果 |
| --- | --- |
| 正常串行运行 | 实现、验证按序执行；审批前父任务不能 done |
| 相同 Agent 执行不同步骤 | 独立子 Issue、独立会话和目录，无待派发唯一性冲突 |
| 首次 Task 技术失败 | 新 Task 归属同一 NodeRun 和子 Issue，输入不变，次数递增 |
| 到达最大尝试数 | 没有额外自动或手动尝试；返回明确失败原因 |
| NodeRun 已选定但机器繁忙 | 保持等待，不越过节点、不误判结束 |
| 暂停与 claim/start 并发 | 暂停后不出现新的 running；已有 running 可收尾，后继不启动 |
| 暂停期间失败 | 保存 retry 意图；恢复后重查资格，不能丢失或偷偷派发 |
| 暂停时最后一次尝试失败或返回 needs_changes | 允许按失败或拒绝规则结束，不因暂停卡住终态 |
| 取消与完成回调并发 | 以同一事务资格决定接纳；取消获胜后迟到结果不能推进 |
| 取消后收到正常完成回调 | 可记录可信停止证据并完成取消，不能把业务结果当成功 |
| 守护进程失联 | execution_unknown 阻塞；旧进程未确认停止前没有替代执行 |
| Task 成功但缺提交或输出无效 | NodeRun 阻塞，不接受普通自然语言完成声明 |
| submit 后 Agent 继续改代码 | 声明 SHA 与最终工作目录不一致则不放行 |
| 上传或 Git 发布失败 | 记录可定位原因；下游不读取本地路径或缺失产物 |
| provider 成功但交付失败 | Task completed 且交付失败可见，NodeRun blocked；允许受预算限制的新尝试，不永久 running |
| 配置、skill 或评论发生变化 | 已启动运行仍使用原快照；权限撤销则阻塞 |
| 旧 mention、rerun、child-done、PR merge | 不能启动额外 Task，不能跳过 Graph 审批 |
| 跨节点、旧 Task 或伪造摘要提交 | 服务端拒绝，保留有效当前结果 |
| 旧 daemon 或 CLI | 在执行前拒绝并说明缺少的能力 |
| 并发启动同一父任务 | 只产生一个有效活动 GraphRun |
| 在各事务提交前后注入崩溃 | 重启对账后无漏推进，同一次 attempt 无重复 Task |
| 重复或过期审批 | 相同决定幂等，旧请求或不同候选不能生效 |
| 普通任务回归 | 既有 assignment、评论、Squad、自动化和 Task 生命周期保持现有语义 |

验证采用纯状态机单测、PostgreSQL 事务与竞态集成测试、带假 Agent 可执行程序的 daemon 协议测试，以及 Web/Desktop 流程测试。默认测试不调用用户真实 Agent CLI；真实模型验证应单独授权。

按三个里程碑交付第一版。第一阶段完成定义发布、归属保护、状态模型、事务内 Task 适配和持久协调器，用假执行器跑通串行和人工审批。第二阶段接入真实 daemon 协议、固定输入、产物交付、重试与取消，并完成故障注入。第三阶段补齐步骤编辑、运行时间线、审批、CLI、内置 skill 和两端页面，跑通第一张验收图。

功能开关至少分开控制新建运行与已有运行推进。关闭新建后，既有运行可继续受控完成或暂停；紧急停止必须走 Graph 取消和停止确认，不能直接恢复普通派发。只有没有活动或执行未知的 GraphRun 时，才允许移除协议支持或回退相关数据库结构。数据和产物保留用于审计。

运行观测至少包含节点等待时长、执行时长、重试次数、阻塞原因、协调器待办积压、拒绝的迟到结果和取消未确认数量。第一版以尝试上限、超时和截止时间控制执行规模；费用统计使用现有用量记录，不承诺对所有 provider 实现精确金额硬熔断。

## 19 后续拓扑扩展的边界

第二阶段扩展可以增加结构化 Parallel/Join 和有限 Switch。Fork 或路由决策提交时固定本轮选中的分支集合；Join 等待这些分支，包括尚在排队的分支，而不是只统计已启动的 Task。必需分支失败和取消不能作为成功。

再增加显式、单入口单出口的有限返工区域。返工创建新的节点激活身份和业务轮次，技术重试仍留在同一 NodeRun；Join 与审批绑定对应轮次。禁止先复用一个全局 result 字段，再通过覆盖数据模拟返工。

画布和 AI 生成共用同一个受版本约束的定义格式、校验器和发布流程。它们是定义与观察入口，不能成为绕过执行契约的另一套调度器。
