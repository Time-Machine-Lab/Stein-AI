# 异常和必须暂停的情况

只有触发以下条件时才读取本文件。

## 项目环境缺失或冲突

Delivery Manager 检查 Project、Project Context、声明的 Workflow、Git 仓库和项目级 Skill。任一必要内容缺失、冲突或无法验证时，按 `multica-project-bootstrap` 自身的说明执行。不得修改 Agent 的 Skill 绑定，也不得猜测 Workflow。缺少必要环境或权限时暂停。

## 产品或规格冲突

记录冲突的内容和影响，把决定交给 Product Owner。不得通过实现代码解决产品冲突，也不得为了匹配现有代码而修改预期行为。

## Task 依赖或共享资源冲突

说明被阻塞的工作、需要的结果、负责人和资源。Delivery Manager 决定顺序、责任人或等待方式。不依赖该资源的工作可以继续。

## 范围扩大

区分原始需求和新想法，由 Product Owner 判断是否属于已确认范围。新增产品范围应创建新的 Change，不得悄悄加入当前 Proposal。

## Baseline 或 Candidate Version 不明确

在修改或验证代码前暂停。Delivery Manager 必须明确具体分支、提交或 Candidate Version。不得从旧消息或无关的成功运行中推断 Baseline。

## 安全、权限或敏感数据问题

暂停并说明要执行的动作、影响和需要的决定。包括破坏性数据变更、安全或权限变更、隐私敏感数据、缺少凭据和外部服务不可用。

## 验证失败

将结果分类为实现缺陷、需求或规格冲突、测试缺陷、环境问题或无法得出结论。实现缺陷交给相关 Agent，产品冲突交给 Product Owner，环境或流程问题交给 Delivery Manager。保留验证证据。

## Change 过大

如果 Change 包含独立产品目标、验证范围不清晰、长期造成等待或合并冲突不断扩大，应建议拆分。拆分后的每个 Change 都需要自己的目标、Proposal、集成结果、QA 结论、Product Acceptance、PR 和 Archive。
