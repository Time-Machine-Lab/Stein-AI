---
name: light-workflow-openspec
description: 团队级轻量化 OpenSpec 协作 Workflow，用于协调多 Role Agent 完成产品项目的开发与维护；通过明确的 Stage、Handoff 和 Role 协作推进产品需求、开发、QA、验收与交付；不用于孤立的编码、测试或一般 OpenSpec 咨询。
---

# 轻量级 OpenSpec Workflow

本 Workflow 是团队级轻量化产品项目开发与维护流程，用于让 Multica 小队从产品需求确认开始，经过方案制定、前后端开发、QA 验证、产品验收和最终交付，协同完成项目工作。它通过统一的 Stage、Role、Issue、Handoff 和完成门禁，明确每个阶段由谁推进、谁负责执行、何时可以进入下一阶段，同时保留各专业 Agent 在职责范围内的自主决策空间。OpenSpec 在其中作为需求、Proposal、规格和变更记录工具使用；本 Workflow 本身不限制具体技术实现，也不要求所有额外任务都使用 Workflow。

## 自动连续推进原则

本 Workflow 默认持续自动推进。Agent 不得因为发现问题、等待团队协作或完成当前产物而自行结束整条流程；每次退出当前 Task 前，都必须完成有效 Handoff，或明确记录真正需要人类介入的事项。`in_review` 只表示当前 Stage 按其定义进入内部完成确认，不默认表示等待人类，也不默认要求 Delivery Manager 重做专业审核。各 Stage 的负责人和完成条件决定谁确认、确认什么以及是否直接进入 `done`。

Task 在 Change 目标完成前不得自行结束。非终态退出必须在同一条评论中 `@mention` 一个平台可解析的具体 Agent，并写明唯一下一动作；只写 Role 名称或“请 Delivery Manager 处理”不会触发后续 Task。终态 `done` 仅在没有任何后续动作时使用。

Delivery Manager 的职责是保持路由连续：确认当前 Stage 的完成条件是否满足、处理必要的集成或返工，并实际创建、激活或触发下一 Stage。只写“下一步进入某阶段”而没有触发下一项工作，不算推进完成。异常恢复仍按本 Workflow 的 Handoff、依赖和返工规则处理。

## 入口

1. 读取 [roles.md](roles.md)，按实际职责认领固定 Role。
2. 公共规则未知时，读取 [common.md](policies/common.md)。
3. 由 `Delivery Manager` 先判断当前 Change 的复杂度，再根据下方路由表进入对应流程。

Stage Issue 的创建、分配和路由参数由`Delivery Manager` 进行并遵循 [stage-issue-routing.md](policies/stage-issue-routing.md)。

### Change 复杂度分级

进入 `change` 模式时，先记录 `change_profile`：

| `change_profile` | 判断标准 | 执行方式 |
| --- | --- | --- |
| `micro` | 不改变产品目标、公开接口、数据结构或核心运行时行为，范围有限且可由一个 Agent 完成 | 使用轻量路径；一个 Driver 记录目标和方案、完成实现与必要校验，再由 `Delivery Manager` 收口 |
| `standard` | 一个明确产品目标或局部功能/缺陷，需要常规实现和验证，但不存在必须并行的独立工作流 | 使用标准 Stage 顺序，默认由一个 Driver 推进，按需邀请专业 Agent |
| `complex` | 存在真实并行工作、专业或权限隔离、独立验证需求，或单一上下文无法有效承载 | 使用完整 Stage 顺序，可按独立产物拆分多个 Agent 和 Issue |

`change_profile` 是路由依据，不是按任务大小或参与人数自动推断的标签。无法判断时先按 `standard` 处理，由 `Delivery Manager` 在 Proposal 中补充判断；不得因为存在多个固定 Role 就直接判为 `complex`。`micro` 仍需保留目标、完成条件和验证证据，但不为没有独立产物的角色创建子 Issue。

## Workflow 路由

| mode | stage | 目的 | 入口文件 |
| --- | --- | --- | --- |
| `setup` | - | 准备项目 Workflow 环境 | [stages/setup/index.md](stages/setup/index.md) |
| `change` | `goal` | 定义本次产品需求和范围 | [stages/goal.md](stages/goal.md) |
| `change` | `proposal` | 形成 Proposal 和 Task | [stages/proposal.md](stages/proposal.md) |
| `change` | `development` | 完成分配的开发和集成 | [stages/development.md](stages/development.md) |
| `change` | `qa` | 验证 Candidate Version | [stages/qa.md](stages/qa.md) |
| `change` | `acceptance` | 完成 Product Acceptance | [stages/acceptance.md](stages/acceptance.md) |
| `change` | `delivery` | 合并、交付和 Archive | [stages/delivery.md](stages/delivery.md) |

Setup 是首次使用本 Workflow 前的一次性准备，不属于 Change 的阶段顺序。
`standard` 和 `complex` Change 按 `goal（产品定义）→ proposal → development → qa → acceptance → delivery` 推进。`micro` Change 可以在同一 Task 中合并记录目标与方案，直接由一个 Driver 执行并完成必要校验后交付；只有出现产品判断、运行时风险或独立验证需求时，才补充对应 Stage。当前 Stage 达到完成条件后，由 `Delivery Manager` 自主推进下一 Stage；返工时返回对应 Stage。

每个 Change 的 Proposal 阶段必须判断是否影响项目 `tml-docs/` 基线文档；只有长期产品、工程、设计、测试或 Workflow 约束发生变化时才创建文档维护任务，具体规则见 [common.md](policies/common.md)。

Workflow 中的 `backlog` 只用于停放尚未满足前置条件的未来 Stage 或任务，不是永久等待。当前 Stage 完成且下一项依赖满足后，`Delivery Manager` 必须将对应 Issue 提升为 `todo` 并实际触发负责人；普通 backlog 和依赖未满足的任务保持不动。

## Skill 命令 解析

```text
mode=setup | change
setup_id=<Setup 小阶段 ID>                         # mode=setup 时使用
stage=<goal|proposal|development|qa|acceptance|delivery>  # mode=change 时使用
step_id=<当前 Stage 定义的小阶段 ID>                       # 仅在该 Stage 定义时使用
role=<固定 Role>
change_profile=<micro|standard|complex>               # mode=change 时由 Delivery Manager 记录；缺省时按 standard 判断
```

示例：

```text
light-workflow-openspec mode=setup setup_id=product-concept role="Product Owner"
light-workflow-openspec mode=change stage=development role="Backend Engineer"
```

没有参数时，`Delivery Manager` 默认进入 Setup；其他 Role 等待指派。`step_id` 只由当前 Stage 文件解释，不建立全局清单。

## 资源阻塞

缺少资源时停止受影响工作，并在同一条阻塞评论中 `@mention` 具体的 `Delivery Manager` 成员账号，说明所需资源和恢复条件；不得猜测、伪造或输出敏感值。详细规则见 [setup-resource-handling.md](policies/setup-resource-handling.md)。

## Workflow 边界

- 蓝皮书中定义的任务，使用对应路由；
- 蓝皮书未定义的额外任务，不强制使用本 Skill；
- 额外任务由 `Delivery Manager` 决定负责人和执行方式。
