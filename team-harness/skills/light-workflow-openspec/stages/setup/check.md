# Setup Check

**负责人：** Delivery Manager

## 检查流程

1. 确认 Multica 环境可用。
2. 按 [index.md](index.md) 的路由表逐项检查。
3. 已满足的项跳过；不适用的项记录跳过原因。
4. 只有路由表中的缺失或失效项才创建 Workflow Issue，并带上 `mode=setup`、`setup_id` 和 `role`。
5. 其他项目任务按 `Delivery Manager` 的决定创建普通 Issue，不使用本 Workflow 路由。
6. 负责人完成后重新执行本检查。
7. 全部适用项满足后，执行 `setup_id=finalize`。

## 检查标准

- `open-spec`：CLI、`openspec/` 和所需 Skill 可用。
- `product-concept`：`tml-docs/product-concept.md` 存在且 Product Owner 已确认完成。
- `backend`：项目需要 Backend，工程结构和 Development Spec 已完成。
- `frontend`：项目需要 Frontend，工程结构和 Development Spec 已完成。
- `qa`：QA 文档存在且 QA Engineer 已确认完成。
- `finalize`：所有适用项完成。
