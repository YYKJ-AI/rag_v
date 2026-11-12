package generator_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ragbot/internal/generator"
)

func TestLoadConfigDisabledByDefault(t *testing.T) {
	cfg := generator.LoadConfig(map[string]string{})
	if cfg.Valid() {
		t.Fatal("空配置不应启用 generator")
	}
	cfg = generator.LoadConfig(map[string]string{
		"RAGBOT_GENERATOR_ENABLED": "true",
	})
	if cfg.Valid() {
		t.Fatal("缺 BaseURL/Model 时不应启用")
	}
	cfg = generator.LoadConfig(map[string]string{
		"RAGBOT_GENERATOR_ENABLED":         "true",
		"RAGBOT_GENERATOR_BASE_URL":        "http://x/v1",
		"RAGBOT_GENERATOR_MODEL":           "test-model",
		"RAGBOT_GENERATOR_TIMEOUT_SECONDS": "15",
	})
	if !cfg.Valid() {
		t.Fatal("完整配置应启用")
	}
	if cfg.Timeout != 15*time.Second {
		t.Fatalf("超时应为 15s，实际 %v", cfg.Timeout)
	}
}

func TestStreamAnswerWithFakeUpstream(t *testing.T) {
	// 模拟 OpenAI 兼容 SSE 上游
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("缺少鉴权头")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		for _, chunk := range []string{"你好", "，", "回答"} {
			data := `{"choices":[{"delta":{"content":"` + chunk + `"}}]}`
			w.Write([]byte("data: " + data + "\n\n"))
			f.Flush()
		}
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	cfg := generator.LoadConfig(map[string]string{
		"RAGBOT_GENERATOR_ENABLED":  "true",
		"RAGBOT_GENERATOR_BASE_URL": upstream.URL,
		"RAGBOT_GENERATOR_MODEL":    "fake",
		"RAGBOT_GENERATOR_API_KEY":  "test-key",
	})
	g := generator.NewGateway(cfg)

	var deltas []string
	got, err := g.StreamAnswer(context.Background(), "问题", "模板回答", []string{"证据一"}, func(d string) {
		deltas = append(deltas, d)
	})
	if err != nil {
		t.Fatalf("StreamAnswer 失败: %v", err)
	}
	if got != "你好，回答" {
		t.Fatalf("完整文本不符: %q", got)
	}
	if strings.Join(deltas, "") != "你好，回答" {
		t.Fatalf("增量片段不符: %v", deltas)
	}
}

func TestStreamAnswerFallbackOnUpstreamFailure(t *testing.T) {
	// 上游返回 500 → generator 报错，由调用方回退模板
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	cfg := generator.LoadConfig(map[string]string{
		"RAGBOT_GENERATOR_ENABLED":  "true",
		"RAGBOT_GENERATOR_BASE_URL": upstream.URL,
		"RAGBOT_GENERATOR_MODEL":    "fake",
	})
	g := generator.NewGateway(cfg)
	_, err := g.StreamAnswer(context.Background(), "问题", "模板", nil, nil)
	if err == nil {
		t.Fatal("上游 500 应返回 error（触发回退）")
	}
}

func TestStreamAnswerEmptyOutputFails(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	cfg := generator.LoadConfig(map[string]string{
		"RAGBOT_GENERATOR_ENABLED":  "true",
		"RAGBOT_GENERATOR_BASE_URL": upstream.URL,
		"RAGBOT_GENERATOR_MODEL":    "fake",
	})
	g := generator.NewGateway(cfg)
	_, err := g.StreamAnswer(context.Background(), "问题", "模板", nil, nil)
	if err == nil {
		t.Fatal("空输出应返回 error（触发回退），不得以空文本冒充成功")
	}
}
