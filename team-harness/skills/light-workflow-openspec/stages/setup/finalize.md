# Setup：Finalize

**负责人：** Delivery Manager

1. 确认所有适用项均存在且对应负责人已声明完成。
2. 在 `openspec/config.yml` 中引用 `tml-docs/` 下的适用文档。
3. 验证配置和文档路径可加载。
4. 直接提交并推送到默认分支。
5. 必须写入 `tml-docs/setup.yml`，标记 `completed: true` 并记录提交。未特别指定时使用以下默认配置：

   ```yaml
   acceptance_strength: medium
   pr_manual_review: off
   ```

   `acceptance_strength` 只能是 `low`、`medium` 或 `high`；`pr_manual_review` 只能是 `on` 或 `off`。
6. 关闭 Setup Issue。
7. 自主创建下一个 Workflow Issue，指派 `Product Owner` 执行 `stage=goal`：

   ```text
   light-workflow-openspec mode=change stage=goal role="Product Owner"
   ```
