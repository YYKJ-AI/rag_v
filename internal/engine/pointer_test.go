package engine_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"ragbot/internal/types"
)

func pointerDoc(id, version, date, scope, replaces, body string) string {
	return fmt.Sprintf("---\nid: %s\nversion: %s\ntitle: 社区申请指南\neffective_date: %s\nscope_keys: %s\nreplaces: %s\n---\n# 社区申请指南\n%s\n", id, version, date, scope, replaces, body)
}

func TestInheritedClauseShowsBothSources(t *testing.T) {
	v1 := pointerDoc("guide", "v1", "2020-01-01", "resident", "", "## 申请人条件\n申请人须年满十八周岁，并具有本社区户籍。")
	v2 := pointerDoc("guide", "v2", "2021-01-01", "resident", "guide@v1", "## 申请人条件\n申请人条件沿用 guide@v1#sec-1，不作调整。")
	e := engineFromDocuments(t, v1, v2)
	query := "现在申请人条件是什么"
	answer := e.AnswerWithScope(query, nil, "resident")
	if answer.Status != "answered" {
		t.Fatalf("AnswerWithScope(%q, resident) = %+v, want answered", query, answer)
	}
	if len(answer.Citations) != 2 {
		t.Fatalf("AnswerWithScope(%q, resident).Citations = %+v, want 2 source citations", query, answer.Citations)
	}
	if answer.Citations[0].Role != "current_pointer" || answer.Citations[1].Role != "inherited_clause" {
		t.Errorf("AnswerWithScope(%q, resident).Citations = %+v, want current_pointer then inherited_clause roles", query, answer.Citations)
	}
	if !hasCitation(answer.Citations, "guide", "v2", "申请人条件沿用") || !hasCitation(answer.Citations, "guide", "v1", "年满十八周岁") {
		t.Errorf("AnswerWithScope(%q, resident).Citations = %+v, want v2 pointer and v1 clause", query, answer.Citations)
	}
	if !strings.Contains(answer.Answer, "具体条款来自") || !strings.Contains(answer.Answer, "v1") {
		t.Errorf("AnswerWithScope(%q, resident).Answer = %q, want explicit inherited-source label", query, answer.Answer)
	}
	old := time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC)
	historical := e.AnswerWithScope(query, &old, "resident")
	assertOnlyVersion(t, historical, "v1")
}

func TestPointerMissingTargetRefuses(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"implicit", "申请人条件沿用原规定，不作调整。"},
		{"missing-document", "申请人条件沿用 absent@v1#sec-1，不作调整。"},
		{"missing-section", "申请人条件沿用 guide@v1#sec-9，不作调整。"},
		{"ambiguous-targets", "申请人条件沿用 guide@v1#sec-1 或 guide@v1#sec-2。"},
		{"cycle", "申请人条件沿用 guide@v2#sec-1，不作调整。"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v1 := pointerDoc("guide", "v1", "2020-01-01", "resident", "", "## 申请人条件\n申请人须年满十八周岁。")
			v2 := pointerDoc("guide", "v2", "2021-01-01", "resident", "guide@v1", "## 申请人条件\n"+tc.body)
			e := engineFromDocuments(t, v1, v2)
			answer := e.AnswerWithScope("现在申请人条件是什么", nil, "resident")
			if answer.Status != "insufficient_evidence" || len(answer.Citations) != 0 {
				t.Errorf("AnswerWithScope(%q, resident) with %s = %+v, want refusal without citations", "现在申请人条件是什么", tc.name, answer)
			}
		})
	}
}

func TestPointerScopeMismatchAndReferenceDoNotInherit(t *testing.T) {
	v1 := pointerDoc("guide", "v1", "2020-01-01", "organization", "", "## 申请人条件\n申请人须年满十八周岁。")
	v2 := pointerDoc("guide", "v2", "2021-01-01", "resident", "guide@v1", "## 申请人条件\n申请人条件沿用 guide@v1#sec-1，不作调整。")
	answer := engineFromDocuments(t, v1, v2).AnswerWithScope("现在申请人条件是什么", nil, "resident")
	if answer.Status != "insufficient_evidence" {
		t.Errorf("AnswerWithScope(scope mismatch).Status = %s, want insufficient_evidence", answer.Status)
	}
	answer = engineFromDocuments(t, v1, v2).Answer("现在申请人条件是什么", nil)
	if answer.Status != "insufficient_evidence" {
		t.Errorf("Answer(unscoped with disjoint document scopes).Status = %s, want insufficient_evidence", answer.Status)
	}
	reference := pointerDoc("guide", "v2", "2021-01-01", "organization", "guide@v1", "## 申请人条件\n申请人条件参照 guide@v1#sec-1。")
	answer = engineFromDocuments(t, v1, reference).AnswerWithScope("现在申请人条件是什么", nil, "organization")
	if answer.Status != "insufficient_evidence" {
		t.Errorf("AnswerWithScope(reference-only).Status = %s, want insufficient_evidence", answer.Status)
	}
	query := "申请人条件参照了哪个条款"
	answer = engineFromDocuments(t, v1, reference).AnswerWithScope(query, nil, "organization")
	if answer.Status != "answered" || len(answer.Citations) != 1 || answer.Citations[0].Version != "v2" || !strings.Contains(answer.Answer, "guide") {
		t.Errorf("AnswerWithScope(%q, organization) = %+v, want source-only pointer location answer", query, answer)
	}
}

func TestPointerDoesNotFollowSecondHopOrFutureTarget(t *testing.T) {
	old := pointerDoc("guide", "v1", "2020-01-01", "resident", "", "## 申请人条件\n申请人条件沿用 archive@v1#sec-1。")
	current := pointerDoc("guide", "v2", "2021-01-01", "resident", "guide@v1", "## 申请人条件\n申请人条件沿用 guide@v1#sec-1。")
	archive := pointerDoc("archive", "v1", "2019-01-01", "resident", "", "## 申请人条件\n申请人须年满十八周岁。")
	query := "现在申请人条件是什么"
	answer := engineFromDocuments(t, archive, old, current).AnswerWithScope(query, nil, "resident")
	if answer.Status != "insufficient_evidence" || len(answer.Citations) != 0 {
		t.Errorf("AnswerWithScope(%q, resident) with two-hop pointer = %+v, want refusal", query, answer)
	}
	future := pointerDoc("future", "v1", "2030-01-01", "resident", "", "## 申请人条件\n申请人须年满二十周岁。")
	current = pointerDoc("guide", "v2", "2021-01-01", "resident", "guide@v1", "## 申请人条件\n申请人条件沿用 future@v1#sec-1。")
	answer = engineFromDocuments(t, future, current).AnswerWithScope(query, nil, "resident")
	if answer.Status != "insufficient_evidence" || len(answer.Citations) != 0 {
		t.Errorf("AnswerWithScope(%q, resident) with future target = %+v, want refusal", query, answer)
	}
}

func hasCitation(citations []types.Citation, id, version, excerptPart string) bool {
	for _, citation := range citations {
		if citation.DocumentID == id && citation.Version == version && strings.Contains(citation.Excerpt, excerptPart) {
			return true
		}
	}
	return false
}
