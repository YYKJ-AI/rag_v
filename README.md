# ragbot：可溯源的离线知识库问答

**一句话定位：**默认无需模型的 Go 知识库问答系统；从 Markdown 提取证据，按版本与适用时间回答，证据不足时拒答。另提供可选的云端 Embedding + pgvector + RRF 检索实验。演示数据均为虚构资料。

## 三个核心能力

1. **版本与引用可追溯。**按生效日、失效日、适用范围和明确的替代关系选择版本。新版若明确继承旧版条款，只跟随一跳，并分别列出新版指针和旧版原文的来源；不会把旧文写成新版原文。
2. **确定性证据与拒答。**真实子块检索后，从原文抽取可定位句子；缺少可核验依据时返回 `insufficient_evidence`，不编造引用。HTTP 问答的时间提示按请求独立计算，避免并发串扰。
3. **双轨导入与索引隔离。**默认严格校验 Markdown 元数据；显式使用 `-allow-unstructured` 时，有正文但缺少必要元数据的文件进入待审核搜索索引。待审核资料可搜索，但不会参与正式问答、版本裁决或已认证索引的 BM25 排序。

## 运行

需要 Go 1.25+；默认数据写入 `data/store.json`，离线问答无需 Python、模型或 API 密钥。向量实验使用 `github.com/jackc/pgx/v5` 连接 PostgreSQL/pgvector；默认问答仍可不连接数据库。首次使用建议指定全新数据目录，避免旧演示索引残留。

模型配置样例见 [`.env.example`](.env.example)；真实密钥和 `RAGBOT_PG_DSN` 写入被 Git 忽略的 `.env`。`vector-sync`、`hybrid-search`、`hybrid-evaluate` 读取 Embedding 的 `ENABLED=true`、`PROVIDER=remote_http`、`ENDPOINT_URL`（OpenAI 兼容 `/v1` 基地址或完整 `/embeddings` 地址）、`MODEL`、`API_KEY`、`TIMEOUT_SECONDS` 及 PostgreSQL DSN。`serve` 从本机 `.env` 读取可选 Generator 配置，仅在离线引擎已判为 `answered` 时改写回答文字；校验失败回退原模板。Reranker 配置尚未接入。CLI 的 `ask`、`evaluate` 仍是离线路径。

```bash
go run ./cmd/ragbot -data data/demo ingest docs
go run ./cmd/ragbot -data data/demo ask "现在申请要交哪些材料"
go run ./cmd/ragbot -data data/demo evaluate
go run ./cmd/ragbot -data data/demo -queries eval/holdout.json -baseline data/demo/holdout.json evaluate
go run ./cmd/ragbot -data data/demo serve
go test -count=1 ./...
```

`serve` 默认在 `http://localhost:8080` 提供现有网页和 API。导入非规范资料可用 `go run ./cmd/ragbot -data data/demo -allow-unstructured ingest <目录>`；其来源会标为未认证。需要重新评测时，请清空或更换演示数据目录。

网页的流式接口先接收完整模型响应并检查结束标记与部分机械事实，再一次性发送回答；断流或校验失败只显示模板。模型不能改动服务端确定的状态与引用。明显要求覆盖指令、读取密钥的用户输入会被拒答，含指令样式的引用不会送入模型。这些检查不能判断任意改写是否在语义上充分，也不是完整的提示注入防护。

## 可选向量检索实验

准备 PostgreSQL 与 pgvector 扩展，并在本机 `.env` 配好 Embedding 和 `RAGBOT_PG_DSN`。向量表由命令创建；每个 `-data` 目录默认映射到独立 collection。以下资料和标签均为虚构，和旧评测集分开报告：

```bash
go run ./cmd/ragbot -data data/dense-demo ingest docs-dense
go run ./cmd/ragbot -data data/dense-demo vector-sync
go run ./cmd/ragbot -data data/dense-demo vector-sync
go run ./cmd/ragbot -data data/dense-demo hybrid-search "蓝桥木工场次开启切割设备要办哪些手续"
go run ./cmd/ragbot -data data/dense-demo hybrid-evaluate
```

`vector-sync` 会把已认证资料的子块原文发送到配置的云端 Embedding 接口，仅用于获准提交的资料；重复运行按文本哈希跳过，并清理该 collection 的过期子块。资料更新后需再运行一次同步；检索会排除内容哈希不符的旧向量。检索对照保留 BM25、pgvector 余弦距离和 RRF 三套排名，RRF 在父节层去重，固定 `k=60`；当前是小语料精确向量扫描，没有建立 ANN 索引。`hybrid-search` 输出原文片段、位置与排名，**不会调用 Reranker、Generator，也不会把 RRF 分数当作证据充分性或直接生成答案**。普通 `ask` 仍使用原有离线 BM25 问答链路。

2026-11-12 在隔离目录实际导入 3 份稠密虚构资料（12 节、28 子块），同步首次写入 28 行、再次写入 0 行；8 条人工标注检索题的目标节 Hit@5：首次 BM25 8/8、pgvector 7/8、RRF 8/8，复跑为 8/8、8/8、8/8。D07 的目标节明确说资料没有价格，因此命中该节仍须拒答；不同运行的向量排序也有波动。这是本地小测试床的检索结果，不代表回答支持率或真实资料泛化能力。云端模型与数据库不可用时，向量命令报错；离线问答命令仍可运行。

## 可选语义判定旁路

`semantic-shadow` 从已认证、按业务时间筛选的 BM25 前 N 个节导出完整候选句，绕开当前问答的句子对齐阈值；它只写逐题报告，**不会改变 `ask` 的回答、状态或引用**。先用 `-shadow-candidates-only` 审计候选是否含必要证据；调用模型时复用 `.env` 中已启用的 Generator OpenAI 兼容配置，仅将虚构资料送给获准使用的接口。无配置或判定失败会报错，报告中保留失败题；无模型的导入、问答和评测照常可用。

```bash
go run ./cmd/ragbot -data data/demo ingest docs-dense
go run ./cmd/ragbot -data data/demo -queries eval/gate-regression.json -shadow-pool 8 -shadow-candidates-only semantic-shadow
go run ./cmd/ragbot -data data/demo -queries eval/gate-regression.json -shadow-pool 8 semantic-shadow
```

开发、保留和现场集须分开运行并指定不同的 `-shadow-report` 路径，否则默认报告会被覆盖。旁路当前只使用问句内时间和默认未指定适用范围；传入 `-as-of` 或 `-scope-key` 会明确报错。模型只可选择报告中已有的候选 ID；报告分别记录候选缺失、必要证据组覆盖、引用支持、无证据题误答、证据不合格的回答、安全拦截和模型失败。单次结果不能证明模型可靠；RRF 与 Reranker 仍未接入普通问答。

## 实测结果与已知边界

2026-11-12 在全新目录导入 12/12 份虚构资料并重新运行两套问题；`go test -count=1 ./...` 和 `go vet ./...` 均通过。新口径给每条有答案的问题标注必要证据片段，逐条检查引用是否属于人工确认的支持证据；旧口径只检查来源及摘录可定位。

| 数据集 | 新口径逐题通过 | Hit@5 | 来源可核验 | 证据合格题 | 引用支持 | 拒答 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 开发集（新增同主题缺字段负例 Q23） | **13/23** | 19/19 | 19/19 | 10/19 | 22/42 | 3/4 |
| 当前保留文件（含已参与修复的 H03） | **8/13** | 11/11 | 10/11 | 6/11 | 11/18 | 2/2 |

**已知失败：**Q23 问星河工作坊报名费用，资料未记载，系统却引用入场券领取并误答（BUG-13）。Q03“盖什么章”没有引用实际写明公章的句子；Q06 漏掉安全许可条件，多题包含无关引用（BUG-10）。新稠密语料也复现“升级材料包多少钱”在无价格时被标为 `answered`，以及报名费、木工手续问题混入无关引用；这些发生在未接入 RRF 的离线 `ask` 路径，仍需单独修复。H13“最低年龄”命中正确资料后仍拒答（BUG-12）。H03“初审时限”曾用于修复并转入开发集，不能当作独立验证成功。旧口径下开发集 22/22、保留文件 12/13 是历史结果；新数字来自更严格的证据标注和新增负例，并非本次修改了问答策略。词表与阈值本轮未改，不能据此宣称真实业务准确率。

2026-11-13 新增 [14 条现场回归题](eval/gate-regression.json)：在隔离目录导入旧 12 份与稠密 3 份虚构资料后，严格口径从 1/14 变为 **5/14**；Hit@5 8/9、必要证据合格 2/9、引用支持 4/20、正确拒答 3/5。“恰好四十人”仅在同节相邻规则明确写出人数与所问职责时走窄范围离线兜底；直接越权输入三题拒答。未来版本日期、跨主题经费、年龄句及多要件引用等仍失败。该数据集在实现过程中使用，不是独立保留集；旧 12 份资料单独复跑仍为开发集 13/23、保留文件 8/13。

逐条结果、指标定义与复现步骤见 [评测结果](eval/RESULTS.md)；完整快照在 `eval/baseline/offline-evidence-*.json`。本地工作记录保存在被 Git 忽略的 `project-notes/`。
