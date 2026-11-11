package engine_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ragbot/internal/types"
)

func workshopEngine(t *testing.T) func(string) types.Answer {
	t.Helper()
	var documents []string
	for _, name := range []string{"workshop-plan.md", "workshop-intro.md", "workshop-process.md", "workshop-budget.md"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "docs", name))
		if err != nil {
			t.Fatalf("os.ReadFile(%s) error = %v, want nil", name, err)
		}
		documents = append(documents, string(raw))
	}
	e := engineFromDocuments(t, documents...)
	return func(query string) types.Answer { return e.AnswerWithScope(query, nil, "workshop_applicant") }
}

func TestWorkshopDirectEvidenceAcrossDocumentTypes(t *testing.T) {
	answer := workshopEngine(t)
	for _, tt := range []struct {
		name, query, docID, excerpt, exclude string
	}{
		{"plan", "星河工作坊在哪里举办", "workshop-plan", "社区创作室", ""},
		{"introduction", "星河工作坊的活动主题是什么", "workshop-intro", "夜空色彩", ""},
		{"process", "报名星河工作坊第二步需要上传什么", "workshop-process", "监护人同意书", "第一步"},
		{"table", "星光画笔的单价是多少元", "workshop-budget", "星光画笔", "星图纸"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ans := answer(tt.query)
			if ans.Status != "answered" {
				t.Fatalf("Answer(%q).Status = %s, want answered", tt.query, ans.Status)
			}
			found := false
			for _, citation := range ans.Citations {
				if tt.name == "plan" && citation.DocumentID == "workshop-budget" {
					t.Errorf("Answer(%q) cites table header %q, want no unrelated budget citation", tt.query, citation.Excerpt)
				}
				if citation.DocumentID == tt.docID && strings.Contains(citation.Excerpt, tt.excerpt) && (tt.exclude == "" || !strings.Contains(citation.Excerpt, tt.exclude)) {
					found = true
				}
			}
			if !found {
				t.Errorf("Answer(%q).Citations = %+v, want %s excerpt containing %q", tt.query, ans.Citations, tt.docID, tt.excerpt)
			}
		})
	}
}

func TestWorkshopCombinedEvidenceRequiresBothSources(t *testing.T) {
	answer := workshopEngine(t)
	query := "报名星河工作坊要上传什么，入场券在哪里领取"
	ans := answer(query)
	if ans.Status != "answered" {
		t.Fatalf("Answer(%q).Status = %s, want answered", query, ans.Status)
	}
	found := map[string]bool{}
	for _, citation := range ans.Citations {
		found[citation.DocumentID] = true
	}
	if !found["workshop-process"] || !found["workshop-plan"] {
		t.Errorf("Answer(%q).Citations = %+v, want workshop-process and workshop-plan", query, ans.Citations)
	}
}

func TestWorkshopUnsupportedCalculationAndInferenceRefuse(t *testing.T) {
	answer := workshopEngine(t)
	for _, query := range []string{
		"星图纸和星光画笔合计多少钱",
		"能否推断星河工作坊的孩子会提高多少创造力",
	} {
		ans := answer(query)
		if ans.Status != "insufficient_evidence" || len(ans.Citations) != 0 {
			t.Errorf("Answer(%q) = %+v, want refusal without citations", query, ans)
		}
	}
}

func TestExplicitTotalMayBeQuotedDirectly(t *testing.T) {
	raw := `---
id: total-budget
version: v1
title: 物料预算
effective_date: 2025-01-01
---
# 物料预算
## 合计金额
星图纸和星光画笔合计金额为六百元。
`
	e := engineFromDocuments(t, raw)
	query := "星图纸和星光画笔合计多少钱"
	ans := e.Answer(query, nil)
	if ans.Status != "answered" || len(ans.Citations) == 0 || !strings.Contains(ans.Citations[0].Excerpt, "合计金额为六百元") {
		t.Errorf("Answer(%q) = %+v, want direct cited total", query, ans)
	}
}

func TestSupplementMethodHasDirectSource(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "art-guide-v2.md"))
	if err != nil {
		t.Fatalf("os.ReadFile(art-guide-v2.md) error = %v, want nil", err)
	}
	e := engineFromDocuments(t, string(raw))
	query := "补交材料的方式有哪些"
	ans := e.Answer(query, nil)
	found := false
	for _, citation := range ans.Citations {
		if strings.Contains(citation.Excerpt, "补交材料可通过社区服务平台线上提交") {
			found = true
		}
	}
	if ans.Status != "answered" || !found {
		t.Errorf("Answer(%q) = %+v, want cited supplement method", query, ans)
	}
}

func TestEligibilitySynonymFindsApplicantCriterion(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "art-guide-v2.md"))
	if err != nil {
		t.Fatalf("os.ReadFile(art-guide-v2.md) error = %v, want nil", err)
	}
	e := engineFromDocuments(t, string(raw))
	query := "报名艺术节有什么资格要求"
	ans := e.Answer(query, nil)
	found := false
	for _, citation := range ans.Citations {
		if strings.Contains(citation.Excerpt, "申请人须为年满十八周岁") {
			found = true
		}
	}
	if ans.Status != "answered" || !found {
		t.Errorf("Answer(%q) = %+v, want cited applicant criterion", query, ans)
	}
}

func TestSharedTitleTopicKeepsDocumentFocus(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", "docs"))
	if err != nil {
		t.Fatalf("os.ReadDir(docs) error = %v, want nil", err)
	}
	var documents []string
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "README.md" || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("..", "..", "docs", entry.Name()))
		if err != nil {
			t.Fatalf("os.ReadFile(%s) error = %v, want nil", entry.Name(), err)
		}
		documents = append(documents, string(raw))
	}
	e := engineFromDocuments(t, documents...)
	for _, tt := range []struct {
		query, wantDoc, unwantedDoc string
	}{
		{query: "报名艺术节有什么资格要求", wantDoc: "art-guide", unwantedDoc: "venue-notice"},
		{query: "星河工作坊在哪里举办", wantDoc: "workshop-plan", unwantedDoc: "venue-notice"},
		{query: "现在申请要交哪些材料", wantDoc: "art-guide", unwantedDoc: ""},
	} {
		t.Run(tt.query, func(t *testing.T) {
			ans := e.Answer(tt.query, nil)
			found := false
			for _, citation := range ans.Citations {
				if citation.DocumentID == tt.wantDoc {
					found = true
				}
				if tt.unwantedDoc != "" && citation.DocumentID == tt.unwantedDoc {
					t.Errorf("Answer(%q) cites %s, want no unrelated %s citation", tt.query, citation.DocumentID, tt.unwantedDoc)
				}
			}
			if ans.Status != "answered" || !found {
				t.Errorf("Answer(%q) = %+v, want answered citing %s", tt.query, ans, tt.wantDoc)
			}
		})
	}
}
