package shadow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

// AtomicAlignment ties a reviewed model decomposition to a fixed report.
// Empty gold IDs mean the generated requirement is extraneous.
type AtomicAlignment struct {
	ID                string              `json:"id"`
	PoolFingerprint   string              `json:"pool_fingerprint"`
	ModelRequirements []AtomicRequirement `json:"model_requirements"`
	MatchGoldIDs      []string            `json:"match_gold_ids"`
}

// AtomicReview is a separate, explicit alignment; the model never sees it.
type AtomicReview struct {
	PromptFingerprint string            `json:"prompt_fingerprint"`
	ReviewStatus      string            `json:"review_status"`
	Entries           []AtomicAlignment `json:"entries"`
}

// AtomicReviewScore reports decomposition quality on reviewed valid judgments.
type AtomicReviewScore struct {
	ReviewStatus        string  `json:"review_status"`
	ReviewedCases       int     `json:"reviewed_cases"`
	ExcludedModelErrors int     `json:"excluded_model_errors"`
	GoldRequirements    int     `json:"gold_requirements"`
	MatchedGold         int     `json:"matched_gold"`
	ModelRequirements   int     `json:"model_requirements"`
	MatchedModel        int     `json:"matched_model"`
	AllGoldCoveredCases int     `json:"all_gold_covered_cases"`
	CleanCases          int     `json:"clean_cases"`
	Recall              float64 `json:"recall"`
	Precision           float64 `json:"precision"`
}

// LoadAtomicReview reads a human or researcher authored alignment file.
func LoadAtomicReview(path string) (AtomicReview, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return AtomicReview{}, fmt.Errorf("读取原子要求复核失败: %w", err)
	}
	var review AtomicReview
	if err := json.Unmarshal(data, &review); err != nil {
		return AtomicReview{}, fmt.Errorf("解析原子要求复核失败: %w", err)
	}
	return review, nil
}

// LoadAtomicComparison reads the fixed-pool model output before review.
func LoadAtomicComparison(path string) (AtomicComparison, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return AtomicComparison{}, fmt.Errorf("读取原子协议报告失败: %w", err)
	}
	var report AtomicComparison
	if err := json.Unmarshal(data, &report); err != nil {
		return AtomicComparison{}, fmt.Errorf("解析原子协议报告失败: %w", err)
	}
	if len(report.Records) == 0 {
		return AtomicComparison{}, fmt.Errorf("原子协议报告为空")
	}
	return report, nil
}

// ScoreAtomicReview validates alignment against exact model output, then counts
// missed gold requirements and extra generated requirements separately.
func ScoreAtomicReview(report AtomicComparison, review AtomicReview) (AtomicReviewScore, error) {
	if review.PromptFingerprint == "" || review.PromptFingerprint != report.PromptFingerprint ||
		(review.ReviewStatus != "provisional" && review.ReviewStatus != "adjudicated") {
		return AtomicReviewScore{}, fmt.Errorf("原子复核提示词指纹或评审状态无效")
	}
	byID := make(map[string]AtomicAlignment, len(review.Entries))
	for _, entry := range review.Entries {
		if entry.ID == "" {
			return AtomicReviewScore{}, fmt.Errorf("原子复核 ID 为空")
		}
		if _, exists := byID[entry.ID]; exists {
			return AtomicReviewScore{}, fmt.Errorf("原子复核重复 ID: %s", entry.ID)
		}
		byID[entry.ID] = entry
	}
	score := AtomicReviewScore{ReviewStatus: review.ReviewStatus}
	for _, rec := range report.Records {
		if rec.New == nil || rec.AtomicDecision == nil {
			if rec.Error != "" {
				score.ExcludedModelErrors++
			}
			if _, exists := byID[rec.ID]; exists {
				return AtomicReviewScore{}, fmt.Errorf("无法复核未完成的题目: %s", rec.ID)
			}
			continue
		}
		entry, exists := byID[rec.ID]
		if !exists {
			return AtomicReviewScore{}, fmt.Errorf("缺少原子复核题目: %s", rec.ID)
		}
		delete(byID, rec.ID)
		generated := rec.AtomicDecision.Decomposed
		if len(generated) == 0 {
			for _, req := range rec.AtomicDecision.Requirements {
				generated = append(generated, req.Requirement)
			}
		}
		if entry.PoolFingerprint != rec.PoolFingerprint || !reflect.DeepEqual(entry.ModelRequirements, generated) || len(entry.MatchGoldIDs) != len(generated) {
			return AtomicReviewScore{}, fmt.Errorf("题目 %s 的复核内容与模型输出不一致", rec.ID)
		}
		goldIDs := map[string]bool{}
		for _, req := range rec.GoldRequirements {
			goldIDs[req.ID] = true
		}
		matched := map[string]bool{}
		for _, id := range entry.MatchGoldIDs {
			if id == "" {
				continue
			}
			if !goldIDs[id] || matched[id] {
				return AtomicReviewScore{}, fmt.Errorf("题目 %s 的金标匹配无效或重复", rec.ID)
			}
			matched[id] = true
			score.MatchedModel++
		}
		score.ReviewedCases++
		score.GoldRequirements += len(goldIDs)
		score.MatchedGold += len(matched)
		score.ModelRequirements += len(generated)
		if len(matched) == len(goldIDs) {
			score.AllGoldCoveredCases++
			if len(matched) == len(generated) {
				score.CleanCases++
			}
		}
	}
	if len(byID) > 0 {
		return AtomicReviewScore{}, fmt.Errorf("复核文件包含报告之外的题目")
	}
	if score.ReviewedCases == 0 || score.GoldRequirements == 0 || score.ModelRequirements == 0 {
		return AtomicReviewScore{}, fmt.Errorf("没有可复核的有效原子要求")
	}
	score.Recall = float64(score.MatchedGold) / float64(score.GoldRequirements)
	score.Precision = float64(score.MatchedModel) / float64(score.ModelRequirements)
	return score, nil
}

// Save writes the reviewed decomposition metrics to a local file.
func (s AtomicReviewScore) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化原子复核结果失败: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}
