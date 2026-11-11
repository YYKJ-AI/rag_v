# 工作与变更记录

本文件记录**实际完成**的工作、修改原因及产生的影响。计划和待办见 `Plan.md`；缺陷见 `bug.md`。

## 2026-11-11：建立项目工作流

- **分支：**从 `main` 建立非 `main` 基线 `work/ragbot`，再建立任务分支 `feature/project-workflow`。尚未合并到 `main`。
- **改动：**新增仓库级 `AGENTS.md` 与 `.agents/skills/ragbot-workflow/SKILL.md`，约定先写计划、任务分支开发、记录改动/测试/缺陷及合并门槛。
- **改动：**新增 `Plan.md`、`issues.md`、`test.md`、`bug.md`；修改 `.gitignore`，仅追踪这四份工作记录，继续忽略 `project-notes/` 下其他内部笔记。
- **影响：**后续工作有固定的计划与验证入口；普通任务先汇入非 `main` 工作分支，`main` 仅在用户明确要求时合并。未修改 Go 业务逻辑或运行数据。
- **后续：**按 `Plan.md` 中的独立任务逐项推进；本次流程任务的验证结果见 `test.md`。
- **合并结果：**`feature/project-workflow` 在验证通过后合回原非 `main` 分支 `work/ragbot`；`main` 保持不变。
