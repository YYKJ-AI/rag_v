# ragbot — 可溯源知识库问答

小型独立知识库问答系统：导入 Markdown 资料 → 检索 → 返回**带逐条出处**的回答，妥善处理版本冲突与证据不足。核心链路全部为**确定性实现**（无模型依赖），无密钥即可完成导入、检索、评测与有证据问答。

## 环境要求

- Go 1.22+（构建与测试）
- 可选：Docker（PG + pgvector 后端，默认路径不需要）

## 快速开始

```bash
go build -o ragbot.exe ./cmd/ragbot

# 导入虚构演示资料（docs/ 内 8 份互相关联文档）
./ragbot.exe ingest docs

# 提问（带出处）
./ragbot.exe ask "现在申请要提交哪些材料"

# 时间点查询（历史版本口径）
./ragbot.exe -as-of 2025-06-01 ask "申请要提前多少个工作日提交"

# 运行评测（14 条标注查询）
./ragbot.exe evaluate
```

## 架构

```
入库 ingest：front-matter 打标 → SimHash 文档级指纹（幂等）→ 父子分块
             → 块级 SimHash 去重 → 内存 BM25 倒排（JSON 落盘）
检索 ask：  同义词展开 → BM25（标题/节路径加权）→ 父块聚合 → 废弃版本降权
回答：      证据句对齐抽取 → 冲突三段式（时间窗/替代关系/标注人工）→ 模板组装
评测：      14 条标注查询 → Hit@5 / 有出处比例 / 来源核验 / 拒答正确率 / 回答耗时
```

### 设计决策

| 决策 | 理由 |
| --- | --- |
| 拒答与版本裁决全部确定性 | 模型输出抖动会使分数跨阈值漂移；检索层/规则层判断可复现、可测试 |
| 冲突"只检测不裁决" | 自动裁决错了是静默污染；标注+人工核实错了只是多点一下 |
| 父子分块 | 子块检索命中、父块作引用上下文，excerpt 可人工定位 |
| SimHash 双指纹 | 文档级幂等导入 + 块级召回去重；局部修订指纹不同 → 识别为新版本 |
| 冲突降级为标注 | 时间窗不重叠 = 演变史（各答各版）；重叠+无依据 = conflict 交人工 |
| 标题/节路径参与索引与对齐 | 节标题承载语义（"申请条件"），正文句子可能不含查询词 |
| 是非问句需许可/禁止性证据 | "可以…吗"类问题无明确 modal 表述时拒答，不用无关相似内容拼回答 |

### 已知边界（如实声明）

- 证据句对齐为单字包含度启发式，同义表达依赖同义词表（`internal/tokenize`），未用语义向量；
- 冲突检测针对"同节路径+数值差异+高互含"的数值型冲突，语义级冲突（无数值）不检测；
- 时间点查询用 front-matter 业务日期（effective_date/replaces）推演，不做双时间轴；
- 存储默认 JSON 落盘（data/store.json），PG/pgvector 适配器见 `assets`/migrations（演化位）；
- 评测集 14 条，dev/holdout 未分离（P1 改进项）。

## 评测（基线：docs/ 全量语料）

```
评测结果：14/14 通过 | Hit@5=13/13 | 有出处=13/13 | 来源可核验=13/13 | 拒答=1/1
回答平均耗时≈0.22ms（2026-11-11 Windows 隔离复跑，具体耗时随机器变化）
```

逐条引用、来源版本和耗时快照见 `eval/baseline/reference.json`。复现时运行：

```bash
go run ./cmd/ragbot -data /tmp/ragbot-eval-data ingest docs
go run ./cmd/ragbot -data /tmp/ragbot-eval-data -baseline /tmp/ragbot-eval-report.json evaluate
```

上述 14 条是演示资料上的开发评测，尚无独立保留集。Hit@5 的分母是 13 条有答案案例，每条要求全部 `expect_docs` 均进入前五；有出处率是这 13 条中至少有一条引用的比例。来源可核验率要求所有标注的文档与版本均被引用，且每条引用摘录可在对应检索父块的原文中定位。拒答率的分母是 1 条标注拒答案例，正确拒答要求状态为 `insufficient_evidence` 且没有引用。通过数按逐条 `pass` 汇总；有答案案例必须同时满足状态、Hit@5、来源覆盖及摘录定位。

耗时仅测 `Answer` 调用，每题连续运行 64 次并取单次均值，不含检索指标和引用核验。来源可核验只检查来源完整性和逐字定位，**不等于语义事实正确**；真实业务答案仍需人工核查。快照耗时和生成时间会随环境变化。

## 自动化测试覆盖

```bash
go test ./...
```

覆盖：幂等导入、指纹稳定性、来源定位（citation 字段完整性）、新旧版本关窗替代、
无证据拒答（不捏造出处）、未裁决冲突展示双方来源、as-of 时间点查询（不引用未生效版本）、
重复查询确定性、front-matter 缺字段拒绝导入。

## 安全边界

- 文档正文只作为检索数据，绝不进入指令位（当前实现无 LLM，天然免疫注入；接入 LLM 时正文用分隔符隔离）；
- 密钥只经环境变量提供（当前实现无需任何密钥）；
- 导入失败（缺字段/格式错误）显式报错退出，不静默成功。

## 目录

```
cmd/ragbot/       CLI 入口（ingest / ask / search / evaluate）
internal/
  ingest/         解析、打标、分块、指纹
  store/          内存 + JSON 落盘（PG 适配器预留）
  retriever/      BM25 倒排、父块聚合、版本降权
  engine/         证据抽取、冲突三段式、模板回答、as-of
  evaluate/       评测链路
  tokenize/       中文分词（bigram+unigram）与同义词表
  simhash/        64 位 SimHash（纯标准库）
docs/             虚构演示语料（8 份互相关联文档，均为虚构）
eval/             评测查询集与基线
migrations/       PG schema（演化位）
```

## 开源复用说明

当前实现**零第三方依赖**（纯 Go 标准库）。演化位选型（未引入）：bleve（Apache-2.0）、
gse（MIT）、goldmark（MIT）、mfonda/simhash（MIT）、pgvector（PostgreSQL License）。
