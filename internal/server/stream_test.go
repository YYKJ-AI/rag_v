package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ragbot/internal/engine"
	"ragbot/internal/retriever"
	"ragbot/internal/server"
	"ragbot/internal/types"
)

// streamFixture 构建含一份数据的引擎（generator 未启用 → 模板回退路径）
func streamFixture(t *testing.T) http.Handler {
	t.Helper()
	date := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	docs := []*types.Document{
		{ID: "fee-doc", Version: "v1", Title: "申请费用", EffectiveDate: date},
	}
	sections := []*types.Section{
		{ID: "fee-doc@v1#sec-1", DocID: "fee-doc", Version: "v1", SectionPath: "费用标准", Text: "居民申请费用为一百元。"},
	}
	chunks := []*types.Chunk{
		{ID: "fee-doc@v1#chunk-1", SectionID: sections[0].ID, Text: sections[0].Text},
	}
	r := retriever.New()
	r.Index(docs, sections, chunks)
	return server.Handler(server.Deps{Engine: engine.New(r), Ret: r})
}

func TestAskStreamEmitsDeterministicMetaAndTemplateFallback(t *testing.T) {
	h := streamFixture(t)
	body := `{"query":"申请费用是多少元"}`
	req := httptest.NewRequest(http.MethodPost, "/api/ask/stream", strings.NewReader(body))
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.Code, resp.Body.String())
	}
	out := resp.Body.String()
	if !strings.Contains(out, "event: meta") || !strings.Contains(out, "event: delta") || !strings.Contains(out, "event: done") {
		t.Fatalf("SSE 缺少 meta/delta/done 事件: %s", out)
	}
	if !strings.Contains(out, `"generated_by":"template"`) {
		t.Fatal("generator 未启用时应为 template 路径")
	}
	if !strings.Contains(out, `"citations":[`) {
		t.Fatal("meta 事件必须携带确定性 citations")
	}
	if !strings.Contains(out, "一百元") {
		t.Fatal("模板文本应包含证据原文")
	}
}

func TestAskStreamRejectsEmptyQuery(t *testing.T) {
	h := streamFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/api/ask/stream", strings.NewReader(`{"query":"  "}`))
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("空问题应 400，实际 %d", resp.Code)
	}
}
