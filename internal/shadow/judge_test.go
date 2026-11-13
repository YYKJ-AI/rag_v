package shadow

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ragbot/internal/engine"
	"ragbot/internal/generator"
)

func TestRemoteJudgeValidatesSourceIDsAndSchema(t *testing.T) {
	candidate := engine.Candidate{ID: "guide@v1#sec-1/sent-1", DocumentID: "guide", Version: "v1", Location: "条件", Text: "面向年满十二周岁居民。"}
	for _, tc := range []struct {
		name    string
		status  int
		content string
		wantErr bool
	}{
		{name: "supported", status: 200, content: `{"supported_ids":["guide@v1#sec-1/sent-1"],"sufficient":true,"reason":"年龄有据"}`},
		{name: "unknown ID", status: 200, content: `{"supported_ids":["fabricated"],"sufficient":true,"reason":"有据"}`, wantErr: true},
		{name: "missing sufficient", status: 200, content: `{"supported_ids":[],"reason":"无据"}`, wantErr: true},
		{name: "no support but sufficient", status: 200, content: `{"supported_ids":[],"sufficient":true,"reason":"有据"}`, wantErr: true},
		{name: "not JSON", status: 200, content: "答案是 12 岁", wantErr: true},
		{name: "upstream error", status: 503, content: "secret-url", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
					t.Errorf("Judge Authorization = %q, want Bearer test-key", got)
				}
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, tc.content)
			}))
			t.Cleanup(server.Close)
			judge, err := NewRemoteJudge(generator.Config{
				Enabled: true, Provider: "openai_compatible", BaseURL: server.URL,
				Model: "test", APIKey: "test-key", Timeout: time.Second,
			})
			if err != nil {
				t.Fatalf("NewRemoteJudge(test config) error = %v, want nil", err)
			}
			got, err := judge.Judge(context.Background(), "最低年龄", []engine.Candidate{candidate})
			if (err != nil) != tc.wantErr {
				t.Errorf("Judge(%q, candidate) error = %v, want error presence %t", tc.name, err, tc.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), "secret-url") {
				t.Errorf("Judge(%q, candidate) error leaked upstream body: %v", tc.name, err)
			}
			if !tc.wantErr && (!got.Sufficient || len(got.SupportedIDs) != 1 || got.SupportedIDs[0] != candidate.ID) {
				t.Errorf("Judge(%q, candidate) = %+v, want sufficient with source ID", tc.name, got)
			}
		})
	}
}

func TestNewRemoteJudgeRequiresConfig(t *testing.T) {
	if judge, err := NewRemoteJudge(generator.Config{}); err == nil || judge != nil {
		t.Errorf("NewRemoteJudge(empty config) = %v, %v; want nil judge and error", judge, err)
	}
}
