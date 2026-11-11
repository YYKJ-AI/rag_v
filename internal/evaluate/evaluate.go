// Package evaluate checks retrieval, answer status, and citation provenance.
package evaluate

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"ragbot/internal/engine"
	"ragbot/internal/types"
)

// Source identifies an expected citation document and version.
type Source struct {
	DocumentID string `json:"document_id"`
	Version    string `json:"version"`
}

// TestCase is one labeled query. ExpectSources refines ExpectDocs with versions.
type TestCase struct {
	ID            string   `json:"id"`
	Query         string   `json:"query"`
	ExpectDocs    []string `json:"expect_docs"`
	ExpectSources []Source `json:"expect_sources,omitempty"`
	ExpectState   string   `json:"expect_status"`
	Note          string   `json:"note,omitempty"`
}

// Record contains independent checks for one query.
type Record struct {
	ID               string           `json:"id"`
	Query            string           `json:"query"`
	ExpectedStatus   string           `json:"expected_status"`
	Status           string           `json:"status"`
	Hit5             bool             `json:"hit5"`
	Cited            bool             `json:"cited"`
	SourceCoverage   bool             `json:"source_coverage"`
	CitationsLocated bool             `json:"citations_located"`
	Citations        []types.Citation `json:"citations,omitempty"`
	MissingSources   []Source         `json:"missing_sources,omitempty"`
	Refusal          bool             `json:"refusal"`
	Pass             bool             `json:"pass"`
	LatencyMs        float64          `json:"latency_ms"`
}

// Report summarizes counts with explicit denominators.
type Report struct {
	GeneratedAt   string   `json:"generated_at"`
	Total         int      `json:"total"`
	Passed        int      `json:"passed"`
	Hit5Passed    int      `json:"hit5_passed"`
	Hit5Total     int      `json:"hit5_total"`
	Hit5Rate      float64  `json:"hit5_rate"`
	CitedPassed   int      `json:"cited_passed"`
	CitedTotal    int      `json:"cited_total"`
	CitedRate     float64  `json:"cited_rate"`
	SourcePassed  int      `json:"source_passed"`
	SourceTotal   int      `json:"source_total"`
	SourceRate    float64  `json:"source_rate"`
	RefusalPassed int      `json:"refusal_passed"`
	RefusalTotal  int      `json:"refusal_total"`
	RefusalRate   float64  `json:"refusal_rate"`
	RefusalOK     bool     `json:"refusal_ok"`
	AvgLatencyMs  float64  `json:"avg_latency_ms"`
	Records       []Record `json:"records"`
}

// Run evaluates the answer and retrieval results for every labeled query.
func Run(e *engine.Engine, queries []TestCase, topN int) *Report {
	const latencySamples = 64
	records := make([]Record, 0, len(queries))
	for _, tc := range queries {
		start := time.Now()
		ans := e.Answer(tc.Query, nil)
		for i := 1; i < latencySamples; i++ {
			e.Answer(tc.Query, nil)
		}
		elapsed := time.Since(start) / latencySamples
		// Search runs outside latency timing. Full hits verify excerpts against
		// their stored parent sections, regardless of ranking.
		allHits := e.Top(tc.Query, 0)
		topHits := allHits
		if topN > 0 && len(topHits) > topN {
			topHits = topHits[:topN]
		}
		records = append(records, score(tc, ans, topHits, allHits, elapsed))
	}
	return summarize(records)
}

func score(tc TestCase, ans types.Answer, topHits, allHits []types.Hit, elapsed time.Duration) Record {
	rec := Record{
		ID: tc.ID, Query: tc.Query, ExpectedStatus: tc.ExpectState, Status: ans.Status,
		Citations: ans.Citations, Cited: len(ans.Citations) > 0,
		LatencyMs: float64(elapsed) / float64(time.Millisecond),
	}
	if tc.ExpectState == "insufficient_evidence" {
		rec.Refusal = ans.Status == "insufficient_evidence" && !rec.Cited
		rec.Pass = rec.Refusal
		return rec
	}

	foundDocs := make(map[string]bool, len(topHits))
	for _, hit := range topHits {
		foundDocs[hit.Section.DocID] = true
	}
	rec.Hit5 = len(tc.ExpectDocs) > 0
	for _, docID := range tc.ExpectDocs {
		if !foundDocs[docID] {
			rec.Hit5 = false
		}
	}

	rec.CitationsLocated = rec.Cited
	for _, citation := range ans.Citations {
		located := false
		if citation.DocumentID != "" && citation.Version != "" && citation.Location != "" && citation.Excerpt != "" {
			for _, hit := range allHits {
				s := hit.Section
				if s.DocID == citation.DocumentID && s.Version == citation.Version && s.SectionPath == citation.Location && strings.Contains(s.Text, citation.Excerpt) {
					located = true
					break
				}
			}
		}
		if !located {
			rec.CitationsLocated = false
		}
	}

	rec.SourceCoverage = rec.Cited
	if len(tc.ExpectSources) > 0 {
		sourceDocs := make(map[string]bool, len(tc.ExpectSources))
		for _, want := range tc.ExpectSources {
			sourceDocs[want.DocumentID] = true
			found := false
			for _, citation := range ans.Citations {
				if citation.DocumentID == want.DocumentID && citation.Version == want.Version {
					found = true
					break
				}
			}
			if !found {
				rec.MissingSources = append(rec.MissingSources, want)
				rec.SourceCoverage = false
			}
		}
		for _, docID := range tc.ExpectDocs {
			if sourceDocs[docID] {
				continue
			}
			found := false
			for _, citation := range ans.Citations {
				if citation.DocumentID == docID {
					found = true
					break
				}
			}
			if !found {
				rec.MissingSources = append(rec.MissingSources, Source{DocumentID: docID})
				rec.SourceCoverage = false
			}
		}
	} else {
		for _, docID := range tc.ExpectDocs {
			found := false
			for _, citation := range ans.Citations {
				if citation.DocumentID == docID {
					found = true
					break
				}
			}
			if !found {
				rec.MissingSources = append(rec.MissingSources, Source{DocumentID: docID})
				rec.SourceCoverage = false
			}
		}
	}
	rec.Pass = ans.Status == tc.ExpectState && rec.Hit5 && rec.SourceCoverage && rec.CitationsLocated
	return rec
}

func summarize(records []Record) *Report {
	rep := &Report{Records: records, Total: len(records), GeneratedAt: time.Now().Format(time.RFC3339)}
	for _, rec := range records {
		if rec.Pass {
			rep.Passed++
		}
		rep.AvgLatencyMs += rec.LatencyMs
		if rec.ExpectedStatus == "insufficient_evidence" {
			rep.RefusalTotal++
			if rec.Refusal {
				rep.RefusalPassed++
			}
			continue
		}
		rep.Hit5Total++
		rep.CitedTotal++
		rep.SourceTotal++
		if rec.Hit5 {
			rep.Hit5Passed++
		}
		if rec.Cited {
			rep.CitedPassed++
		}
		if rec.SourceCoverage && rec.CitationsLocated {
			rep.SourcePassed++
		}
	}
	if rep.Total > 0 {
		rep.AvgLatencyMs /= float64(rep.Total)
	}
	if rep.Hit5Total > 0 {
		rep.Hit5Rate = float64(rep.Hit5Passed) / float64(rep.Hit5Total)
		rep.CitedRate = float64(rep.CitedPassed) / float64(rep.CitedTotal)
		rep.SourceRate = float64(rep.SourcePassed) / float64(rep.SourceTotal)
	}
	if rep.RefusalTotal > 0 {
		rep.RefusalRate = float64(rep.RefusalPassed) / float64(rep.RefusalTotal)
	}
	rep.RefusalOK = rep.RefusalPassed == rep.RefusalTotal
	return rep
}

// Save writes the full evaluation record as JSON.
func (rep *Report) Save(path string) error {
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}

// LoadQueries reads a labeled query set.
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
		return nil, fmt.Errorf("查询集不足 12 条（当前 %d），任务书要求至少 12 条", len(qs))
	}
	return qs, nil
}
