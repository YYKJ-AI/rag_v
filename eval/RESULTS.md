# 离线问答评测结果（2026-11-12）

## 2026-11-13：现场问题固定回归

`eval/gate-regression.json` 含 14 条现场及相邻问法；隔离索引依次导入 `docs/` 12 份和 `docs-dense/` 3 份。修复前严格逐题 1/14，修复后 5/14；修复后 Hit@5 8/9、必要证据合格 2/9、引用支持 4/20、正确拒答 3/5。通过 G07、G08、G12–G14。G08 的数字条件与所问职责必须由同节相邻原文共同确认；G12–G14 是直接越权输入，不计为有证据回答。其余 9 题保留失败状态，包括 G09 未知未来发布时间误答、G10 经费跨主题引用、G11 年龄证据误选。仅修复机械安全边界和一类明确的条件例外，没有接入 RRF 或 Reranker 到 `ask`。

```bash
go run ./cmd/ragbot -data data/gate-demo ingest docs
go run ./cmd/ragbot -data data/gate-demo ingest docs-dense
go run ./cmd/ragbot -data data/gate-demo -queries eval/gate-regression.json -baseline data/gate-demo/result.json evaluate
```

旧 12 份资料独立索引复跑保持开发集 13/23、保留文件 8/13；两套测试床不混报。

## 本轮证据支持指标

**数据：**全新隔离目录导入 `docs/` 12 份虚构资料。问答代码、词表和阈值未修改。开发集增加 Q23“星河工作坊报名费用是多少？”作为同主题缺字段负例；原 H13 留在保留集。全部 19 条开发集和 11 条保留集有答案题均标注必要证据，部分允许引用的额外支持句标为可选。逐条标签在 `eval/queries.json` 和 `eval/holdout.json`；完整结果在 `eval/baseline/offline-evidence-dev.json` 和 `eval/baseline/offline-evidence-holdout.json`。

| 数据集 | 严格逐题通过 | Hit@5 | 来源可定位 | 证据合格题 | 引用支持 | 拒答 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 开发集 | **13/23** | 19/19 | 19/19 | 10/19 | 22/42 | 3/4 |
| 保留文件（含已参与修复的 H03） | **8/13** | 11/11 | 10/11 | 6/11 | 11/18 | 2/2 |

**指标定义：**Hit@5 要求标注来源全部进入前五。“来源可定位”要求预期文档与版本被引用、每条摘录能在对应节找到。证据合格题要求所有人工标注的必要证据被引用，且没有任何不支持问题的额外引用。“引用支持”逐条核对引用的文档、版本、位置与摘录片段是否匹配人工确认的支持证据。拒答要求 `insufficient_evidence` 且零引用。严格逐题通过须满足该题全部适用指标。人工标签决定何为支持，程序只负责精确核对；这不是未见资料的语义准确率。耗时为每题 64 次回答调用均值，不含导入和评测检查；本轮约 0.396ms / 0.459ms。

**逐条结果：**开发集通过 Q02、Q09–Q12、Q15–Q22；失败 Q01、Q03–Q08、Q13–Q14、Q23。保留文件通过 H03–H04、H07–H12；失败 H01–H02、H05–H06、H13。Q03 没有引用写明公章的原句，四条引用均不支持所问事项；Q06 漏掉安全许可条件；Q23 没有费用依据却引用入场券领取句并回答；H13 命中正确文档后仍拒答。其余失败多为无关附带引用。H03 曾参与修复，不计为独立验证成功。BUG-10、BUG-12、BUG-13 均保持开放。

```bash
go test -count=1 ./...
go run ./cmd/ragbot -data data/audit ingest docs
go run ./cmd/ragbot -data data/audit -baseline data/audit/dev.json evaluate
go run ./cmd/ragbot -data data/audit -queries eval/holdout.json -baseline data/audit/holdout.json evaluate
```

## 历史：仅核对来源和定位的旧口径（2026-11-11）

旧口径开发集 22/22、当前保留文件 12/13，只核对命中、出处和摘录位置。最初保留集 H03 失败，修复后转入开发回归；H13 继续失败。原始逐条快照保留于 `eval/baseline/reference.json`、`eval/baseline/holdout-initial.json` 和 `eval/baseline/holdout.json`，未覆盖或改写。这些历史数字不能与本轮严格证据指标直接比较。
