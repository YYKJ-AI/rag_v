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

## 2026-11-11：V1 跨版本指针证据链

**分支：**`feature/document-pointer-relations`。**环境：**Windows PowerShell、Go 1.27.3；`GOCACHE` 指向项目内忽略目录 `data/.gocache`。隔离演示目录为 `data/pointer-eval-20261111`，不改默认数据。

| 实际执行 | 通过 | 失败 | 跳过 | 结果 |
| --- | ---: | ---: | ---: | --- |
| 初次聚焦测试（接口实现尚未完成时） | 0 | 1 次编译失败 | 0 | `answerFromPointer` 未定义；补齐实现后复测通过，不计入最终通过数 |
| 指针聚焦测试 `go test -count=1 -run 'TestInheritedClauseShowsBothSources|TestPointerMissingTargetRefuses|TestPointerScopeMismatchAndReferenceDoNotInherit|TestPointerDoesNotFollowSecondHopOrFutureTarget|TestBuildPointerRelationAndPersist|TestBuildUnresolvedPointerAndExplicitReference' -v ./internal/engine ./internal/ingest` | 6 个顶层测试、5 个子测试 | 0 | 0 | 双来源、历史、显式引用、存储持久化及拒答边界通过 |
| 最终 `go test -count=1 ./...` | 5 个有测试的包 | 0 | 0 | 全包通过；另一次 `go test -count=1 -json ./...` 统计 60 个 pass 事件，含顶层与子测试，不重复相加 |
| `go vet ./...`、本次 Go 文件 `gofmt -l` | 2 | 0 | 0 | 无诊断或待格式化文件 |
| `git diff --check` | 1 | 0 | 0 | 无空白错误；PowerShell 显示 Plan.md 的 CRLF 将被 Git 归一化提示 |
| 隔离目录导入 `docs/` | 12 份文档 | 0 | 0 | 全部建索引 |
| 同目录 `eval/queries.json` 演示评测 | 21 条查询 | 0 | 0 | 21/21；Hit@5 18/18、来源可核验 18/18、拒答 3/3；此指标不证明语义充分性 |

**最终复测：**指针聚焦用例通过；Go 全包和演示集通过。BUG-10 未由本任务解决；交付验证分支的 H13 保留失败不计入本分支测试成绩。

## 2026-11-11：同步开放问题与文档边界

**分支：**`docs/sync-pointer-issue-status`。**范围：**文档与 `.gitignore`，没有改 Go 代码；不重复运行无关 Go 测试。

| 实际执行 | 通过 | 失败 | 跳过 | 结果 |
| --- | ---: | ---: | ---: | --- |
| `git status --short --branch` 与 `git check-ignore -v project-notes/open-issues.md` | 2 | 0 | 0 | 从 `work/ragbot` 建任务分支；Git 规则以 `!` 例外允许追踪开放问题清单 |
| `rg` 核对 OI-3a/OI-3b、BUG-3 和 P2 模型边界 | 3 组文档检查 | 0 | 0 | 已解决范围、保留问题、未合并分支状态和候选入库门槛相互一致 |
| 对照 `git show` 中交付验证分支和当前基线的 Engine/缺陷记录 | 3 | 0 | 0 | 交付验证分支无共享 `lastInferredTime`，BUG-3 标已解决、H13 标未解决；当前基线仍有共享状态，BUG-3 标未解决 |
| `git diff --check` | 1 | 0 | 0 | 无空白错误；PowerShell 提示 Plan.md 的 CRLF 将由 Git 归一化 |
| Go 单测、CLI 评测 | 0 | 0 | 2 类未运行 | 本次没有业务代码变动；沿用上次指针关系任务的实测记录，不冒充本次新测试 |

**最终文档核验：**暂存清单仅含 `.gitignore` 与六份项目笔记；`git diff --cached --check` 无空白错误。无新增业务测试结果。

## 2026-11-11：非规范 Markdown 降级入库

**分支：**`feature/unstructured-markdown-ingest`。**环境：**Windows PowerShell、项目隔离数据目录 `data/unstructured-check-*`；系统 Go 缓存需沙箱外运行。以下通过数是实际执行结果，不把预期的严格拒绝计为代码失败。

| 实际执行 | 通过 | 失败 | 跳过 | 结果 |
| --- | ---: | ---: | ---: | --- |
| 新增 8 个聚焦测试：BOM、非规范解析、无正文/损坏元数据、精确副本/别名/同名冲突、搜索未认证标注、问答拒答、独立索引 BM25 隔离 | 8 | 0 | 0 | 最终聚焦复测通过；初版 Engine 测试漏传 `nil` 时间参数造成 1 次编译失败，已修正后通过 |
| `go test -count=1 ./...` | 6 个有测试的包 | 0 | 0 | 最终全包通过；另 4 个包没有测试文件。首次沙箱内运行因系统 Go 缓存拒绝访问，沙箱外复测通过 |
| `go vet ./...`、`gofmt -l internal cmd`、`git diff --check` | 3 | 0 | 0 | 无诊断、未格式化文件或空白错误 |
| CLI 显式 `-allow-unstructured` 导入带 BOM 的无元数据文件 | 1 文件，1 节，1 块 | 0 | 0 | 标记 `needs_review`，业务日期未知；初版产生 2 节，修复 BOM 后新目录复测为 1 节 |
| CLI `search 身份证明` 与 `ask 申请人需要提交什么` | 2 | 0 | 0 | 搜索标记未认证；问答为 `insufficient_evidence`，零引用 |
| CLI 默认严格导入同一文件 | 1 个预期拒绝 | 0 | 0 | 返回“缺少 front-matter”且退出码 1，符合默认严格契约 |

**最终结论：**已认证索引与待审核搜索索引隔离；待审核文档不影响正式 BM25 分数。新增 BOM 缺陷已修复并复测。既有 BUG-3、BUG-10 和未合并保留集 H13 不计入本次通过数。
