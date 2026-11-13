package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ragbot/internal/engine"
	"ragbot/internal/evaluate"
	"ragbot/internal/safety"
)

// Record keeps the candidate pool and the model decision separately.
type Record struct {
	ID                     string              `json:"id"`
	Query                  string              `json:"query"`
	ExpectedStatus         string              `json:"expected_status"`
	OfflineStatus          string              `json:"offline_status"`
	Candidates             []engine.Candidate  `json:"candidates"`
	MissingFromPool        []evaluate.Evidence `json:"missing_from_pool,omitempty"`
	MissingFromSelection   []evaluate.Evidence `json:"missing_from_selection,omitempty"`
	Decision               *Decision           `json:"decision,omitempty"`
	JudgeError             string              `json:"judge_error,omitempty"`
	JudgeLatencyMs         float64             `json:"judge_latency_ms,omitempty"`
	SelectedSupported      int                 `json:"selected_supported"`
	SelectedTotal          int                 `json:"selected_total"`
	RequiredEvidence       int                 `json:"required_evidence"`
	PoolEvidence           int                 `json:"pool_evidence"`
	SelectedEvidence       int                 `json:"selected_evidence"`
	CandidateGroupComplete bool                `json:"candidate_group_complete"`
	SelectionGroupComplete bool                `json:"selection_group_complete"`
	SafetyBlocked          bool                `json:"safety_blocked"`
}

// Report measures candidate availability and judgment without changing Answer.
type Report struct {
	GeneratedAt         string   `json:"generated_at"`
	PoolSections        int      `json:"pool_sections"`
	Total               int      `json:"total"`
	Answerable          int      `json:"answerable"`
	RefusalTotal        int      `json:"refusal_total"`
	RefusalJudged       int      `json:"refusal_judged"`
	ConflictTotal       int      `json:"conflict_total"`
	SafetyBlocked       int      `json:"safety_blocked"`
	NoCandidates        int      `json:"no_candidates"`
	CandidateGroups     int      `json:"candidate_groups"`
	CandidateGroupTotal int      `json:"candidate_group_total"`
	RequiredEvidence    int      `json:"required_evidence"`
	PoolEvidence        int      `json:"pool_evidence"`
	ValidJudgments      int      `json:"valid_judgments"`
	JudgeErrors         int      `json:"judge_errors"`
	SelectedEvidence    int      `json:"selected_evidence"`
	SelectedSupported   int      `json:"selected_supported"`
	SelectedTotal       int      `json:"selected_total"`
	CompleteAnswers     int      `json:"complete_answers"`
	FalseAnswers        int      `json:"false_answers"`
	UnsupportedAnswers  int      `json:"unsupported_answers"`
	UnsafeAnswers       int      `json:"unsafe_answers"`
	CorrectRefusals     int      `json:"correct_refusals"`
	JudgeMisses         int      `json:"judge_misses"`
	AvgJudgeLatencyMs   float64  `json:"avg_judge_latency_ms"`
	Records             []Record `json:"records"`
}

// Run measures a fixed source pool. A nil judge performs an offline pool audit.
func Run(ctx context.Context, e *engine.Engine, cases []evaluate.TestCase, poolSections int, judge Judge) Report {
	report := Report{GeneratedAt: time.Now().Format(time.RFC3339), PoolSections: poolSections, Total: len(cases)}
	var latencySum float64
	for _, tc := range cases {
		candidates := e.CandidateSentences(tc.Query, poolSections)
		rec := Record{ID: tc.ID, Query: tc.Query, ExpectedStatus: tc.ExpectState,
			OfflineStatus: e.Answer(tc.Query, nil).Status, Candidates: candidates}
		if safety.IsDirectOverride(tc.Query) {
			rec.SafetyBlocked = true
			report.SafetyBlocked++
		} else if len(candidates) == 0 {
			report.NoCandidates++
		}
		switch tc.ExpectState {
		case "answered":
			report.Answerable++
			report.CandidateGroupTotal++
		case "conflict":
			report.ConflictTotal++
			report.CandidateGroupTotal++
		case "insufficient_evidence":
			report.RefusalTotal++
		}
		for _, want := range tc.ExpectEvidence {
			if want.Optional {
				continue
			}
			rec.RequiredEvidence++
			report.RequiredEvidence++
			if hasEvidence(candidates, want, nil) {
				rec.PoolEvidence++
				report.PoolEvidence++
			} else {
				rec.MissingFromPool = append(rec.MissingFromPool, want)
			}
		}
		rec.CandidateGroupComplete = rec.RequiredEvidence > 0 && len(rec.MissingFromPool) == 0
		if rec.CandidateGroupComplete {
			report.CandidateGroups++
		}
		if judge != nil && len(candidates) > 0 {
			start := time.Now()
			decision, err := judge.Judge(ctx, tc.Query, candidates)
			rec.JudgeLatencyMs = float64(time.Since(start)) / float64(time.Millisecond)
			if err != nil {
				rec.JudgeError = err.Error()
				report.JudgeErrors++
			} else if err := validateDecision(decision, candidates); err != nil {
				rec.JudgeError = err.Error()
				report.JudgeErrors++
			} else {
				rec.Decision = &decision
				report.ValidJudgments++
				if tc.ExpectState == "insufficient_evidence" {
					report.RefusalJudged++
				}
				latencySum += rec.JudgeLatencyMs
				scoreDecision(&report, &rec, tc, decision)
			}
		}
		report.Records = append(report.Records, rec)
	}
	if report.ValidJudgments > 0 {
		report.AvgJudgeLatencyMs = latencySum / float64(report.ValidJudgments)
	}
	return report
}

func scoreDecision(report *Report, rec *Record, tc evaluate.TestCase, decision Decision) {
	selected := make(map[string]bool, len(decision.SupportedIDs))
	for _, id := range decision.SupportedIDs {
		selected[id] = true
	}
	for _, want := range tc.ExpectEvidence {
		if want.Optional {
			continue
		}
		if hasEvidence(rec.Candidates, want, selected) {
			rec.SelectedEvidence++
			report.SelectedEvidence++
		} else {
			rec.MissingFromSelection = append(rec.MissingFromSelection, want)
		}
	}
	// Refusal cases have no positive sentence labels; do not score their selected
	// IDs as unsupported merely because the annotation describes absence of an answer.
	if tc.ExpectState != "insufficient_evidence" {
		for _, candidate := range rec.Candidates {
			if !selected[candidate.ID] {
				continue
			}
			rec.SelectedTotal++
			report.SelectedTotal++
			for _, want := range tc.ExpectEvidence {
				if matchesEvidence(candidate, want) {
					rec.SelectedSupported++
					report.SelectedSupported++
					break
				}
			}
		}
	}
	rec.SelectionGroupComplete = rec.RequiredEvidence > 0 && len(rec.MissingFromSelection) == 0
	if tc.ExpectState == "answered" && decision.Sufficient && rec.SelectionGroupComplete && rec.SelectedTotal == rec.SelectedSupported {
		report.CompleteAnswers++
	}
	if tc.ExpectState == "answered" && decision.Sufficient && (!rec.SelectionGroupComplete || rec.SelectedTotal != rec.SelectedSupported) {
		report.UnsupportedAnswers++
		report.UnsafeAnswers++
	}
	if tc.ExpectState != "answered" && decision.Sufficient {
		report.FalseAnswers++
		report.UnsafeAnswers++
	}
	if tc.ExpectState == "insufficient_evidence" && !decision.Sufficient {
		report.CorrectRefusals++
	}
	if tc.ExpectState == "answered" && rec.CandidateGroupComplete && !decision.Sufficient {
		report.JudgeMisses++
	}
}

func hasEvidence(candidates []engine.Candidate, want evaluate.Evidence, selected map[string]bool) bool {
	for _, candidate := range candidates {
		if (selected == nil || selected[candidate.ID]) && matchesEvidence(candidate, want) {
			return true
		}
	}
	return false
}

func matchesEvidence(candidate engine.Candidate, want evaluate.Evidence) bool {
	return candidate.DocumentID == want.DocumentID && candidate.Version == want.Version &&
		candidate.Location == want.Location && want.ExcerptContains != "" &&
		strings.Contains(candidate.Text, want.ExcerptContains)
}

func validateDecision(decision Decision, candidates []engine.Candidate) error {
	known := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		known[candidate.ID] = true
	}
	seen := make(map[string]bool, len(decision.SupportedIDs))
	for _, id := range decision.SupportedIDs {
		if !known[id] || seen[id] {
			return fmt.Errorf("判定器引用未知或重复候选 ID")
		}
		seen[id] = true
	}
	if decision.Sufficient && len(decision.SupportedIDs) == 0 {
		return fmt.Errorf("判定器声称证据充分但没有支持 ID")
	}
	return nil
}

// Save writes a reproducible per-query report outside the tracked baseline.
func (r Report) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化旁路报告失败: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}
