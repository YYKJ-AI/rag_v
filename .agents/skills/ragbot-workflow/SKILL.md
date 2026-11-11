---
name: ragbot-workflow
description: Plan, implement, test, review, and merge work in this ragbot repository using its non-main branch workflow and project-notes records. Use for repository work; do not apply outside this project.
---

# ragbot 工作流

只在本仓库使用。以仓库根目录的 AGENTS.md 和本地 project-notes/ 为入口。project-notes/ 被 Git 忽略；新检出若没有该目录，先在本地建立下述工作记录。

## 开工

1. 先阅读本地 project-notes/Plan.md（不存在则创建）与当前分支、工作区状态。对本次任务写明目标、范围、原工作分支、任务分支、验收条件和计划执行的测试；计划写好后才修改业务代码或配置。
2. 原工作分支必须不是 main。若当前仅有 main，先建立非 main 工作分支，再从它创建任务分支。若已有未提交工作，保留原状并先确定归属，避免跨任务混入。
3. 每项任务从原工作分支创建独立任务分支；默认使用 feature/<topic>，修复可用 fix/<topic>。记录任务分支及合并目标，不把 main 当作日常合并目标。

## 实施与记录

- 在任务分支实现本次计划，不把规划项写成已完成项。
- project-notes/ 只作为本地工作记录，不加入 Git 索引；需跨机器保存时，由用户另行决定保存方式。
- 在 project-notes/issues.md 记录实际改动、原因、影响和未完成事项。
- 在 project-notes/test.md 记录每次实际执行的测试：日期、分支、命令或手工步骤、环境/数据集、通过数、失败数、跳过数、结果与失败原因。未运行的测试标为未运行。
- 在 project-notes/bug.md 记录发现或引入的缺陷、复现方式、影响、修复和复测结果；未修复的缺陷保持打开状态。没有新缺陷时明确记为“无新增缺陷”。
- 对只改文档或流程的任务，也要验证路径、Git 状态和相关文档契约；运行受影响范围内的测试，不为了凑数量执行无关测试。

## 完成与合并

1. 核对计划验收条件、改动和测试记录；失败的必需测试或未解决的本任务缺陷意味着任务未完成。
2. 任务完成且相关测试通过后，才可将任务分支合回记录的原非 main 工作分支。先确认合并目标和工作区状态，并在 issues.md 记录合并结果。
3. 只有用户明确要求合并到 main，才可执行任何以 main 为目标的合并。任务分支合回非 main 不等于获准合入 main。
4. 不以临时跳过测试、改写失败记录或仅凭 go test 通过替代验收；在最终回复中说明已完成、测试结果、遗留风险和当前分支。
