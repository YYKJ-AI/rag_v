// Package server HTTP 服务模式：静态页 + JSON API（纯标准库，无框架）。
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ragbot/internal/engine"
	"ragbot/internal/generator"
	"ragbot/internal/retriever"
)

// Deps 服务依赖（main 组装后注入）
type Deps struct {
	Engine    *engine.Engine
	Ret       *retriever.Retriever
	WebDir    string
	Generator *generator.Gateway // 可为 nil（未启用模型时走模板路径）
}

// Handler 构建路由
func Handler(d Deps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "time": time.Now().Format(time.RFC3339)})
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

	// 流式问答：SSE。先输出确定性结果（状态+citations，模型无权改动），
	// 再流式输出表述层（generator 启用时为模型改写，否则/失败时为模板文本）。
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
		ans := d.Engine.AnswerWithScope(req.Query, asOf, req.ScopeKey)

		meta := map[string]any{
			"status":       ans.Status,
			"citations":    ans.Citations,
			"latency_ms":   time.Since(start).Milliseconds(),
			"detected":     asOf == nil && engine.QueryHasInferredTime(req.Query),
			"generated_by": "template",
		}
		send("meta", meta)

		// 拒答/冲突不调用模型：裁决与结论由确定性引擎负责。
		if ans.Status != "answered" || d.Generator == nil {
			send("delta", map[string]any{"text": ans.Answer})
			send("done", map[string]any{
				"total_ms":     time.Since(start).Milliseconds(),
				"generated_by": "template",
			})
			return
		}

		evidence := make([]string, 0, len(ans.Citations))
		for _, c := range ans.Citations {
			evidence = append(evidence, c.Excerpt)
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		llmText, err := d.Generator.StreamAnswer(ctx, req.Query, ans.Answer, evidence, func(delta string) {
			send("delta", map[string]any{"text": delta})
		})
		generatedBy := "llm"
		if err != nil || strings.TrimSpace(llmText) == "" {
			// 任何失败回退模板文本；citations 不变，如实标注来源与原因。
			generatedBy = "template"
			send("delta", map[string]any{"text": ans.Answer})
			send("notice", map[string]any{"fallback_reason": fmt.Sprint(err)})
		}
		send("done", map[string]any{
			"total_ms":     time.Since(start).Milliseconds(),
			"generated_by": generatedBy,
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
