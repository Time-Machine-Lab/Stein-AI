# Stage：Project Workflow Setup

## 目的

为当前项目准备可执行的 Workflow 环境。Setup 属于整个 Workflow，不属于某个 Change，正常只执行一次。

## 入口

1. `Delivery Manager` 检查 `tml-docs/setup.yml`。
2. `completed: true` 时跳过 Setup。
3. 文件缺失或未完成时，`Delivery Manager` 创建 Setup Issue，并读取 [check.md](check.md)。
4. 被指派的 Role 只读取与 `setup_id` 对应的阶段文件。

## 路由表

| setup_id | Role | 阶段文件 |
| --- | --- | --- |
| `open-spec` | Delivery Manager | [open-spec.md](open-spec.md) |
| `product-concept` | Product Owner | [product-concept.md](product-concept.md) |
| `backend` | Backend Engineer | [backend.md](backend.md) |
| `frontend` | Frontend Engineer | [frontend.md](frontend.md) |
| `qa` | QA Engineer | [qa.md](qa.md) |
| `finalize` | Delivery Manager | [finalize.md](finalize.md) |

## 完成

所有适用检查项完成后，由 `Delivery Manager` 执行 [finalize.md](finalize.md)，并将 `completed: true` 写入 `tml-docs/setup.yml`。
