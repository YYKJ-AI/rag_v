// Package server HTTP 服务模式：静态页 + JSON API（纯标准库，无框架）。
package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ragbot/internal/engine"
	"ragbot/internal/retriever"
)

// Deps 服务依赖（main 组装后注入）
type Deps struct {
	Engine *engine.Engine
	Ret    *retriever.Retriever
	WebDir string
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
