// ragbot CLI：ingest / ask / evaluate 三个能力（任务书建议接口）。
// 无密钥可完整运行：导入、检索、评测、有证据问答全走确定性路径。
package main

import (
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
	"ragbot/internal/ingest"
	"ragbot/internal/retriever"
	"ragbot/internal/server"
	"ragbot/internal/store"
)

func main() {
	dataDir := flag.String("data", "data", "数据目录（store.json 落盘位置）")
	docsDir := flag.String("docs", "docs", "文档目录")
	queries := flag.String("queries", "eval/queries.json", "评测查询集")
	baseline := flag.String("baseline", "eval/baseline/latest.json", "评测结果输出")
	asOfStr := flag.String("as-of", "", "时间点查询（2006-01-02）")
	addrFlag := flag.String("addr", ":8080", "serve 模式监听地址")
	webDir := flag.String("web", "web", "静态页目录")
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
		doIngest(st, ret, dir)
	case "ask":
		q := flag.Arg(1)
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
		ans := e.Answer(q, asOf)
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
		q := flag.Arg(1)
		if q == "" {
			die(fmt.Errorf("用法：ragbot search \"查询\""))
		}
		for i, h := range ret.Search(q, 8) {
			fmt.Printf("%d. %.3f  %s@%s  %s\n", i+1, h.Score, h.Section.DocID, h.Section.Version, h.Section.SectionPath)
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
	default:
		fmt.Println(`ragbot — 可溯源知识库问答

用法：
  ragbot ingest [目录]            导入 Markdown 文档（默认 docs/）
  ragbot ask "问题" [-as-of 日期]  提问，返回带出处的回答
  ragbot evaluate                 运行评测集，输出指标与逐条结果

参数：
  -data 目录    数据落盘位置（默认 data/）
  -docs 目录    文档目录（默认 docs/）
  -queries 文件 评测查询集（默认 eval/queries.json）`)
	}
}

// ---- 组装辅助 ----

func reloadIndex(ret *retriever.Retriever, st *store.Store) {
	ret.Index(st.Docs, st.Sections, st.Chunks)
}

func newEngine(ret *retriever.Retriever) *engine.Engine {
	return engine.New(ret)
}

func doIngest(st *store.Store, ret *retriever.Retriever, dir string) {
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
			die(fmt.Errorf("%s: %w", ent.Name(), err))
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
		n++
	}
	if err := st.Save(); err != nil {
		die(err)
	}
	reloadIndex(ret, st)
	fmt.Printf("完成：处理 %d 个文件，索引 %d 份文档\n", n, len(st.Docs))
}

func printReport(rep *evaluate.Report) {
	fmt.Printf("评测结果：%d/%d 通过 | Hit@5=%.2f | 有出处=%.2f | 拒答=%v | 平均耗时=%.1fms\n",
		rep.Passed, rep.Total, rep.Hit5Rate, rep.CitedRate, rep.RefusalOK, rep.AvgLatencyMs)
	for _, r := range rep.Records {
		mark := "✓"
		if !r.Pass {
			mark = "✗"
		}
		fmt.Printf("  %s %-4s %-22s hit5=%-5v cited=%-5v %dms  %s\n",
			mark, r.ID, r.Status, r.Hit5, r.Cited, r.LatencyMs, r.Query)
	}
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "错误：", err)
	os.Exit(1)
}
