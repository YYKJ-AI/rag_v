package server_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ragbot/internal/engine"
	"ragbot/internal/generator"
	"ragbot/internal/retriever"
	"ragbot/internal/server"
	"ragbot/internal/types"
)

func TestGeneratorStatusAndToggleEndpoints(t *testing.T) {
	r := retriever.New()
	h := server.Handler(server.Deps{Engine: engine.New(r), Ret: r,
		Generator: generator.NewGateway(generator.Config{Enabled: true, BaseURL: "http://x", Model: "fake-model"})})

	// 状态：在线
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/generator/status", nil))
	var st map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if st["enabled"] != true || st["model"] != "fake-model" {
		t.Fatalf("status = %s", rec.Body.String())
	}

	// 关闭 → 离线
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/generator/toggle",
		strings.NewReader(`{"enabled":false}`)))
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if st["enabled"] != false {
		t.Fatalf("toggle off = %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/generator/status", nil))
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if st["enabled"] != false || st["configured"] != true {
		t.Fatalf("status after off = %s", rec.Body.String())
	}

	// 非法请求体
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/generator/toggle",
		strings.NewReader(`{"enabled":"yes"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("toggle invalid body = %d", rec.Code)
	}

	// 未配置 Generator → toggle 409
	h2 := server.Handler(server.Deps{Engine: engine.New(retriever.New()), Ret: retriever.New()})
	rec = httptest.NewRecorder()
	h2.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/generator/toggle",
		strings.NewReader(`{"enabled":true}`)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("toggle without generator = %d, want 409", rec.Code)
	}
}

func TestIngestEndpoint(t *testing.T) {
	calls := 0
	h := server.Handler(server.Deps{
		Engine: engine.New(retriever.New()), Ret: retriever.New(),
		IngestDocument: func(name string, raw []byte) (*server.IngestResult, error) {
			calls++
			if strings.HasPrefix(name, "bad") {
				return nil, errors.New("版本冲突：x@v1 已存在且内容不同")
			}
			return &server.IngestResult{Action: "created", DocID: "x", Version: "v1",
				Title: "测试", Sections: 2, Chunks: 3, VectorSync: "offline"}, nil
		},
	})

	post := func(name string, content string) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("file", name)
		_, _ = fw.Write([]byte(content))
		mw.Close()
		req := httptest.NewRequest(http.MethodPost, "/api/ingest", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := post("notice.md", "---\nid: x\nversion: v1\ntitle: 测试\n---\n## 节\n内容")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"action":"created"`) ||
		!strings.Contains(rec.Body.String(), `"vector_sync":"offline"`) {
		t.Fatalf("ingest valid = %d %s", rec.Code, rec.Body.String())
	}
	if calls != 1 {
		t.Fatalf("callback calls = %d", calls)
	}

	rec = post("bad.md", "内容")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "版本冲突") {
		t.Fatalf("ingest error passthrough = %d %s", rec.Code, rec.Body.String())
	}
	rec = post("note.txt", "内容")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), ".md") {
		t.Fatalf("ingest bad ext = %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/ingest", strings.NewReader("x")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ingest without multipart = %d", rec.Code)
	}

	// 回调未配置 → 501
	h2 := server.Handler(server.Deps{Engine: engine.New(retriever.New()), Ret: retriever.New()})
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "a.md")
	_, _ = fw.Write([]byte("x"))
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/ingest", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec2 := httptest.NewRecorder()
	h2.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusNotImplemented {
		t.Fatalf("ingest without callback = %d, want 501", rec2.Code)
	}
}

// TestStreamHandlerCarriesPlan 验证流式 meta 携带 plan 字段（离线时 source=offline）。
func TestStreamHandlerCarriesPlan(t *testing.T) {
	date := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	sec := &types.Section{ID: "fee@v1#sec-1", DocID: "fee", Version: "v1", SectionPath: "费用标准", Text: "申请费用为一百元。"}
	r := retriever.New()
	r.Index([]*types.Document{{ID: "fee", Version: "v1", Title: "申请费用", EffectiveDate: date}},
		[]*types.Section{sec}, []*types.Chunk{{ID: "fee@v1#c1", SectionID: sec.ID, Text: sec.Text}})
	h := server.Handler(server.Deps{Engine: engine.New(r), Ret: r})
	out := askStream(t, h, "申请费用是多少")
	if !strings.Contains(out, `"plan":{"source":"offline"}`) {
		t.Errorf("meta 应携带离线 plan: %s", out)
	}
}
