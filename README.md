# ragbot：可溯源的离线知识库问答

**一句话定位：**纯 Go、无模型依赖的知识库问答系统；从 Markdown 提取证据，按版本与适用时间回答，证据不足时拒答。演示数据均为虚构资料。

## 三个核心能力

1. **版本与引用可追溯。**按生效日、失效日、适用范围和明确的替代关系选择版本。新版若明确继承旧版条款，只跟随一跳，并分别列出新版指针和旧版原文的来源；不会把旧文写成新版原文。
2. **确定性证据与拒答。**真实子块检索后，从原文抽取可定位句子；缺少可核验依据时返回 `insufficient_evidence`，不编造引用。HTTP 问答的时间提示按请求独立计算，避免并发串扰。
3. **双轨导入与索引隔离。**默认严格校验 Markdown 元数据；显式使用 `-allow-unstructured` 时，有正文但缺少必要元数据的文件进入待审核搜索索引。待审核资料可搜索，但不会参与正式问答、版本裁决或已认证索引的 BM25 排序。

## 运行

需要 Go 1.22+；默认数据写入 `data/store.json`，无需 Python、模型或 API 密钥。首次使用建议指定全新数据目录，避免旧演示索引残留。

后续模型接入的配置样例见 [`.env.example`](.env.example)；本机值写入被 Git 忽略的 `.env`。当前 CLI 尚不读取 `.env` 或其中的模型变量，运行仍走上述确定性链路。

```bash
go run ./cmd/ragbot -data data/demo ingest docs
go run ./cmd/ragbot -data data/demo ask "现在申请要交哪些材料"
go run ./cmd/ragbot -data data/demo evaluate
go run ./cmd/ragbot -data data/demo -queries eval/holdout.json -baseline data/demo/holdout.json evaluate
go run ./cmd/ragbot -data data/demo serve
go test -count=1 ./...
```

`serve` 默认在 `http://localhost:8080` 提供现有网页和 API。导入非规范资料可用 `go run ./cmd/ragbot -data data/demo -allow-unstructured ingest <目录>`；其来源会标为未认证。需要重新评测时，请清空或更换演示数据目录。

## 实测结果与已知边界

2026-11-12 在全新目录导入 12/12 份虚构资料并重新运行两套问题；`go test -count=1 ./...` 和 `go vet ./...` 均通过。新口径给每条有答案的问题标注必要证据片段，逐条检查引用是否属于人工确认的支持证据；旧口径只检查来源及摘录可定位。

| 数据集 | 新口径逐题通过 | Hit@5 | 来源可核验 | 证据合格题 | 引用支持 | 拒答 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 开发集（新增同主题缺字段负例 Q23） | **13/23** | 19/19 | 19/19 | 10/19 | 22/42 | 3/4 |
| 当前保留文件（含已参与修复的 H03） | **8/13** | 11/11 | 10/11 | 6/11 | 11/18 | 2/2 |

**已知失败：**Q23 问星河工作坊报名费用，资料未记载，系统却引用入场券领取并误答（BUG-13）。Q03“盖什么章”没有引用实际写明公章的句子；Q06 漏掉安全许可条件，多题包含无关引用（BUG-10）。H13“最低年龄”命中正确资料后仍拒答（BUG-12）。H03“初审时限”曾用于修复并转入开发集，不能当作独立验证成功。旧口径下开发集 22/22、保留文件 12/13 是历史结果；新数字来自更严格的证据标注和新增负例，并非本次修改了问答策略。词表与阈值本轮未改，不能据此宣称真实业务准确率。

逐条结果、指标定义与复现步骤见 [评测结果](eval/RESULTS.md)；完整快照在 `eval/baseline/offline-evidence-*.json`。本地工作记录保存在被 Git 忽略的 `project-notes/`。
