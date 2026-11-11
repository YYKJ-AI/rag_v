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
