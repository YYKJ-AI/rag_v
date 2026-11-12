// ragbot CLI：ingest / ask / evaluate 三个能力（任务书建议接口）。
// 无密钥可完整运行：导入、检索、评测、有证据问答全走确定性路径。
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ragbot/internal/engine"
	"ragbot/internal/evaluate"
	"ragbot/internal/hybrid"
	"ragbot/internal/ingest"
	"ragbot/internal/modelconfig"
	"ragbot/internal/remoteembed"
	"ragbot/internal/retriever"
	"ragbot/internal/server"
	"ragbot/internal/store"
	"ragbot/internal/types"
	"ragbot/internal/vectorstore"
)

func main() {
	dataDir := flag.String("data", "data", "数据目录（store.json 落盘位置）")
	docsDir := flag.String("docs", "docs", "文档目录")
	queries := flag.String("queries", "eval/queries.json", "评测查询集")
	retrievalQueries := flag.String("retrieval-queries", "eval/dense-retrieval.json", "混合检索评测查询集")
	baseline := flag.String("baseline", "eval/baseline/latest.json", "评测结果输出")
	asOfStr := flag.String("as-of", "", "时间点查询（2006-01-02）")
	scopeKey := flag.String("scope-key", "", "适用范围键（文档 scope_keys 中的值）")
	allowUnstructured := flag.Bool("allow-unstructured", false, "允许正文型 Markdown 以待审核身份降级入库")
	addrFlag := flag.String("addr", ":8080", "serve 模式监听地址")
	webDir := flag.String("web", "web", "静态页目录")
	envFile := flag.String("env", ".env", "向量实验的本机环境文件")
	collectionFlag := flag.String("collection", "", "pgvector collection（默认按数据目录隔离）")
	pool := flag.Int("pool", 20, "混合检索每路候选节数")
	flag.Parse()

	if err := os.MkdirAll(*dataDir, 0755); err != nil {
		die(err)
	}
	st := store.New(filepath.Join(*dataDir, "store.json"))
	if err := st.Load(); err != nil {
		die(err)
	}
	ret := retriever.New()
	reloadIndex(ret, st)

	cmd := flag.Arg(0)
	switch cmd {
	case "ingest":
		dir := flag.Arg(1)
		if dir == "" {
			dir = *docsDir
		}
		doIngest(st, ret, dir, *allowUnstructured)
	case "ask":
		q := strings.TrimSpace(flag.Arg(1))
		if q == "" {
			die(fmt.Errorf("用法：ragbot ask \"你的问题\""))
		}
		var asOf *time.Time
		if *asOfStr != "" {
			t, err := time.Parse("2006-01-02", *asOfStr)
			if err != nil {
				die(fmt.Errorf("as-of 格式应为 2006-01-02: %w", err))
			}
			asOf = &t
		}
		e := newEngine(ret)
		ans := e.AnswerWithScope(q, asOf, *scopeKey)
		b, _ := json.MarshalIndent(ans, "", "  ")
		fmt.Println(string(b))
	case "serve":
		addr := *addrFlag
		srv := server.Handler(server.Deps{Engine: newEngine(ret), Ret: ret, WebDir: *webDir})
		fmt.Printf("ragbot 已启动: http://localhost%s （Ctrl+C 停止）\n", addr)
		if err := http.ListenAndServe(addr, srv); err != nil {
			die(err)
		}
	case "search":
		q := strings.TrimSpace(flag.Arg(1))
		if q == "" {
			die(fmt.Errorf("用法：ragbot search \"查询\""))
		}
		for i, h := range ret.Search(q, 8) {
			status := ""
			for _, doc := range st.Docs {
				if doc.ID == h.Section.DocID && doc.Version == h.Section.Version && (doc.Status == "needs_review" || doc.IdentityKind == "synthetic") {
					status = " [未认证来源: " + doc.SourceName + "]"
					break
				}
			}
			fmt.Printf("%d. %.3f  %s@%s  %s%s\n", i+1, h.Score, h.Section.DocID, h.Section.Version, h.Section.SectionPath, status)
		}
	case "evaluate":
		qs, err := evaluate.LoadQueries(*queries)
		if err != nil {
			die(err)
		}
		e := newEngine(ret)
		rep := evaluate.Run(e, qs, 5)
		if err := rep.Save(*baseline); err != nil {
			die(err)
		}
		printReport(rep)
	case "vector-sync":
		svc, collection, err := newHybridService(*envFile, *dataDir, *collectionFlag, ret)
		if err != nil {
			die(err)
		}
		defer svc.Vectors.Close(context.Background())
		stats, err := svc.Sync(context.Background(), st.Docs, st.Sections, st.Chunks)
		if err != nil {
			die(err)
		}
		fmt.Printf("向量同步 collection=%s eligible=%d embedded=%d skipped=%d pruned=%d rows=%d\n",
			collection, stats.Eligible, stats.Embedded, stats.Skipped, stats.Pruned, stats.Rows)
	case "hybrid-search":
		q := strings.TrimSpace(flag.Arg(1))
		if q == "" {
			die(fmt.Errorf("用法：ragbot hybrid-search \"查询\""))
		}
		svc, _, err := newHybridService(*envFile, *dataDir, *collectionFlag, ret)
		if err != nil {
			die(err)
		}
		defer svc.Vectors.Close(context.Background())
		lexical, semantic, fused, err := svc.Search(context.Background(), q, *pool, 8)
		if err != nil {
			die(err)
		}
		printHits("BM25", lexical, 8)
		printHits("pgvector (cosine distance; lower is nearer)", semantic, 8)
		printHits("RRF", fused, 8)
	case "hybrid-evaluate":
		svc, _, err := newHybridService(*envFile, *dataDir, *collectionFlag, ret)
		if err != nil {
			die(err)
		}
		defer svc.Vectors.Close(context.Background())
		if err := evaluateHybridRetrieval(context.Background(), svc, *retrievalQueries, *pool); err != nil {
			die(err)
		}
	default:
		fmt.Println(`ragbot — 可溯源知识库问答

用法：
  ragbot ingest [目录]            导入 Markdown 文档（默认 docs/）
  ragbot -as-of 日期 -scope-key 键 ask "问题"  按时间和适用范围提问（参数可省略）
  ragbot evaluate                 运行评测集，输出指标与逐条结果
  ragbot vector-sync              生成子块向量并幂等写入 pgvector
  ragbot hybrid-search "问题"       对照 BM25、pgvector 与 RRF 排名
  ragbot hybrid-evaluate          按 eval/dense-retrieval.json 检索召回对照

参数：
  -data 目录    数据落盘位置（默认 data/）
  -docs 目录    文档目录（默认 docs/）
  -queries 文件 评测查询集（默认 eval/queries.json）
  -scope-key 键  仅使用标注该适用范围键或 * 的文档`)
	}
}

func newHybridService(envFile, dataDir, collection string, ret *retriever.Retriever) (*hybrid.Service, string, error) {
	cfg, err := modelconfig.LoadVector(envFile)
	if err != nil {
		return nil, "", err
	}
	if collection == "" {
		absolute, err := filepath.Abs(dataDir)
		if err != nil {
			return nil, "", err
		}
		sum := sha256.Sum256([]byte(strings.ToLower(absolute)))
		collection = "ragbot-" + hex.EncodeToString(sum[:8])
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	vectors, err := vectorstore.Open(ctx, cfg.DSN, collection, cfg.Embedding.Model)
	if err != nil {
		return nil, "", err
	}
	return &hybrid.Service{Embeddings: remoteembed.New(cfg.Embedding), Vectors: vectors, Retriever: ret}, collection, nil
}

func printHits(label string, hits []types.Hit, limit int) {
	fmt.Println(label + ":")
	for i, hit := range hits {
		if i >= limit {
			break
		}
		excerpt := []rune(strings.ReplaceAll(hit.Chunk.Text, "\n", " "))
		if len(excerpt) > 80 {
			excerpt = excerpt[:80]
		}
		fmt.Printf("  %d. %.5f %s %s (%s) %q\n", i+1, hit.Score,
			hit.Section.ID, hit.Section.SectionPath, hit.Chunk.ID, string(excerpt))
	}
}

func evaluateHybridRetrieval(ctx context.Context, svc *hybrid.Service, path string, pool int) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cases []struct {
		ID             string   `json:"id"`
		Query          string   `json:"query"`
		ExpectSections []string `json:"expect_sections"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		return err
	}
	if len(cases) == 0 {
		return fmt.Errorf("检索评测集为空")
	}
	var bm25Passed, vectorPassed, fusedPassed int
	for _, tc := range cases {
		if tc.ID == "" || strings.TrimSpace(tc.Query) == "" || len(tc.ExpectSections) == 0 {
			return fmt.Errorf("检索评测条目缺少 id、query 或 expect_sections")
		}
		lexical, semantic, fused, err := svc.Search(ctx, tc.Query, pool, 5)
		if err != nil {
			return fmt.Errorf("%s 检索失败: %w", tc.ID, err)
		}
		bm25 := containsAllSections(lexical, tc.ExpectSections, 5)
		vector := containsAllSections(semantic, tc.ExpectSections, 5)
		rrf := containsAllSections(fused, tc.ExpectSections, 5)
		if bm25 {
			bm25Passed++
		}
		if vector {
			vectorPassed++
		}
		if rrf {
			fusedPassed++
		}
		fmt.Printf("%s BM25=%t vector=%t RRF=%t %s\n", tc.ID, bm25, vector, rrf, tc.Query)
	}
	fmt.Printf("检索目标节 Hit@5: BM25 %d/%d, vector %d/%d, RRF %d/%d（仅召回；D07 目标节说明缺少价格，不代表可回答）\n",
		bm25Passed, len(cases), vectorPassed, len(cases), fusedPassed, len(cases))
	return nil
}

func containsAllSections(hits []types.Hit, expected []string, top int) bool {
	if len(expected) == 0 {
		return false
	}
	seen := make(map[string]bool, top)
	for i, hit := range hits {
		if i >= top {
			break
		}
		seen[hit.Section.ID] = true
	}
	for _, id := range expected {
		if !seen[id] {
			return false
		}
	}
	return true
}

// ---- 组装辅助 ----

func reloadIndex(ret *retriever.Retriever, st *store.Store) {
	ret.Index(st.Docs, st.Sections, st.Chunks)
}

func newEngine(ret *retriever.Retriever) *engine.Engine {
	return engine.New(ret)
}

func doIngest(st *store.Store, ret *retriever.Retriever, dir string, allowUnstructured bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		die(err)
	}
	n := 0
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".md") {
			continue
		}
		if ent.Name() == "README.md" {
			continue // 目录说明文件不是资料，跳过
		}
		raw, err := os.ReadFile(filepath.Join(dir, ent.Name()))
		if err != nil {
			die(err)
		}
		meta, drafts, err := ingest.Parse(raw)
		if err != nil {
			if !allowUnstructured {
				die(fmt.Errorf("%s: %w", ent.Name(), err))
			}
			meta, drafts, err = ingest.ParseUnstructured(raw, ent.Name())
			if err != nil {
				die(fmt.Errorf("%s: %w", ent.Name(), err))
			}
		}
		doc, secs, chunks, err := ingest.Build(meta, drafts)
		if err != nil {
			die(fmt.Errorf("%s: %w", ent.Name(), err))
		}
		action, err := st.Ingest(doc, secs, chunks)
		if err != nil {
			die(err)
		}
		fmt.Printf("  %-20s %s (%d 节 / %d 块)\n", ent.Name(), action, len(secs), len(chunks))
		if doc.IdentityKind == "synthetic" && action == "created" {
			fmt.Printf("    待审核：%s@%s；原因：%s\n", doc.ID, doc.Version, strings.Join(doc.MetadataWarnings, ", "))
		}
		n++
	}
	if err := st.Save(); err != nil {
		die(err)
	}
	reloadIndex(ret, st)
	fmt.Printf("完成：处理 %d 个文件，索引 %d 份文档\n", n, len(st.Docs))
}

func printReport(rep *evaluate.Report) {
	fmt.Printf("评测结果：%d/%d 通过 | Hit@5=%d/%d (%.2f) | 有出处=%d/%d (%.2f) | 来源可核验=%d/%d (%.2f) | 必要证据完整且无无关引用=%d/%d (%.2f) | 引用支持=%d/%d (%.2f) | 拒答=%d/%d (%.2f) | 回答平均耗时=%.3fms\n",
		rep.Passed, rep.Total,
		rep.Hit5Passed, rep.Hit5Total, rep.Hit5Rate,
		rep.CitedPassed, rep.CitedTotal, rep.CitedRate,
		rep.SourcePassed, rep.SourceTotal, rep.SourceRate,
		rep.EvidencePassed, rep.EvidenceTotal, rep.EvidenceRate,
		rep.SupportedCitations, rep.ScoredCitations, rep.CitationPrecision,
		rep.RefusalPassed, rep.RefusalTotal, rep.RefusalRate, rep.AvgLatencyMs)
	for _, r := range rep.Records {
		mark := "✓"
		if !r.Pass {
			mark = "✗"
		}
		if r.ExpectedStatus == "insufficient_evidence" {
			fmt.Printf("  %s %-4s %-22s refusal=%-5v %.3fms  %s\n",
				mark, r.ID, r.Status, r.Refusal, r.LatencyMs, r.Query)
			continue
		}
		fmt.Printf("  %s %-4s %-22s hit5=%-5v cited=%-5v source=%-5v located=%-5v evidence=%-5v support=%d/%d %.3fms  %s\n",
			mark, r.ID, r.Status, r.Hit5, r.Cited, r.SourceCoverage, r.CitationsLocated, r.EvidenceCoverage, r.SupportedCitations, r.ScoredCitations, r.LatencyMs, r.Query)
	}
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "错误：", err)
	os.Exit(1)
}
