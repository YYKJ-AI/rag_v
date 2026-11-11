package engine_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"ragbot/internal/engine"
	"ragbot/internal/ingest"
	"ragbot/internal/retriever"
	"ragbot/internal/store"
	"ragbot/internal/types"
)

func engineFromDocuments(t *testing.T, documents ...string) *engine.Engine {
	t.Helper()
	st := store.New(t.TempDir() + "/store.json")
	for _, raw := range documents {
		meta, drafts, err := ingest.Parse([]byte(raw))
		if err != nil {
			t.Fatalf("ingest.Parse(test document) error = %v, want nil", err)
		}
		doc, sections, chunks, err := ingest.Build(meta, drafts)
		if err != nil {
			t.Fatalf("ingest.Build(%s) error = %v, want nil", meta.ID, err)
		}
		if _, err := st.Ingest(doc, sections, chunks); err != nil {
			t.Fatalf("Store.Ingest(%s) error = %v, want nil", meta.ID, err)
		}
	}
	r := retriever.New()
	r.Index(st.Docs, st.Sections, st.Chunks)
	return engine.New(r)
}

func versionDocument(id, version, date, replaces, claim string) string {
	return fmt.Sprintf("---\nid: %s\nversion: %s\ntitle: 申请指南\neffective_date: %s\nreplaces: %s\n---\n# 申请指南\n## 申请材料\n申请需要提交%s项材料。\n", id, version, date, replaces, claim)
}

func TestVersionSelectionByBusinessDate(t *testing.T) {
	futureDate := time.Now().AddDate(2, 0, 0).Format("2006-01-02")
	e := engineFromDocuments(t,
		versionDocument("guide", "v1", "2020-01-01", "", "三"),
		versionDocument("guide", "v2", "2021-01-01", "guide@v1", "四"),
		versionDocument("guide", "v3", futureDate, "guide@v2", "五"),
	)
	query := "申请需要提交几项材料"
	current := e.Answer(query, nil)
	assertOnlyVersion(t, current, "v2")
	if strings.Contains(current.Answer, "已废止") {
		t.Errorf("Answer(%q, now) = %q, want current v2 without superseded label", query, current.Answer)
	}
	old := time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC)
	historical := e.Answer(query, &old)
	assertOnlyVersion(t, historical, "v1")
	if strings.Contains(historical.Answer, "已废止") {
		t.Errorf("Answer(%q, 2020-06-01) = %q, want historically active v1 without superseded label", query, historical.Answer)
	}
	afterFuture := time.Now().AddDate(3, 0, 0)
	assertOnlyVersion(t, e.Answer(query, &afterFuture), "v3")
	for _, hit := range e.Top(query, 5) {
		if hit.Section.Version != "v2" {
			t.Errorf("Top(%q, 5) includes %s, want only currently eligible v2", query, hit.Section.Version)
		}
	}
}

func TestCrossDocumentReplacement(t *testing.T) {
	e := engineFromDocuments(t,
		versionDocument("old-policy", "v1", "2020-01-01", "", "三"),
		versionDocument("new-policy", "v1", "2021-01-01", "old-policy@v1", "四"),
	)
	current := e.Answer("申请需要提交几项材料", nil)
	if current.Status != "answered" || len(current.Citations) == 0 {
		t.Fatalf("Answer(cross-ID replacement) = %+v, want answered with citations", current)
	}
	for _, citation := range current.Citations {
		if citation.DocumentID != "new-policy" {
			t.Errorf("Answer(cross-ID replacement) cites %s, want only new-policy", citation.DocumentID)
		}
	}
}

func TestAnswerWithScopeKey(t *testing.T) {
	residents := "---\nid: resident-fee\nversion: v1\ntitle: 申请费用\neffective_date: 2020-01-01\nscope_keys: resident\n---\n# 申请费用\n## 费用标准\n居民申请费用为一百元。\n"
	organizations := "---\nid: org-fee\nversion: v1\ntitle: 申请费用\neffective_date: 2020-01-01\nscope_keys: organization\n---\n# 申请费用\n## 费用标准\n组织申请费用为二百元。\n"
	e := engineFromDocuments(t, residents, organizations)
	query := "申请费用是多少元"
	for _, tt := range []struct {
		key, wantDoc string
	}{
		{"resident", "resident-fee"},
		{"organization", "org-fee"},
	} {
		ans := e.AnswerWithScope(query, nil, tt.key)
		if ans.Status != "answered" || len(ans.Citations) == 0 {
			t.Errorf("AnswerWithScope(%q, %q) = %+v, want answered with citations", query, tt.key, ans)
			continue
		}
		for _, citation := range ans.Citations {
			if citation.DocumentID != tt.wantDoc {
				t.Errorf("AnswerWithScope(%q, %q) cites %s, want %s", query, tt.key, citation.DocumentID, tt.wantDoc)
			}
		}
	}
	ans := e.AnswerWithScope(query, nil, "unknown")
	if ans.Status != "insufficient_evidence" || len(ans.Citations) != 0 {
		t.Errorf("AnswerWithScope(%q, unknown) = %+v, want refusal without citations", query, ans)
	}
}

func TestReplacementAppliesWithinRequestedScope(t *testing.T) {
	old := "---\nid: fee\nversion: v1\ntitle: 申请费用\neffective_date: 2020-01-01\nscope_keys: resident,organization\n---\n# 申请费用\n## 费用标准\n申请费用为一百元。\n"
	newer := "---\nid: fee\nversion: v2\ntitle: 申请费用\neffective_date: 2021-01-01\nscope_keys: organization\nreplaces: fee@v1\n---\n# 申请费用\n## 费用标准\n申请费用为二百元。\n"
	e := engineFromDocuments(t, old, newer)
	query := "申请费用是多少元"
	assertOnlyVersion(t, e.AnswerWithScope(query, nil, "resident"), "v1")
	assertOnlyVersion(t, e.AnswerWithScope(query, nil, "organization"), "v2")
}

func TestDocumentValidityEndIsInclusive(t *testing.T) {
	raw := "---\nid: annual-fee\nversion: v1\ntitle: 申请费用\neffective_date: 2020-01-01\nvalid_until: 2020-12-31\n---\n# 申请费用\n## 费用标准\n申请费用为一百元。\n"
	e := engineFromDocuments(t, raw)
	query := "申请费用是多少元"
	lastDay := time.Date(2020, 12, 31, 0, 0, 0, 0, time.UTC)
	if got := e.Answer(query, &lastDay).Status; got != "answered" {
		t.Errorf("Answer(%q, 2020-12-31).Status = %s, want answered", query, got)
	}
	nextDay := lastDay.AddDate(0, 0, 1)
	if got := e.Answer(query, &nextDay).Status; got != "insufficient_evidence" {
		t.Errorf("Answer(%q, 2021-01-01).Status = %s, want insufficient_evidence", query, got)
	}
}

func assertOnlyVersion(t *testing.T, ans types.Answer, version string) {
	t.Helper()
	if ans.Status != "answered" || len(ans.Citations) == 0 {
		t.Fatalf("Answer(version=%s) = %+v, want answered with citations", version, ans)
	}
	for _, citation := range ans.Citations {
		if citation.Version != version {
			t.Errorf("Answer(version=%s) cites %s, want only %s", version, citation.Version, version)
		}
	}
}
