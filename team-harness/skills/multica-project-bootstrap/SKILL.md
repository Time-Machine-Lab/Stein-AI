---
name: multica-project-bootstrap
description: 在项目开发开始前，确认 Workflow、GitHub 仓库，并把选定的 Workflow Skill 安装到后续任务可继承的仓库基线。
---

# Multica 项目初始化

本 Skill 只负责准备项目环境，不定义具体 Workflow。

## 使用条件

用户要求开始项目开发、维护或迭代，且以下任一条件未满足时使用：

- 无法确认目标 Multica Project；
- Project Context 未声明当前 Workflow；
- Project 没有关联可用的 Git 仓库；
- 仓库中没有当前 Workflow 的项目级 Skill。

咨询、解释和方案讨论不要使用本 Skill。

## 关键流程

### 1. 确认 Workflow

读取 Project Context。若没有当前 Workflow，向用户询问要使用的 Workflow。

从 Multica Workspace Skills 中确认该 Workflow Skill 存在：

```bash
multica skill list --output json
multica skill get <skill-id> --output json
```

不要预设唯一 Workflow，也不要创建 `tml-workflow` 路由层。

### 2. 准备 GitHub 仓库

如果已有 `github_repo` 资源，验证 URL、默认 `ref` 和 Runtime 访问权限。

如果没有可用仓库，一次性向用户确认：

- GitHub 账号或组织；
- 仓库名称；
- 可见性（默认建议私有）；
- 默认分支（默认 `main`）。

用户确认后创建仓库，记录 clone URL，并将它关联为 Project 的 `github_repo` 资源。创建成功但关联失败时停止，不重复创建。

### 3. 更新 Project Context

保留其他内容，只写入用户确认的 Workflow：

```markdown
## AI Development Team Workflow

- 当前项目工作流程：`<selected-workflow-skill>`
- 团队规则：项目成员处理本项目工作时，统一遵循当前声明的 Workflow Skill。
```

Project Context 只声明团队应使用哪个 Workflow，不写 Skill 的加载路径和实现细节。

### 4. 安装项目级 Skill

在当前任务的仓库 checkout 中写入：

```text
.agents/skills/<selected-workflow-skill>/
├── SKILL.md
└── <支持文件，保持相对路径>
```

不要写入 `.codex/skills`。目标目录存在冲突时，先报告并等待确认，不得覆盖用户文件。

### 5. 推送到后续任务基线

当前任务可以直接使用刚写入的 Skill，但后续任务通常从远端分支或 `ref` 创建新的 checkout。因此必须完成：

```text
提交 Skill 文件
    -> 推送到远端
    -> 合并到后续任务使用的分支或 ref（默认 main）
    -> 重新读取远端基线，确认包含 .agents/skills/<selected-workflow-skill>/
```

只有远端基线验证通过，项目初始化才算完成。具体提交、推送和 PR 命令遵循项目 Git 规则。

### 6. 完成或停止

完成后报告：Project、Workflow Skill、仓库 URL、后续任务使用的远端分支/ref，以及验证结果。

以下情况必须停止并询问用户：

- Project 或 Workflow 无法确定；
- 缺少 GitHub 账号/组织、仓库名、可见性或创建授权；
- 凭据或权限不可用；
- Skill 或 Project Context 存在冲突；
- 远端基线未包含 Skill；
- 持久化修改无法验证。
