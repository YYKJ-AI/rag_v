package shadow

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"ragbot/internal/engine"
	"ragbot/internal/evaluate"
)

// AtomicLabel is a pre-reviewed requirement. Each evidence set is an AND;
// any one set may satisfy the requirement. An empty set list means no answer is
// present in the corpus. These labels are for audit, never sent to the model.
type AtomicLabel struct {
	ID           string                `json:"id"`
	Requirement  string                `json:"requirement"`
	EvidenceSets [][]evaluate.Evidence `json:"evidence_sets"`
}

// AtomicCase contains one pre-reviewed query and its expected evidence logic.
type AtomicCase struct {
	ID           string        `json:"id"`
	Query        string        `json:"query"`
	ExpectStatus string        `json:"expect_status"`
	Requirements []AtomicLabel `json:"requirements"`
	Note         string        `json:"note,omitempty"`
}

// LoadAtomicCases validates the manually reviewed evidence groups.
func LoadAtomicCases(path string) ([]AtomicCase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取原子要求标注失败: %w", err)
	}
	var cases []AtomicCase
	if err := json.Unmarshal(data, &cases); err != nil {
		return nil, fmt.Errorf("解析原子要求标注失败: %w", err)
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("原子要求标注为空")
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		if strings.TrimSpace(tc.ID) == "" || seen[tc.ID] || strings.TrimSpace(tc.Query) == "" ||
			(tc.ExpectStatus != "answered" && tc.ExpectStatus != "insufficient_evidence") || len(tc.Requirements) == 0 {
			return nil, fmt.Errorf("原子要求标注题目无效: %q", tc.ID)
		}
		seen[tc.ID] = true
		reqSeen := map[string]bool{}
		answerable := true
		for _, req := range tc.Requirements {
			if strings.TrimSpace(req.ID) == "" || reqSeen[req.ID] || strings.TrimSpace(req.Requirement) == "" {
				return nil, fmt.Errorf("题目 %s 的原子要求无效", tc.ID)
			}
			reqSeen[req.ID] = true
			if len(req.EvidenceSets) == 0 {
				answerable = false
			}
			for _, set := range req.EvidenceSets {
				if len(set) == 0 {
					return nil, fmt.Errorf("题目 %s 的证据组为空", tc.ID)
				}
				for _, ev := range set {
					if ev.Optional || ev.DocumentID == "" || ev.Version == "" || ev.Location == "" || ev.ExcerptContains == "" {
						return nil, fmt.Errorf("题目 %s 的证据定位无效", tc.ID)
					}
				}
			}
		}
		if (tc.ExpectStatus == "answered") != answerable {
			return nil, fmt.Errorf("题目 %s 的状态与证据组不符", tc.ID)
		}
	}
	return cases, nil
}

// evidenceSetSatisfied checks document/version/location/text for every member.
func evidenceSetSatisfied(candidates []engine.Candidate, selected map[string]bool, set []evaluate.Evidence) bool {
	for _, ev := range set {
		if !hasEvidence(candidates, ev, selected) {
			return false
		}
	}
	return true
}

func requirementSatisfied(candidates []engine.Candidate, selected map[string]bool, label AtomicLabel) bool {
	for _, set := range label.EvidenceSets {
		if evidenceSetSatisfied(candidates, selected, set) {
			return true
		}
	}
	return false
}
