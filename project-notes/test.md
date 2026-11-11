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

## 2026-11-11：修正版本和适用范围

**分支：**`fix/version-scope`。**环境：**Windows PowerShell，Go 1.27.3；临时 `GOCACHE`。CLI 使用 `%TEMP%/ragbot-scope-final-20261111` 全新目录导入八份虚构资料；默认数据目录未更改。

| 实际执行 | 通过 | 失败 | 跳过 | 结果 |
| --- | ---: | ---: | ---: | --- |
| 最初聚焦测试在旧实现下 `go test -run 'TestVersionSelectionByBusinessDate|TestCrossDocumentReplacement|TestAnswerWithScopeKey' -v ./internal/engine` | 0 | 1（预期红灯） | 0 | `AnswerWithScope` 未实现，后续实现并复测 |
| `go test -run TestParseYMDRejectsNormalizedDates -v ./internal/engine`，先复现旧解析器错误 | 0 | 1（预期红灯） | 0 | 2025-02-29、2025-02-30 被归一化，后续修复 |
| 聚焦版本/范围/HTTP 单测 | 6 个顶层测试、2 个子测试 | 0 | 0 | 未来与历史版本、跨 ID 替代、范围筛选及部分范围替代、有效期末日、HTTP 范围参数 |
| `go test -count=1 -v ./...` | 22 个顶层测试、7 个子测试 | 0 | 0 | 全包通过 |
| `go vet ./...` | 1 | 0 | 0 | 无诊断 |
| `gofmt -l`（本次 Go 文件）与 `git diff --check` | 2 | 0 | 0 | 无输出 |
| 全新目录 `go run ./cmd/ragbot -data <临时目录> ingest docs` | 8 个文件 | 0 | 0 | 索引 8 份文档 |
| 同目录 `go run ./cmd/ragbot -data <临时目录> -baseline <临时报告> evaluate` | 14 条查询 | 0 | 0 | 14/14；Hit@5 13/13，来源可核验 13/13，拒答 1/1，平均约 0.237ms |
| CLI 按 `resident` 与未知范围提问、显式历史日期提问 | 3 | 0 | 0 | 当前 v2、未知范围拒答、历史 v1；HTTP 另由单测验证 |
| 解析最终参考快照核对逐条结果与旧版引用 | 1 | 0 | 0 | 14/14；当前 Q01/Q04/Q05/Q14 的旧版和年度历史引用数为 0 |

**最终复测统计：**22 个顶层 Go 测试、7 个子测试，失败 0、跳过 0；CLI 导入 8/8、评测 14/14。聚焦和全量测试有重复，未相加宣称独立用例数。两次预期红灯单列，最终均已复测通过。

## 2026-11-11：接通真实子块检索

**分支：**`feature/real-chunk-retrieval`。**环境：**Windows PowerShell，Go 1.27.3；临时 `GOCACHE`。CLI 使用 `%TEMP%/ragbot-chunks-20261111` 全新数据目录，导入八份虚构资料。

| 实际执行 | 通过 | 失败 | 跳过 | 结果 |
| --- | ---: | ---: | ---: | --- |
| 在旧实现上运行三个检索聚焦测试 | 0 | 3（预期红灯） | 0 | 真实子块 ID 为空，父块独有文字被虚假命中，同分顺序不稳定；实现后复测通过 |
| `go test -run 'TestSearchUsesIngestedChunksForLongProcessAndTable|TestSearchAggregatesBestChunkAndReindexIsIdempotent|TestSearchTieOrderIsStable' -v ./internal/retriever` | 3 个顶层测试、3 个子测试 | 0 | 0 | 长文、流程、表格、聚合、幂等与排序通过 |
| `go test -count=1 -v ./...` | 25 个顶层测试、10 个子测试 | 0 | 0 | 全包通过 |
| `go vet ./...`、`git diff --check` | 2 | 0 | 0 | 无诊断或空白问题 |
| 隔离目录导入 `docs/` | 8 个文件 | 0 | 0 | 索引 8 份文档 |
| 隔离目录运行 14 条查询 | 14 | 0 | 0 | 14/14；Hit@5 13/13、来源可核验 13/13、拒答 1/1，平均约 0.239ms |
| 对照上轮参考快照的 14 条状态、引用来源和摘录 | 14 | 0 | 0 | 引用内容变化 0 条；更新时间与耗时快照 |

**最终复测统计：**25 个顶层 Go 测试、10 个子测试，失败 0、跳过 0；导入 8/8、评测 14/14。初次 3 个预期红灯单列，最终均已修复。

## 2026-11-11：扩充文档类型和证据判定

**分支：**`feature/document-evidence-types`。**环境：**Windows PowerShell、Go 1.27.3，`GOCACHE` 临时指向项目内缓存（测试后删除）；`data/evidence-final-20261111` 为全新隔离数据目录，不改默认数据。

| 实际执行 | 通过 | 失败 | 跳过 | 结果 |
| --- | ---: | ---: | ---: | --- |
| 首次全量 `go test -count=1 ./...` | 0 | 1（缓存访问阻断） | 0 | 默认用户缓存被沙箱拒绝访问；改用临时缓存后进入测试 |
| 临时缓存下首次全量 Go 测试 | 其余包通过 | 4 个顶层测试和 2 个子测试失败 | 0 | 标题主题剥离误伤“申请费用”；收紧条件并新增回归测试后复测。首次输出未统计顶层通过总数 |
| 全量 `go test -count=1 -json ./...` | 32 个顶层测试、17 个子测试 | 0 | 0 | 全包通过；子测试另列，不与顶层数相加为独立案例 |
| `go vet ./...`、`gofmt -l`（五个改动的 Go 文件）、`git diff --check` | 3 | 0 | 0 | 无诊断、未格式化文件或空白错误 |
| 新目录 `go run ./cmd/ragbot -data data/evidence-final-20261111 ingest docs` | 12 份文档 | 0 | 0 | 四类新增资料全部索引 |
| 同目录 21 条查询评测并保存 `eval/baseline/reference.json` | 21 条 | 0 | 0 | Hit@5=18/18、出处=18/18、来源可核验=18/18、拒答=3/3，平均回答调用约 0.337ms |
| 对照旧 14 条状态及引用集合；手工核对 Q02、Q03、Q08、Q14、Q15、Q17、Q19 | 14 条状态、7 条重点引用 | 0（按既有评测口径） | 0 | 旧状态全保持；11 条引用集合变化。Q03/Q08/Q14 有不相关附带引用，作为未解决 BUG-10 记录，不宣称语义通过 |

**最终复测统计：**32 个顶层 Go 测试、17 个子测试，失败 0、跳过 0；隔离导入 12/12、开发评测 21/21。首次因缓存阻断的命令及一次真实回归失败单列，均已定位并复测；语义相关性问题保持未解决。
