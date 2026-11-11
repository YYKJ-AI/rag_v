# 测试记录

每次测试记录日期、分支、命令或手工步骤、环境或数据集、通过/失败/跳过数量和结果。未执行的检查不能记作通过。

## 2026-11-11：项目工作流

**分支：**`feature/project-workflow`。

**环境：**Windows PowerShell；Go 1.27.3，`go.mod` 目标版本 1.22；隔离的临时 `GOCACHE`。测试没有改动项目运行数据。

**验证结果：**

| 检查 | 预期 | 结果 |
| --- | --- | --- |
| 分支关系 | `git branch --show-current` 与两次 `git merge-base --is-ancestor`：任务分支来自 `work/ragbot`，工作基线来自 `main` | 通过，3/3 |
| skill 结构 | 读取 `SKILL.md`，检查 frontmatter 的 `name`、`description` 和结束标记 | 通过，1/1 |
| 文档可追踪性 | `git check-ignore -q`：四份工作记录未忽略，`roadmap.md` 仍忽略 | 通过，5/5 |
| 指令路径 | 检查 `AGENTS.md` 引用的 skill 文件存在 | 通过，1/1 |
| 工作区检查 | `git status --short --untracked-files=all` 和 `git diff --check`，仅见本任务文件 | 通过，2/2；未合并到 `main` |
| 暂存内容 | `git diff --cached --check`，检查本任务拟提交文件的空白与补丁格式 | 通过，1/1 |
| 合并检查 | `git merge --no-ff --no-commit feature/project-workflow`，无冲突；合并暂存内容再经 `git diff --cached --check` | 通过，2/2；目标为 `work/ragbot` |
| Go 回归 | `go test -count=1 -v ./...`，临时 `GOCACHE` | 通过，11/11 个测试，0 失败 |

**当前统计：**通过 26，失败 0，跳过 0（上述检查点 15 项，Go 单测 11 项）。

**执行中纠正：**最初将两条 Git 祖先检查合在一条 PowerShell 表达式中，命令因语法错误未执行；改为独立命令后 2/2 通过。此错误未改变仓库文件，也不计为产品测试失败。

**合并身份：**本机没有配置 Git 作者身份；提交和合并临时使用 `Codex <codex@localhost>`，未修改全局 Git 设置。

## 2026-11-11：修正评测口径

**分支：**`fix/evaluation-metrics`。**环境：**Windows PowerShell，Go 1.27.3；临时 `GOCACHE`。CLI 使用 `%TEMP%/ragbot-eval-20261111` 作为隔离数据目录，导入 `docs/` 八份虚构资料；未更改默认运行数据。

| 实际执行 | 通过 | 失败 | 跳过 | 结果 |
| --- | ---: | ---: | ---: | --- |
| 先运行 `go test ./internal/evaluate`，验证聚焦测试在旧实现下编译失败 | 0 | 1（预期红灯） | 0 | 缺少新评测函数和字段，随后实现并复测通过 |
| `go test -run TestScore -v ./internal/evaluate` | 3 个顶层测试、3 个子测试 | 0 | 0 | 覆盖拒答计数、指标分母、多来源、版本、引用定位、亚毫秒记录 |
| `go test -count=1 -v ./...` | 14 个顶层测试、3 个子测试 | 0 | 0 | 全部 Go 包通过；其余包无测试文件 |
| `go vet ./...` | 1 | 0 | 0 | 无诊断 |
| `gofmt -l`（本次三个 Go 文件）与 `git diff --check` | 2 | 0 | 0 | 均无输出 |
| 隔离目录 `go run ./cmd/ragbot -data <临时目录> ingest docs` | 8 个文件 | 0 | 0 | 索引 8 份文档 |
| 隔离目录 `go run ./cmd/ragbot -data <临时目录> -baseline <临时报告> evaluate` | 14 条查询 | 0 | 0 | Hit@5 13/13、有出处 13/13、来源可核验 13/13、拒答 1/1；平均约 0.224ms |
| 解析 `eval/baseline/reference.json` 核对汇总与逐条通过数、分母、正耗时 | 1 | 0 | 0 | 14/14，14 条耗时均大于 0 |

**最终验证统计：**最终复测通过 46 个检查点（顶层 Go 测试 14、嵌套子测试 3、静态/基线检查 4、导入文件 8、查询 14、聚焦顶层测试 3；聚焦及全量测试有重复），失败 0，跳过 0。最初的 1 次预期编译失败单列，不计为最终失败。
