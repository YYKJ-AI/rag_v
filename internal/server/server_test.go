package server_test

import (
	"encoding/json"
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
	r := retriever.New()
	r.Index(docs, sections, nil)
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
