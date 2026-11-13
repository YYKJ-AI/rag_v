package server_test

import (
	"fmt"
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

// TestYearPrecisionAnswersAcrossEffectiveDate 回归：v1 生效日 2025-03-15，
// 问"2025年申请要准备几项材料"（模型解析 time=2025-01-01 + year 精度）
// 应按年末锚点匹配 v1 并回答；若无精度换算（钉死 1 月 1 日）则会误拒答。
func TestYearPrecisionAnswersAcrossEffectiveDate(t *testing.T) {
	var requests int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		body := string(buf[:n])
		if strings.Contains(body, `"stream":false`) {
			// 预处理请求：返回 JSON 计划（year 精度）
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"rewritten\":\"\",\"entities\":[],\"sub_queries\":[\"2025年申请要准备几项材料\"],\"time\":\"2025-01-01\",\"time_precision\":\"year\"}"}}]}`)
			return
		}
		// 改写请求：返回 SSE 流
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"申请须提交活动方案、安全预案及场地使用说明三项材料。\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer upstream.Close()

	date := time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC) // v1 生效日：3 月 15 日
	sec := &types.Section{ID: "guide@v1#s1", DocID: "guide", Version: "v1", SectionPath: "申请条件",
		Text: "申请方须提交活动方案、安全预案及场地使用说明三项材料。"}
	r := retriever.New()
	r.Index([]*types.Document{{ID: "guide", Version: "v1", Title: "申请指南", EffectiveDate: date}},
		[]*types.Section{sec}, []*types.Chunk{{ID: "guide@v1#c1", SectionID: sec.ID, Text: sec.Text}})
	g := generator.NewGateway(generator.Config{Enabled: true, BaseURL: upstream.URL, Model: "fake", Timeout: 3 * time.Second})
	h := server.Handler(server.Deps{Engine: engine.New(r), Ret: r, Generator: g})

	out := askStream(t, h, "2025年申请要准备几项材料")
	if !strings.Contains(out, `"status":"answered"`) || !strings.Contains(out, "三项材料") {
		t.Errorf("year 精度应匹配年内有效的 v1 版本: %s", out)
	}
	if !strings.Contains(out, `"time_precision":"year"`) {
		t.Errorf("meta 应携带时间精度: %s", out)
	}
	if requests != 2 {
		t.Errorf("upstream calls = %d, want 2（预处理+改写）", requests)
	}
}
