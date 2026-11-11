package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"ragbot/internal/engine"
	"ragbot/internal/ingest"
	"ragbot/internal/retriever"
	"ragbot/internal/server"
	"ragbot/internal/types"
)

func TestAskUsesScopeKey(t *testing.T) {
	date := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	docs := []*types.Document{
		{ID: "resident-fee", Version: "v1", Title: "申请费用", EffectiveDate: date, ScopeKeys: []string{"resident"}},
		{ID: "org-fee", Version: "v1", Title: "申请费用", EffectiveDate: date, ScopeKeys: []string{"organization"}},
	}
	sections := []*types.Section{
		{ID: "resident-fee@v1#sec-1", DocID: "resident-fee", Version: "v1", SectionPath: "费用标准", Text: "居民申请费用为一百元。"},
		{ID: "org-fee@v1#sec-1", DocID: "org-fee", Version: "v1", SectionPath: "费用标准", Text: "组织申请费用为二百元。"},
	}
	chunks := []*types.Chunk{
		{ID: "resident-fee@v1#chunk-1", SectionID: sections[0].ID, Text: sections[0].Text},
		{ID: "org-fee@v1#chunk-1", SectionID: sections[1].ID, Text: sections[1].Text},
	}
	r := retriever.New()
	r.Index(docs, sections, chunks)
	h := server.Handler(server.Deps{Engine: engine.New(r), Ret: r})
	for _, tt := range []struct {
		key, wantDoc string
	}{
		{"resident", "resident-fee"},
		{"organization", "org-fee"},
	} {
		t.Run(tt.key, func(t *testing.T) {
			body := `{"query":"申请费用是多少元","scope_key":"` + tt.key + `"}`
			request := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(body))
			response := httptest.NewRecorder()
			h.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("POST /api/ask(%s) status = %d, want 200: %s", tt.key, response.Code, response.Body.String())
			}
			var result struct {
				Status    string           `json:"status"`
				Citations []types.Citation `json:"citations"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatalf("json.Unmarshal(POST /api/ask(%s)) error = %v, want nil", tt.key, err)
			}
			if result.Status != "answered" || len(result.Citations) == 0 {
				t.Fatalf("POST /api/ask(%s) = %+v, want answered with citations", tt.key, result)
			}
			for _, citation := range result.Citations {
				if citation.DocumentID != tt.wantDoc {
					t.Errorf("POST /api/ask(%s) cites %s, want %s", tt.key, citation.DocumentID, tt.wantDoc)
				}
			}
		})
	}
}

func TestSearchMarksUnverifiedSource(t *testing.T) {
	meta, drafts, err := ingest.ParseUnstructured([]byte("## 申请条件\n申请人须提交身份证明材料。"), "guide.md")
	if err != nil {
		t.Fatalf("ParseUnstructured(guide.md) error = %v", err)
	}
	doc, sections, chunks, err := ingest.Build(meta, drafts)
	if err != nil {
		t.Fatalf("Build(guide.md) error = %v", err)
	}
	r := retriever.New()
	r.Index([]*types.Document{doc}, sections, chunks)
	h := server.Handler(server.Deps{Engine: engine.New(r), Ret: r})
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/search?q=身份证明", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/search status = %d, want 200", response.Code)
	}
	var result struct {
		Hits []struct {
			SourceStatus string `json:"source_status"`
			SourceName   string `json:"source_name"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal(GET /api/search) error = %v", err)
	}
	if len(result.Hits) != 1 || result.Hits[0].SourceStatus != "unverified" || result.Hits[0].SourceName != "guide.md" {
		t.Errorf("GET /api/search hits = %+v, want one unverified guide.md hit", result.Hits)
	}
}

func TestConcurrentAskKeepsDetectedTimePerRequest(t *testing.T) {
	r := retriever.New()
	h := server.Handler(server.Deps{Engine: engine.New(r), Ret: r})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		for _, tt := range []struct {
			query string
			want  bool
		}{
			{query: "2025年申请要交哪些材料", want: true},
			{query: "现在申请要交哪些材料", want: false},
		} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				body, err := json.Marshal(map[string]string{"query": tt.query})
				if err != nil {
					t.Errorf("json.Marshal(%q) error = %v, want nil", tt.query, err)
					return
				}
				request := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(string(body)))
				response := httptest.NewRecorder()
				h.ServeHTTP(response, request)
				if response.Code != http.StatusOK {
					t.Errorf("POST /api/ask(%q) status = %d, want 200", tt.query, response.Code)
					return
				}
				var result map[string]json.RawMessage
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Errorf("json.Unmarshal(POST /api/ask(%q)) error = %v, want nil", tt.query, err)
					return
				}
				_, got := result["detected_time"]
				if got != tt.want {
					t.Errorf("POST /api/ask(%q) detected_time present = %t, want %t", tt.query, got, tt.want)
				}
			}()
		}
	}
	wg.Wait()
}

func TestAskRejectsInvalidRequests(t *testing.T) {
	r := retriever.New()
	h := server.Handler(server.Deps{Engine: engine.New(r), Ret: r})
	for _, tt := range []struct {
		name, body string
	}{
		{name: "blank query", body: `{"query":"   "}`},
		{name: "malformed JSON", body: `{"query":`},
		{name: "bad date", body: `{"query":"申请材料","as_of":"2025-02-30"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(tt.body))
			response := httptest.NewRecorder()
			h.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Errorf("POST /api/ask(%s) status = %d, want 400", tt.name, response.Code)
			}
			if strings.Contains(response.Body.String(), `"status":"answered"`) {
				t.Errorf("POST /api/ask(%s) body = %q, want no successful answer", tt.name, response.Body.String())
			}
		})
	}
}
