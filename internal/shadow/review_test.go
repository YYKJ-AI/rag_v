package shadow

import "testing"

func TestScoreAtomicReviewCountsMissingAndExtraRequirements(t *testing.T) {
	a := AtomicRequirement{Subject: "活动", Action: "办理", Object: "报备", Modality: "哪些"}
	b := AtomicRequirement{Subject: "活动", Action: "办理", Object: "许可", Modality: "哪些"}
	extra := AtomicRequirement{Subject: "活动", Action: "缴纳", Object: "场租", Modality: "哪些"}
	report := AtomicComparison{PromptFingerprint: "prompt", Records: []AtomicComparisonRecord{{
		ID: "q", PoolFingerprint: "pool", New: &ProtocolScore{},
		GoldRequirements: []AtomicLabel{{ID: "report"}, {ID: "permit"}},
		AtomicDecision:   &AtomicDecision{Decomposed: []AtomicRequirement{a, b, extra}},
	}, {ID: "failed", Error: "invalid model response"}}}
	review := AtomicReview{PromptFingerprint: "prompt", ReviewStatus: "provisional", Entries: []AtomicAlignment{{
		ID: "q", PoolFingerprint: "pool", ModelRequirements: []AtomicRequirement{a, b, extra}, MatchGoldIDs: []string{"report", "permit", ""},
	}}}
	got, err := ScoreAtomicReview(report, review)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReviewedCases != 1 || got.ExcludedModelErrors != 1 || got.MatchedGold != 2 || got.GoldRequirements != 2 || got.MatchedModel != 2 || got.ModelRequirements != 3 || got.AllGoldCoveredCases != 1 || got.CleanCases != 0 {
		t.Errorf("ScoreAtomicReview(one extra, one error) = %+v, want 2/2 gold, 2/3 model, 1 covered, 0 clean, 1 excluded", got)
	}
	review.Entries[0].ModelRequirements[1].Object = "altered"
	if got, err := ScoreAtomicReview(report, review); err == nil {
		t.Errorf("ScoreAtomicReview(altered model requirement) = %+v, nil; want mismatch error", got)
	}
}
