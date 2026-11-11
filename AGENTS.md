# ragbot 项目约定

在本仓库进行计划、实现、测试、审查或合并时，使用项目专用的 [ragbot-workflow](.agents/skills/ragbot-workflow/SKILL.md) skill。

`main` 只在用户明确提出合并到 `main` 时才接收工作。日常任务从非 `main` 工作分支创建任务分支；工作记录保存在 `project-notes/`。
