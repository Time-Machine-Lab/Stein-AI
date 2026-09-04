# Setup：OpenSpec

**负责人：** Delivery Manager

1. 执行 `openspec --version`。命令不存在或无法返回版本号时，先执行：

   ```bash
   npm install -g @fission-ai/openspec@latest
   ```

2. 检查项目根目录是否存在 `openspec/`。不存在时执行 `openspec init`。
3. 检查当前 Agent 所需的 OpenSpec Skill 是否可加载。当前 Workflow 使用的 Skill 包括 `openspec-explore`、`openspec-propose`、`openspec-apply-change`、`openspec-verify-change` 和 `openspec-archive-change`；每个 Skill 必须能由当前 Agent 的 Skill loader 解析，并且其目录中存在可读取、元数据有效的 `SKILL.md`。OpenSpec CLI 没有检查这些 Skill 的命令，不能用 `openspec` 命令代替 Skill loader 检查。
4. 确保 `tml-docs/` 存在。
5. 再次执行 `openspec --version`，确认 `openspec/` 可被后续 Workflow 使用。

