---
name: light-workflow-openspec
description: 团队级轻量化 OpenSpec 协作 Workflow，用于协调多 Role Agent 完成产品项目的开发与维护；通过明确的 Stage、Handoff 和 Role 协作推进产品需求、开发、QA、验收与交付；不用于孤立的编码、测试或一般 OpenSpec 咨询。
---

# 轻量级 OpenSpec Workflow

本 Workflow 是团队级轻量化产品项目开发与维护流程，用于让 Multica 小队从产品需求确认开始，经过方案制定、前后端开发、QA 验证、产品验收和最终交付，协同完成项目工作。它通过统一的 Stage、Role、Issue、Handoff 和完成门禁，明确每个阶段由谁推进、谁负责执行、何时可以进入下一阶段，同时保留各专业 Agent 在职责范围内的自主决策空间。OpenSpec 在其中作为需求、Proposal、规格和变更记录工具使用；本 Workflow 本身不限制具体技术实现，也不要求所有额外任务都使用 Workflow。

## 入口

1. 读取 [roles.md](roles.md)，按实际职责认领固定 Role。
2. 公共规则未知时，读取 [common.md](policies/common.md)。
3. 根据下方路由表进入对应流程。

Stage Issue 的创建、分配和路由参数由`Delivery Manager` 进行并遵循 [stage-issue-routing.md](policies/stage-issue-routing.md)。

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
每个 Change 按 `goal（产品定义）→ proposal → development → qa → acceptance → delivery` 推进。当前 Stage 达到完成条件后，由 `Delivery Manager` 自主推进下一 Stage；返工时返回对应 Stage。

## Skill 命令 解析

```text
mode=setup | change
setup_id=<Setup 小阶段 ID>                         # mode=setup 时使用
stage=<goal|proposal|development|qa|acceptance|delivery>  # mode=change 时使用
step_id=<当前 Stage 定义的小阶段 ID>                       # 仅在该 Stage 定义时使用
role=<固定 Role>
```

示例：

```text
light-workflow-openspec mode=setup setup_id=product-concept role="Product Owner"
light-workflow-openspec mode=change stage=development role="Backend Engineer"
```

没有参数时，`Delivery Manager` 默认进入 Setup；其他 Role 等待指派。`step_id` 只由当前 Stage 文件解释，不建立全局清单。

## 资源阻塞

缺少资源时停止受影响工作并通知 `Delivery Manager`，不得猜测、伪造或输出敏感值。详细规则见 [setup-resource-handling.md](policies/setup-resource-handling.md)。

## Workflow 边界

- 蓝皮书中定义的任务，使用对应路由；
- 蓝皮书未定义的额外任务，不强制使用本 Skill；
- 额外任务由 `Delivery Manager` 决定负责人和执行方式。
