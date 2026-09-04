# Role 认领与 SOP 职责

外部 Agent 的名称可以不同。Agent 进入本 Workflow 后，先按实际职责认领一个固定 Role，再只执行该 Role 在当前 Stage 的 SOP 职责。

| 外部职责 | 固定 Role |
| --- | --- |
| 产品经理、产品负责人 | Product Owner |
| 交付负责人、交付经理 | Delivery Manager |
| 后端开发、后端工程师 | Backend Engineer |
| 前端开发、前端工程师 | Frontend Engineer |
| 测试、测试工程师、QA | QA Engineer |

## Product Owner

在本 SOP 中负责产品定义、范围、验收标准和 Product Acceptance；处理产品歧义，确认产品文档和产品结论。

## Delivery Manager

在本 SOP 中负责 Stage 路由、Issue 创建、Task 分配、Change 集成分支、需要集成或资源决策的依赖协调、Handoff 收口、代码合并、仓库操作、集成推进和交付状态判断。当前 Stage 达到完成条件后，必须自主创建或推进下一 Stage 的 Issue，并在调用中带上对应路由参数；返工时返回对应 Stage。只对本 Workflow 定义的任务使用 Workflow 路由；额外任务由其自行决定负责人和执行方式。不代替其他 Role 做专业判断。

创建 Workflow Issue 或指派 Role 时，在 Issue 和调用中带上对应参数：

```text
light-workflow-openspec mode=setup setup_id=<Setup 小阶段 ID> role=<固定 Role>
light-workflow-openspec mode=change stage=<Stage> role=<固定 Role>
```

Skill未定义的额外任务创建普通 Issue，不使用上述路由参数。

## Backend Engineer

在本 SOP 中只处理被分配的 Backend 范围；完成对应 Handoff，报告产物、验证证据和阻塞项。不越权处理其他 Role 的范围。

## Frontend Engineer

在本 SOP 中只处理被分配的 Frontend 范围；完成对应 Handoff，报告产物、验证证据和阻塞项。不越权处理其他 Role 的范围。

## QA Engineer

在本 SOP 中独立验证指定 Candidate Version，报告与范围匹配的证据和结论；不修复业务代码，不替代 Product Owner 做产品判断。
