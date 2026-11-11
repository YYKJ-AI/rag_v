package engine_test

import (
	"testing"

	"ragbot/internal/engine"
	"ragbot/internal/ingest"
	"ragbot/internal/retriever"
	"ragbot/internal/types"
)

func TestUnstructuredDocumentIsSearchableButNotAnswerEvidence(t *testing.T) {
	meta, drafts, err := ingest.ParseUnstructured([]byte("## 申请条件\n申请人须提交身份证明材料。"), "guide.md")
	if err != nil {
		t.Fatalf("ParseUnstructured(guide.md) error = %v", err)
	}
	doc, sections, chunks, err := ingest.Build(meta, drafts)
	if err != nil {
		t.Fatalf("Build(guide.md) error = %v", err)
	}
	r := retriever.New()
	r.Index([]*types.Document{doc}, sections, chunks)
	if hits := r.Search("身份证明", 5); len(hits) == 0 {
		t.Fatal("Search(身份证明) returned no hits, want review document to remain searchable")
	}
	answer := engine.New(r).Answer("申请人需要提交什么", nil)
	if answer.Status != "insufficient_evidence" || len(answer.Citations) != 0 {
		t.Errorf("Answer(申请人需要提交什么) = %+v, want insufficient_evidence with zero citations", answer)
	}
}
