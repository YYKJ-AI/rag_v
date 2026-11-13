// Package server HTTP 服务模式：静态页 + JSON API（纯标准库，无框架）。
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ragbot/internal/engine"
	"ragbot/internal/generator"
	"ragbot/internal/retriever"
	"ragbot/internal/safety"
	"ragbot/internal/types"
)

// IngestResult 文档导入结果（确定性入库 + 可选向量同步状态）。
type IngestResult struct {
	Action     string `json:"action"`        // created / duplicate
	DocID      string `json:"doc_id"`
	Version    string `json:"version"`
	Title      string `json:"title"`
	Sections   int    `json:"sections"`
	Chunks     int    `json:"chunks"`
	VectorSync string `json:"vector_sync"` // synced … / offline … / error: …
	SavedAs    string `json:"saved_as,omitempty"`
}

// Deps 服务依赖（main 组装后注入）
type Deps struct {
	Engine    *engine.Engine
	Ret       *retriever.Retriever
	WebDir    string
	Generator *generator.Gateway // 可为 nil（未启用模型时走模板路径）
	// IngestDocument 文档导入回调（main 实现：确定性解析入库 + 在线时向量化入 PG）。
	// nil 时 /api/ingest 返回 501。
	IngestDocument func(filename string, raw []byte) (*IngestResult, error)
}

// Handler 构建路由
func Handler(d Deps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "time": time.Now().Format(time.RFC3339)})
	})

	// 模型开关（运行时启停）：用于测试"模型断线/关闭"的真回退，
	// 不需要重启进程或手改环境变量。
	mux.HandleFunc("GET /api/generator/status", func(w http.ResponseWriter, r *http.Request) {
		if d.Generator == nil {
			writeJSON(w, map[string]any{"enabled": false, "configured": false})
			return
		}
		provider, model, configured := d.Generator.Info()
		writeJSON(w, map[string]any{
			"enabled": d.Generator.Enabled(), "configured": configured,
			"provider": provider, "model": model,
		})
	})
	mux.HandleFunc("POST /api/generator/toggle", func(w http.ResponseWriter, r *http.Request) {
		if d.Generator == nil {
			httpError(w, http.StatusConflict, "模型表述层未配置（.env RAGBOT_GENERATOR_* 缺失），无法切换")
			return
		}
		var req struct {
			Enabled *bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Enabled == nil {
			httpError(w, http.StatusBadRequest, "请求体应为 {\"enabled\": true|false}")
			return
		}
		d.Generator.SetEnabled(*req.Enabled)
		provider, model, _ := d.Generator.Info()
		writeJSON(w, map[string]any{"enabled": d.Generator.Enabled(), "provider": provider, "model": model})
	})

	// 文档导入入口：上传 Markdown → 确定性解析/切分入库 →
	// 在线（向量环境已配置且 PG 可达）时同步向量化入 pgvector；离线时明确标注跳过。
	mux.HandleFunc("POST /api/ingest", func(w http.ResponseWriter, r *http.Request) {
		if d.IngestDocument == nil {
			httpError(w, http.StatusNotImplemented, "导入回调未配置")
			return
		}
		if err := r.ParseMultipartForm(4 << 20); err != nil {
			httpError(w, http.StatusBadRequest, "请以 multipart/form-data 上传（字段名 file）")
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			httpError(w, http.StatusBadRequest, "缺少上传文件字段 file")
			return
		}
		defer file.Close()
		name := header.Filename
		if !(strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".markdown")) {
			httpError(w, http.StatusBadRequest, "仅支持 .md / .markdown 文件")
			return
		}
		raw, err := io.ReadAll(io.LimitReader(file, 2<<20))
		if err != nil {
			httpError(w, http.StatusBadRequest, "读取上传内容失败")
			return
		}
		if len(raw) == 0 {
			httpError(w, http.StatusBadRequest, "上传文件为空")
			return
		}
		res, err := d.IngestDocument(name, raw)
		if err != nil {
			httpError(w, http.StatusBadRequest, "导入失败："+err.Error())
			return
		}
		writeJSON(w, res)
	})

	mux.HandleFunc("POST /api/ask", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query    string `json:"query"`
			AsOf     string `json:"as_of"` // 可选 "2006-01-02"
			ScopeKey string `json:"scope_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "请求体不是合法 JSON")
			return
		}
		req.Query = strings.TrimSpace(req.Query)
		if req.Query == "" {
			httpError(w, http.StatusBadRequest, "问题不能为空")
			return
		}
		var asOf *time.Time
		if req.AsOf != "" {
			t, err := time.Parse("2006-01-02", req.AsOf)
			if err != nil {
				httpError(w, http.StatusBadRequest, "as_of 格式应为 2006-01-02")
				return
			}
			asOf = &t
		}
		start := time.Now()
		ans := d.Engine.AnswerWithScope(req.Query, asOf, req.ScopeKey)
		resp := map[string]any{
			"status":       ans.Status,
			"answer":       ans.Answer,
			"citations":    ans.Citations,
			"latency_ms":   time.Since(start).Milliseconds(),
			"generated_by": "template",
		}
		if asOf == nil && engine.QueryHasInferredTime(req.Query) {
			resp["detected_time"] = req.Query // 时间点由问句文本推断（正则命中）
		}
		writeJSON(w, resp)
	})

	mux.HandleFunc("GET /api/search", func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			httpError(w, http.StatusBadRequest, "缺少查询参数 q")
			return
		}
		n := 8
		if v := r.URL.Query().Get("n"); v != "" {
			if x, err := strconv.Atoi(v); err == nil && x > 0 && x <= 50 {
				n = x
			}
		}
		hits := d.Ret.Search(q, n)
		type hitOut struct {
			DocID            string   `json:"doc_id"`
			Version          string   `json:"version"`
			Path             string   `json:"section_path"`
			Score            float64  `json:"score"`
			Excerpt          string   `json:"excerpt"`
			SourceStatus     string   `json:"source_status"`
			SourceName       string   `json:"source_name,omitempty"`
			MetadataWarnings []string `json:"metadata_warnings,omitempty"`
		}
		out := make([]hitOut, 0, len(hits))
		for _, h := range hits {
			exc := h.Section.Text
			if len([]rune(exc)) > 80 {
				exc = string([]rune(exc)[:80]) + "…"
			}
			item := hitOut{DocID: h.Section.DocID, Version: h.Section.Version, Path: h.Section.SectionPath, Score: h.Score, Excerpt: exc, SourceStatus: "verified"}
			for _, doc := range d.Ret.Docs() {
				if doc.ID == h.Section.DocID && doc.Version == h.Section.Version && (doc.Status == "needs_review" || doc.IdentityKind == "synthetic") {
					item.SourceStatus = "unverified"
					item.SourceName = doc.SourceName
					item.MetadataWarnings = doc.MetadataWarnings
					break
				}
			}
			out = append(out, item)
		}
		writeJSON(w, map[string]any{"hits": out})
	})

	// SSE 问答：先验证完整模型表述，再输出状态、引用和正文。
	// 模型失败或越过机械事实边界时，只发送确定性模板，不暴露半截输出。
	mux.HandleFunc("POST /api/ask/stream", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query    string `json:"query"`
			AsOf     string `json:"as_of"`
			ScopeKey string `json:"scope_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "请求体不是合法 JSON")
			return
		}
		req.Query = strings.TrimSpace(req.Query)
		if req.Query == "" {
			httpError(w, http.StatusBadRequest, "问题不能为空")
			return
		}
		var asOf *time.Time
		if req.AsOf != "" {
			t, err := time.Parse("2006-01-02", req.AsOf)
			if err != nil {
				httpError(w, http.StatusBadRequest, "as_of 格式应为 2006-01-02")
				return
			}
			asOf = &t
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			httpError(w, http.StatusInternalServerError, "当前连接不支持流式输出")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		send := func(event string, payload any) {
			data, _ := json.Marshal(payload)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
			flusher.Flush()
		}

		start := time.Now()

		// 查询预处理层（在线）：模型对 query 做改写/实体抽取/子问题拆分/时间提取，
		// 产出结构化检索计划；失败或离线时回退原问题直检（正则时间推断兜底）。
		// 预处理只影响检索候选，不参与证据门控与裁决。
		type planInfo struct {
			Source     string   `json:"source"` // model / offline
			Entities   []string `json:"entities,omitempty"`
			SubQueries []string `json:"sub_queries,omitempty"`
			Time       string   `json:"time,omitempty"`
			TimePrec   string   `json:"time_precision,omitempty"` // day / month / year
		}
		plan := planInfo{Source: "offline"}
		if d.Generator != nil && d.Generator.Enabled() && !safety.IsDirectOverride(req.Query) {
			pctx, pcancel := context.WithTimeout(r.Context(), 12*time.Second)
			p, perr := d.Generator.PreprocessQuery(pctx, req.Query)
			pcancel()
			if perr == nil {
				plan = planInfo{Source: "model", Entities: p.Entities, SubQueries: p.SubQueries, Time: p.Time, TimePrec: p.TimePrec}
			}
		}
		// 时间锚点裁决（确定性优先）：正则能解析的表达式（去年/年份/年月/完整日期）
		// 一律用正则结果——模型在相对时间上已两次实测翻车（猜错年份/精度）；
		// 仅当正则解析不出时才采用模型提取（按精度换算锚点），覆盖"上个月"类模糊表达。
		if req.AsOf == "" {
			if rt := engine.InferQueryTime(req.Query); rt != nil {
				asOf = rt
			} else if plan.Source == "model" && plan.Time != "" {
				if t, terr := time.Parse("2006-01-02", plan.Time); terr == nil {
					anchor := generator.AnchorTime(t, plan.TimePrec)
					asOf = &anchor
				}
			}
		}

		ans := d.Engine.AnswerWithScopeSub(req.Query, asOf, req.ScopeKey, plan.SubQueries)

		visible := ans.Answer
		generatedBy := "template"
		failureClass := "" // render_fallback=生成层失败降级 / judge_miss=判定层漏判信号 / 空=无降级
		var fallbackReason string
		// 拒答和冲突不调用模型；完整模型输出验证通过后才能到达客户端。
		if ans.Status == "answered" && d.Generator != nil && d.Generator.Enabled() {
			evidence := make([]string, 0, len(ans.Citations))
			for _, c := range ans.Citations {
				evidence = append(evidence, c.Excerpt)
			}
			unsafeEvidence := false
			for _, excerpt := range evidence {
				if safety.IsDirectOverride(excerpt) {
					unsafeEvidence = true
					break
				}
			}
			if unsafeEvidence {
				fallbackReason = "引用资料包含指令样式文本，已停用模型改写"
				failureClass = "render_fallback"
			} else {
				ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
				defer cancel()
				llmText, err := d.Generator.StreamAnswer(ctx, req.Query, ans.Answer, evidence, nil)
				if err == nil {
					err = d.Generator.ValidateRewrite(ans.Answer, evidence, llmText)
				}
				if err != nil && !errors.Is(err, generator.ErrStatusChanged) {
					// 表述校验未通过（或瞬时网络抖动）：把核验失败原因反馈给模型重试一次——
					// "回退能回答的题，模型也应该能回答"，先给模型修正机会，而不是直接降级。
					retryHint := "上一次改写未通过事实核验：" + err.Error() +
						"。请严格贴近\"既有回答\"的原文表述，仅做流畅性微调；保留全部关键数字、版本与条件；不要概括、不要省略条款、不要使用\"等\"类宽泛表述。只输出改写后的正文。"
					llmText, err = d.Generator.StreamAnswerWithHint(ctx, req.Query, ans.Answer, evidence, retryHint, nil)
					if err == nil {
						err = d.Generator.ValidateRewrite(ans.Answer, evidence, llmText)
					}
				}
				if err != nil && errors.Is(err, generator.ErrStatusChanged) {
					// 模型认为证据不支持所问事项——判定层漏判信号（judge_miss）。
					// 不得回退到疑似误答的模板：转为拒答并记录，进评测集归因。
					ans = types.Answer{
						Status: "insufficient_evidence",
						Answer: "语义判定层复核后认为候选证据不支持所问事项（已记录 judge_miss 待归因），无法给出有出处的回答。",
					}
					visible = ans.Answer // 同步清除旧模板正文，避免拒答携带疑似误答内容
					failureClass = "judge_miss"
					fallbackReason = "生成模型判定证据不支持所问事项，已转为拒答"
				} else if err != nil {
					if errors.Is(err, generator.ErrValidationFailed) {
						fallbackReason = "模型表述两次未通过事实核验，已回退确定性模板"
					} else {
						fallbackReason = "模型不可用（网络或上游异常），已回退确定性模板"
					}
					failureClass = "render_fallback"
				} else {
					visible = llmText
					generatedBy = "llm"
				}
			}
		}
		send("meta", map[string]any{
			"status": ans.Status, "citations": ans.Citations,
			"latency_ms":    time.Since(start).Milliseconds(),
			"detected":      asOf == nil && engine.QueryHasInferredTime(req.Query),
			"generated_by":  generatedBy,
			"failure_class": failureClass,
			"plan":          plan,
		})
		send("delta", map[string]any{"text": visible})
		if fallbackReason != "" {
			send("notice", map[string]any{"fallback_reason": fallbackReason})
		}
		send("done", map[string]any{
			"total_ms":      time.Since(start).Milliseconds(),
			"generated_by":  generatedBy,
			"failure_class": failureClass,
			"plan_source":   plan.Source,
		})
	})

	// 静态页
	fs := http.FileServer(http.Dir(d.WebDir))
	mux.Handle("GET /", fs)
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
