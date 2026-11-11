# ragbot：可溯源的离线知识库问答

**一句话定位：**纯 Go、无模型依赖的知识库问答系统；从 Markdown 提取证据，按版本与适用时间回答，证据不足时拒答。演示数据均为虚构资料。

## 三个核心能力

1. **版本与引用可追溯。**按生效日、失效日、适用范围和明确的替代关系选择版本。新版若明确继承旧版条款，只跟随一跳，并分别列出新版指针和旧版原文的来源；不会把旧文写成新版原文。
2. **确定性证据与拒答。**真实子块检索后，从原文抽取可定位句子；缺少可核验依据时返回 `insufficient_evidence`，不编造引用。HTTP 问答的时间提示按请求独立计算，避免并发串扰。
3. **双轨导入与索引隔离。**默认严格校验 Markdown 元数据；显式使用 `-allow-unstructured` 时，有正文但缺少必要元数据的文件进入待审核搜索索引。待审核资料可搜索，但不会参与正式问答、版本裁决或已认证索引的 BM25 排序。

## 运行

需要 Go 1.22+；默认数据写入 `data/store.json`，无需 Python、模型或 API 密钥。首次使用建议指定全新数据目录，避免旧演示索引残留。

```bash
go run ./cmd/ragbot -data data/demo ingest docs
go run ./cmd/ragbot -data data/demo ask "现在申请要交哪些材料"
go run ./cmd/ragbot -data data/demo evaluate
go run ./cmd/ragbot -data data/demo -queries eval/holdout.json -baseline data/demo/holdout.json evaluate
go run ./cmd/ragbot -data data/demo serve
```

`serve` 默认在 `http://localhost:8080` 提供现有网页和 API。导入非规范资料可用 `go run ./cmd/ragbot -data data/demo -allow-unstructured ingest <目录>`；其来源会标为未认证。需要重新评测时，请清空或更换演示数据目录。

## 实测结果与已知边界

2026-11-11 在全新目录导入 12/12 份虚构资料，并运行两套问题；全量 Go 测试与 `go vet ./...` 均通过。

| 数据集 | 总通过 | Hit@5 | 来源可核验 | 拒答 |
| --- | ---: | ---: | ---: | ---: |
| 开发集（原 21 条 + H03 回归） | **22/22** | 19/19 | 19/19 | 3/3 |
| 当前保留文件（含已参与修复的 H03） | **12/13** | 11/11 | 10/11 | 2/2 |
| 未参与 H03 修复的 12 条 | **11/12** | 10/10 | 9/10 | 2/2 |

**H13 是已知 bad case：**问题问“星河工作坊活动对象的最低年龄是多少”，检索已命中正确资料，正文写“年满十二周岁”，但词项证据对齐仍拒答。H03“初审时限”曾失败，现已纳入开发集 Q22；不再把它算作独立验证成功。**BUG-10 仍未解决：**Q03“盖什么章”等回答可能带有无关引用。命中和逐字可定位不能证明语义充分，也不能据此宣称真实业务准确率。后续模型精排或语义判定需用独立标注集验证，关系入库仍须规则校验或人工确认。

详细指标、首次失败记录与复现步骤见 [评测结果](eval/RESULTS.md)；工作过程和限制见 [项目记录](project-notes/Plan.md)、[缺陷记录](project-notes/bug.md)。
