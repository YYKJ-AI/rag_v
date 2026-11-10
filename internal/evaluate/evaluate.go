// Package evaluate 评测链路：查询集 → 指标 → 逐条结果落盘（可复现）。
package evaluate

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"ragbot/internal/engine"
	"ragbot/internal/types"
)

// TestCase 一条标注查询
type TestCase struct {
	ID          string   `json:"id"`
	Query       string   `json:"query"`
	ExpectDocs  []string `json:"expect_docs"`           // 期望命中的文档
	ExpectState string   `json:"expect_status"`         // answered|conflict|insufficient_evidence
	Note        string   `json:"note,omitempty"`
}

// Record 单条结果
type Record struct {
	ID       string `json:"id"`
	Query    string `json:"query"`
	Status   string `json:"status"`
	Hit5     bool   `json:"hit5"`
	Cited    bool   `json:"cited"`
	Refusal  bool   `json:"refusal"`
	Pass     bool   `json:"pass"`
	LatencyMs int64 `json:"latency_ms"`
	Detail   string `json:"detail,omitempty"`
}

// Report 汇总
type Report struct {
	GeneratedAt string   `json:"generated_at"`
	Total       int      `json:"total"`
	Passed      int      `json:"passed"`
	Hit5Rate    float64  `json:"hit5_rate"`
	CitedRate   float64  `json:"cited_rate"`
	RefusalOK   bool     `json:"refusal_ok"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	Records     []Record `json:"records"`
}

// Run 评测主流程
func Run(e *engine.Engine, queries []TestCase, topN int) *Report {
	rep := &Report{}
	var hit, cited int
	var latSum int64
	refusalCases, refusalPassed := 0, 0
	for _, tc := range queries {
		start := time.Now()
		ans := e.Answer(tc.Query, nil)
		ms := time.Since(start).Milliseconds()
		latSum += ms

		hitDocs := map[string]bool{}
		for _, h := range eTop(e, tc.Query, topN) {
			hitDocs[h.Section.DocID] = true
		}
		h5 := false
		for _, d := range tc.ExpectDocs {
			if hitDocs[d] {
				h5 = true
			}
		}
		c := len(ans.Citations) > 0
		pass := h5 && ans.Status == tc.ExpectState
		rec := Record{
			ID: tc.ID, Query: tc.Query, Status: ans.Status,
			Hit5: h5, Cited: c, Pass: pass, LatencyMs: ms,
		}
		if tc.ExpectState == "insufficient_evidence" {
			rec.Refusal = ans.Status == "insufficient_evidence" && len(ans.Citations) == 0
			rec.Pass = rec.Refusal
			refusalCases++
			if rec.Refusal {
				refusalPassed++
			}
		}
		if pass {
			rep.Passed++
		}
		if h5 {
			hit++
		}
		if c {
			cited++
		}
		rep.Records = append(rep.Records, rec)
		rep.Total++
	}
	n := float64(rep.Total)
	if n > 0 {
		rep.Hit5Rate = float64(hit) / n
		rep.CitedRate = float64(cited) / n
		rep.AvgLatencyMs = float64(latSum) / n
	}
	rep.GeneratedAt = time.Now().Format(time.RFC3339)
	rep.RefusalOK = refusalCases == 0 || refusalCases == refusalPassed
	return rep
}

// eTop 暴露检索结果给评测（Hit@5 判定用）
func eTop(e *engine.Engine, q string, n int) []types.Hit { return e.Top(q, n) }

// Save 逐条结果落盘
func (rep *Report) Save(path string) error {
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}

func LoadQueries(path string) ([]TestCase, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取查询集失败: %w", err)
	}
	var qs []TestCase
	if err := json.Unmarshal(b, &qs); err != nil {
		return nil, fmt.Errorf("查询集格式错误: %w", err)
	}
	if len(qs) < 12 {
		return nil, fmt.Errorf("查询集不足 12 条（当前 %d），任务书要求 ≥12", len(qs))
	}
	return qs, nil
}
