package evaluate

import (
	"testing"
	"time"

	"ragbot/internal/types"
)

func TestScoreRefusalAndDenominators(t *testing.T) {
	answered := TestCase{ID: "A", ExpectDocs: []string{"guide"}, ExpectState: "answered"}
	refusal := TestCase{ID: "R", ExpectState: "insufficient_evidence"}
	hits := []types.Hit{{Section: types.Section{DocID: "guide", Version: "v1", SectionPath: "条件", Text: "需要申请"}}}
	citation := types.Citation{DocumentID: "guide", Version: "v1", Location: "条件", Excerpt: "需要申请"}
	rep := summarize([]Record{
		score(answered, types.Answer{Status: "answered", Citations: []types.Citation{citation}}, hits, hits, time.Millisecond),
		score(refusal, types.Answer{Status: "insufficient_evidence"}, hits, hits, time.Microsecond),
	})
	if rep.Passed != 2 || rep.Hit5Total != 1 || rep.CitedTotal != 1 || rep.RefusalTotal != 1 || rep.RefusalPassed != 1 {
		t.Errorf("summarize(answered, refusal) = %+v, want passed=2 and denominators 1/1/1", rep)
	}
	if rep.Hit5Rate != 1 || rep.CitedRate != 1 || rep.RefusalRate != 1 {
		t.Errorf("summarize(answered, refusal) rates = %v/%v/%v, want 1/1/1", rep.Hit5Rate, rep.CitedRate, rep.RefusalRate)
	}
	if rep.Records[1].Hit5 || rep.Records[1].Cited || !rep.Records[1].Pass {
		t.Errorf("score(refusal) = %+v, want no hit/citation and pass", rep.Records[1])
	}
	if rep.AvgLatencyMs <= 0 || rep.Records[1].LatencyMs <= 0 {
		t.Errorf("summarize(latencies) = %v, want positive sub-millisecond latency", rep.AvgLatencyMs)
	}
}

func TestScoreRequiresAllRetrievalAndCitationSources(t *testing.T) {
	tc := TestCase{ID: "multi", ExpectDocs: []string{"a", "b"}, ExpectSources: []Source{{DocumentID: "a", Version: "v1"}, {DocumentID: "b", Version: "v2"}}, ExpectState: "answered"}
	a := types.Hit{Section: types.Section{DocID: "a", Version: "v1", SectionPath: "A", Text: "甲正文"}}
	b := types.Hit{Section: types.Section{DocID: "b", Version: "v2", SectionPath: "B", Text: "乙正文"}}
	ca := types.Citation{DocumentID: "a", Version: "v1", Location: "A", Excerpt: "甲正文"}
	cb := types.Citation{DocumentID: "b", Version: "v2", Location: "B", Excerpt: "乙正文"}

	rec := score(tc, types.Answer{Status: "answered", Citations: []types.Citation{ca}}, []types.Hit{a}, []types.Hit{a, b}, time.Millisecond)
	if rec.Hit5 || rec.SourceCoverage || rec.Pass || len(rec.MissingSources) != 1 {
		t.Errorf("score(missing b) = %+v, want hit=false, source=false, pass=false, one missing source", rec)
	}

	rec = score(tc, types.Answer{Status: "answered", Citations: []types.Citation{ca}}, []types.Hit{a, b}, []types.Hit{a, b}, time.Millisecond)
	if !rec.Hit5 || rec.SourceCoverage || rec.Pass {
		t.Errorf("score(b retrieved but not cited) = %+v, want hit=true, source=false, pass=false", rec)
	}

	rec = score(tc, types.Answer{Status: "answered", Citations: []types.Citation{ca, cb}}, []types.Hit{a, b}, []types.Hit{a, b}, time.Millisecond)
	if !rec.Hit5 || !rec.SourceCoverage || !rec.CitationsLocated || !rec.Pass {
		t.Errorf("score(all sources) = %+v, want hit/source/location/pass=true", rec)
	}

	// A partial version annotation must not silently drop other expected docs.
	tc.ExpectSources = tc.ExpectSources[:1]
	rec = score(tc, types.Answer{Status: "answered", Citations: []types.Citation{ca}}, []types.Hit{a, b}, []types.Hit{a, b}, time.Millisecond)
	if rec.SourceCoverage || rec.Pass || len(rec.MissingSources) != 1 {
		t.Errorf("score(partial version labels) = %+v, want b still required", rec)
	}
}

func TestScoreRejectsWrongVersionAndUnlocatedExcerpt(t *testing.T) {
	tc := TestCase{ID: "version", ExpectDocs: []string{"guide"}, ExpectSources: []Source{{DocumentID: "guide", Version: "v2"}}, ExpectState: "answered"}
	hit := types.Hit{Section: types.Section{DocID: "guide", Version: "v2", SectionPath: "材料", Text: "四项材料"}}
	for _, tt := range []struct {
		name        string
		cit         types.Citation
		wantLocated bool
	}{
		{"wrong version", types.Citation{DocumentID: "guide", Version: "v1", Location: "材料", Excerpt: "四项材料"}, false},
		{"invented excerpt", types.Citation{DocumentID: "guide", Version: "v2", Location: "材料", Excerpt: "五项材料"}, false},
		{"wrong location", types.Citation{DocumentID: "guide", Version: "v2", Location: "费用", Excerpt: "四项材料"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := score(tc, types.Answer{Status: "answered", Citations: []types.Citation{tt.cit}}, []types.Hit{hit}, []types.Hit{hit}, time.Millisecond)
			if rec.Pass || rec.CitationsLocated != tt.wantLocated {
				t.Errorf("score(%s) = %+v, want pass=false and citations_located=%t", tt.name, rec, tt.wantLocated)
			}
		})
	}
}

func TestScoreDistinguishesLocatedFromSupportingCitations(t *testing.T) {
	tc := TestCase{
		ID: "evidence", ExpectDocs: []string{"guide"}, ExpectState: "answered",
		ExpectEvidence: []Evidence{
			{DocumentID: "guide", Version: "v1", Location: "费用", ExcerptContains: "报名不收费"},
			{DocumentID: "guide", Version: "v1", Location: "材料", ExcerptContains: "提交申请表"},
		},
	}
	hits := []types.Hit{
		{Section: types.Section{DocID: "guide", Version: "v1", SectionPath: "费用", Text: "报名不收费。"}},
		{Section: types.Section{DocID: "guide", Version: "v1", SectionPath: "材料", Text: "提交申请表。"}},
		{Section: types.Section{DocID: "guide", Version: "v1", SectionPath: "时间", Text: "活动前三天预约。"}},
	}
	fee := types.Citation{DocumentID: "guide", Version: "v1", Location: "费用", Excerpt: "报名不收费。"}
	material := types.Citation{DocumentID: "guide", Version: "v1", Location: "材料", Excerpt: "提交申请表。"}
	noise := types.Citation{DocumentID: "guide", Version: "v1", Location: "时间", Excerpt: "活动前三天预约。"}

	partial := score(tc, types.Answer{Status: "answered", Citations: []types.Citation{fee, noise}}, hits, hits, time.Millisecond)
	if !partial.CitationsLocated || !partial.SourceCoverage || partial.EvidenceCoverage || partial.SupportedCitations != 1 || partial.ScoredCitations != 2 || len(partial.UnsupportedCitations) != 1 || len(partial.MissingEvidence) != 1 || partial.Pass {
		t.Errorf("score(evidence, fee and noise) = %+v, want located sources but missing evidence, one unsupported citation, and pass=false", partial)
	}

	complete := score(tc, types.Answer{Status: "answered", Citations: []types.Citation{fee, material}}, hits, hits, time.Millisecond)
	if !complete.EvidenceCoverage || complete.SupportedCitations != 2 || len(complete.UnsupportedCitations) != 0 || !complete.Pass {
		t.Errorf("score(evidence, fee and material) = %+v, want complete supporting evidence and pass=true", complete)
	}
	fabricated := fee
	fabricated.Excerpt = "报名不收费，并且赠送礼品。"
	invalid := score(tc, types.Answer{Status: "answered", Citations: []types.Citation{fabricated, material}}, hits, hits, time.Millisecond)
	if invalid.CitationsLocated || invalid.SupportedCitations != 1 || len(invalid.UnsupportedCitations) != 1 || invalid.Pass {
		t.Errorf("score(evidence, fabricated excerpt) = %+v, want unlocated and unsupported citation with pass=false", invalid)
	}

	rep := summarize([]Record{partial, complete})
	if rep.EvidencePassed != 1 || rep.EvidenceTotal != 2 || rep.SupportedCitations != 3 || rep.ScoredCitations != 4 || rep.CitationPrecision != 0.75 {
		t.Errorf("summarize(partial, complete) = %+v, want evidence=1/2 and citation precision=3/4", rep)
	}
}

func TestOptionalEvidenceAllowsExtraRelevantSource(t *testing.T) {
	tc := TestCase{
		ID: "history", ExpectDocs: []string{"history"}, ExpectState: "answered",
		ExpectEvidence: []Evidence{
			{DocumentID: "history", Version: "v1", Location: "材料", ExcerptContains: "三项材料"},
			{DocumentID: "guide", Version: "v1", Location: "材料", ExcerptContains: "三项材料", Optional: true},
		},
	}
	hits := []types.Hit{
		{Section: types.Section{DocID: "history", Version: "v1", SectionPath: "材料", Text: "2025 年提交三项材料。"}},
		{Section: types.Section{DocID: "guide", Version: "v1", SectionPath: "材料", Text: "提交三项材料。"}},
	}
	primary := types.Citation{DocumentID: "history", Version: "v1", Location: "材料", Excerpt: "2025 年提交三项材料。"}
	secondary := types.Citation{DocumentID: "guide", Version: "v1", Location: "材料", Excerpt: "提交三项材料。"}
	for _, citations := range [][]types.Citation{{primary}, {primary, secondary}} {
		rec := score(tc, types.Answer{Status: "answered", Citations: citations}, hits, hits, time.Millisecond)
		if !rec.Pass || rec.SupportedCitations != len(citations) || !rec.EvidenceCoverage {
			t.Errorf("score(history, citations=%+v) = %+v, want pass=true, all citations supported and required evidence covered", citations, rec)
		}
	}
}

func TestScoreCountsUnsupportedAnswerAsRefusalFailure(t *testing.T) {
	tc := TestCase{ID: "missing-fee", Query: "报名费用是多少", ExpectState: "insufficient_evidence"}
	answer := types.Answer{
		Status:    "answered",
		Citations: []types.Citation{{DocumentID: "plan", Version: "v1", Location: "入场券", Excerpt: "领取入场券"}},
	}
	rec := score(tc, answer, nil, nil, time.Millisecond)
	rep := summarize([]Record{rec})
	if rec.Refusal || rec.Pass || rep.RefusalPassed != 0 || rep.RefusalTotal != 1 || rep.Passed != 0 {
		t.Errorf("score(%q, %+v) = %+v; summarize = %+v, want refusal failure 0/1", tc.Query, answer, rec, rep)
	}
}
