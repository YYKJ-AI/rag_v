package shadow

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"ragbot/internal/engine"
)

// ProtocolScore checks a judgment against the same gold evidence groups.
type ProtocolScore struct {
	Sufficient       bool     `json:"sufficient"`
	EvidenceComplete bool     `json:"evidence_complete"`
	UnsupportedIDs   []string `json:"unsupported_ids,omitempty"`
	UnsafeAnswer     bool     `json:"unsafe_answer"`
	JudgeMiss        bool     `json:"judge_miss"`
	SelectedIDs      []string `json:"selected_ids"`
	LatencyMs        float64  `json:"latency_ms"`
}

// AtomicComparisonRecord keeps both judgments and the pool fingerprint.
type AtomicComparisonRecord struct {
	ID               string          `json:"id"`
	Query            string          `json:"query"`
	ExpectedStatus   string          `json:"expected_status"`
	PoolSize         int             `json:"pool_size"`
	PoolFingerprint  string          `json:"pool_fingerprint"`
	PoolComplete     bool            `json:"pool_complete"`
	GoldRequirements []AtomicLabel   `json:"gold_requirements"`
	Old              *ProtocolScore  `json:"old,omitempty"`
	New              *ProtocolScore  `json:"new,omitempty"`
	AtomicDecision   *AtomicDecision `json:"atomic_decision,omitempty"`
	Error            string          `json:"error,omitempty"`
	AttemptCalls     int             `json:"attempt_calls"`
	AttemptLatencyMs float64         `json:"attempt_latency_ms"`
	// Blind decomposition needs a separate human alignment before recall is scored.
	DecompositionReview string `json:"decomposition_review"`
}

// AtomicComparison summarizes paired protocol results and model costs.
type AtomicComparison struct {
	GeneratedAt         string                   `json:"generated_at"`
	InputReports        []string                 `json:"input_reports,omitempty"`
	Model               string                   `json:"model,omitempty"`
	PromptFingerprint   string                   `json:"prompt_fingerprint"`
	Cases               int                      `json:"cases"`
	PoolComplete        int                      `json:"pool_complete"`
	OldValid            int                      `json:"old_valid"`
	OldUnsafe           int                      `json:"old_unsafe"`
	OldMisses           int                      `json:"old_misses"`
	NewValid            int                      `json:"new_valid"`
	NewUnsafe           int                      `json:"new_unsafe"`
	NewMisses           int                      `json:"new_misses"`
	ModelErrors         int                      `json:"model_errors"`
	ModelCalls          int                      `json:"model_calls"`
	TotalModelLatencyMs float64                  `json:"total_model_latency_ms"`
	AvgNewLatencyMs     float64                  `json:"avg_new_latency_ms"`
	DecompositionRecall string                   `json:"decomposition_recall"`
	Records             []AtomicComparisonRecord `json:"records"`
}

// LoadShadowReport reads a prior semantic-shadow report for fixed-pool replay.
func LoadShadowReport(path string) (Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Report{}, fmt.Errorf("读取旁路报告失败: %w", err)
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil || len(report.Records) == 0 {
		return Report{}, fmt.Errorf("旁路报告为空或格式错误")
	}
	return report, nil
}

// CompareAtomic replays exactly the candidate arrays in prior reports. Gold is
// used only for scoring and is never passed to the model.
func CompareAtomic(ctx context.Context, reports []Report, labels []AtomicCase, judge AtomicJudge) (AtomicComparison, error) {
	byID := map[string]Record{}
	for _, report := range reports {
		for _, rec := range report.Records {
			if _, exists := byID[rec.ID]; exists {
				return AtomicComparison{}, fmt.Errorf("重复旁路题目 ID: %s", rec.ID)
			}
			byID[rec.ID] = rec
		}
	}
	result := AtomicComparison{
		GeneratedAt: time.Now().Format(time.RFC3339), Cases: len(labels),
		DecompositionRecall: "pending_manual_review",
		PromptFingerprint:   fmt.Sprintf("%x", sha256.Sum256([]byte(decomposePrompt+supportPrompt))),
	}
	var latencySum float64
	for _, label := range labels {
		prior, ok := byID[label.ID]
		if !ok || prior.Query != label.Query || prior.ExpectedStatus != label.ExpectStatus {
			return AtomicComparison{}, fmt.Errorf("题目 %s 缺少同问句、同预期状态的旧报告", label.ID)
		}
		rec := AtomicComparisonRecord{ID: label.ID, Query: label.Query, ExpectedStatus: label.ExpectStatus,
			PoolSize: len(prior.Candidates), GoldRequirements: label.Requirements, DecompositionReview: "pending_manual_review"}
		poolBytes, err := json.Marshal(prior.Candidates)
		if err != nil {
			return AtomicComparison{}, fmt.Errorf("题目 %s 的候选池无法序列化: %w", label.ID, err)
		}
		rec.PoolFingerprint = fmt.Sprintf("%x", sha256.Sum256(poolBytes))
		rec.PoolComplete = goldComplete(prior.Candidates, nil, label)
		if rec.PoolComplete {
			result.PoolComplete++
		}
		if prior.Decision != nil && prior.JudgeError == "" {
			if err := validateDecision(*prior.Decision, prior.Candidates); err != nil {
				return AtomicComparison{}, fmt.Errorf("题目 %s 的旧判定无效: %w", label.ID, err)
			}
			score := scoreProtocol(prior.Candidates, label, prior.Decision.SupportedIDs, prior.Decision.Sufficient, prior.JudgeLatencyMs, rec.PoolComplete)
			rec.Old = &score
			result.OldValid++
			if score.UnsafeAnswer {
				result.OldUnsafe++
			}
			if score.JudgeMiss {
				result.OldMisses++
			}
		}
		if judge != nil && len(prior.Candidates) > 0 {
			decision, err := judge.JudgeAtomic(ctx, label.Query, prior.Candidates)
			rec.AttemptCalls = decision.Calls
			rec.AttemptLatencyMs = decision.LatencyMs
			result.ModelCalls += decision.Calls
			result.TotalModelLatencyMs += decision.LatencyMs
			if err == nil {
				err = validateAtomicDecision(decision, prior.Candidates)
			}
			if err != nil {
				if decision.Calls > 0 {
					rec.AtomicDecision = &decision
				}
				rec.Error = err.Error()
				result.ModelErrors++
			} else {
				rec.AtomicDecision = &decision
				score := scoreProtocol(prior.Candidates, label, decision.SupportedIDs, decision.Sufficient, decision.LatencyMs, rec.PoolComplete)
				rec.New = &score
				result.NewValid++
				latencySum += decision.LatencyMs
				if score.UnsafeAnswer {
					result.NewUnsafe++
				}
				if score.JudgeMiss {
					result.NewMisses++
				}
			}
		}
		result.Records = append(result.Records, rec)
	}
	if result.NewValid > 0 {
		result.AvgNewLatencyMs = latencySum / float64(result.NewValid)
	}
	return result, nil
}

func goldComplete(candidates []engine.Candidate, selected map[string]bool, label AtomicCase) bool {
	for _, req := range label.Requirements {
		if !requirementSatisfied(candidates, selected, req) {
			return false
		}
	}
	return true
}

func scoreProtocol(candidates []engine.Candidate, label AtomicCase, ids []string, sufficient bool, latency float64, poolComplete bool) ProtocolScore {
	selected := make(map[string]bool, len(ids))
	for _, id := range ids {
		selected[id] = true
	}
	score := ProtocolScore{Sufficient: sufficient, EvidenceComplete: goldComplete(candidates, selected, label), SelectedIDs: ids, LatencyMs: latency}
	for _, candidate := range candidates {
		if !selected[candidate.ID] {
			continue
		}
		supported := false
		for _, req := range label.Requirements {
			for _, set := range req.EvidenceSets {
				for _, ev := range set {
					if matchesEvidence(candidate, ev) {
						supported = true
					}
				}
			}
		}
		if !supported {
			score.UnsupportedIDs = append(score.UnsupportedIDs, candidate.ID)
		}
	}
	score.UnsafeAnswer = sufficient && (label.ExpectStatus != "answered" || !score.EvidenceComplete || len(score.UnsupportedIDs) > 0)
	score.JudgeMiss = label.ExpectStatus == "answered" && poolComplete && !sufficient
	return score
}

func validateAtomicDecision(decision AtomicDecision, candidates []engine.Candidate) error {
	if len(decision.Requirements) < 1 || len(decision.Requirements) > 8 || decision.Calls != len(decision.Requirements)+1 {
		return fmt.Errorf("原子判定结构无效")
	}
	union := map[string]bool{}
	allSupported := true
	for _, req := range decision.Requirements {
		if req.Supported != (len(req.SupportedIDs) > 0) {
			return fmt.Errorf("原子要求支持状态与证据 ID 不一致")
		}
		if len(req.SupportedIDs) == 0 {
			allSupported = false
		}
		if err := validateDecision(Decision{SupportedIDs: req.SupportedIDs}, candidates); err != nil {
			return err
		}
		for _, id := range req.SupportedIDs {
			union[id] = true
		}
	}
	if decision.Sufficient != allSupported || len(decision.SupportedIDs) != len(union) {
		return fmt.Errorf("原子判定聚合与逐项结果不一致")
	}
	for _, id := range decision.SupportedIDs {
		if !union[id] {
			return fmt.Errorf("原子判定含额外候选 ID")
		}
		delete(union, id)
	}
	return nil
}

// Save writes the comparison report to a local path.
func (r AtomicComparison) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化原子判定对照失败: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}
