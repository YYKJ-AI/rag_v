package ingest_test

import (
	"fmt"
	"testing"

	"ragbot/internal/ingest"
	"ragbot/internal/store"
	"ragbot/internal/types"
)

func TestBuildPointerRelationAndPersist(t *testing.T) {
	raw := `---
id: guide
version: v2
title: 社区申请指南
effective_date: 2021-01-01
---
# 社区申请指南
## 申请人条件
申请人条件沿用 guide@v1#sec-1，不作调整。
`
	meta, drafts, err := ingest.Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse(pointer document) error = %v, want nil", err)
	}
	doc, sections, chunks, err := ingest.Build(meta, drafts)
	if err != nil {
		t.Fatalf("Build(pointer document) error = %v, want nil", err)
	}
	if len(chunks) != 1 || chunks[0].Kind != "pointer" || len(chunks[0].Relations) != 1 {
		t.Fatalf("Build(pointer document).Chunks = %+v, want one pointer chunk with one relation", chunks)
	}
	relation := chunks[0].Relations[0]
	if relation.Type != types.RelationInherits || relation.Status != "parsed" || relation.TargetSectionID != "guide@v1#sec-1" || relation.SourceChunkID != chunks[0].ID {
		t.Errorf("Build(pointer document).Relation = %+v, want parsed inherits edge to guide@v1#sec-1", relation)
	}
	path := t.TempDir() + "/store.json"
	st := store.New(path)
	if _, err := st.Ingest(doc, sections, chunks); err != nil {
		t.Fatalf("Store.Ingest(pointer document) error = %v, want nil", err)
	}
	if err := st.Save(); err != nil {
		t.Fatalf("Store.Save(pointer document) error = %v, want nil", err)
	}
	loaded := store.New(path)
	if err := loaded.Load(); err != nil {
		t.Fatalf("Store.Load(pointer document) error = %v, want nil", err)
	}
	if len(loaded.Chunks) != 1 || len(loaded.Chunks[0].Relations) != 1 || loaded.Chunks[0].Relations[0] != relation {
		t.Errorf("Store.Load(%q).Chunks = %+v, want persisted relation %+v", path, loaded.Chunks, relation)
	}
}

func TestBuildUnresolvedPointerAndExplicitReference(t *testing.T) {
	cases := []struct {
		body       string
		wantType   types.RelationType
		wantStatus string
	}{
		{"申请人条件沿用原规定。", types.RelationInherits, "unresolved"},
		{"申请人条件参照 guide@v1#sec-1。", types.RelationReferences, "parsed"},
		{"申请人条件沿用 guide@v1#sec-1 或 guide@v1#sec-2。", types.RelationInherits, "unresolved"},
	}
	for _, tc := range cases {
		raw := fmt.Sprintf("---\nid: guide\nversion: v2\ntitle: 社区申请指南\neffective_date: 2021-01-01\n---\n# 社区申请指南\n## 申请人条件\n%s\n", tc.body)
		meta, drafts, err := ingest.Parse([]byte(raw))
		if err != nil {
			t.Fatalf("Parse(%q) error = %v, want nil", tc.body, err)
		}
		_, _, chunks, err := ingest.Build(meta, drafts)
		if err != nil {
			t.Fatalf("Build(%q) error = %v, want nil", tc.body, err)
		}
		if len(chunks) != 1 || len(chunks[0].Relations) != 1 {
			t.Fatalf("Build(%q).Chunks = %+v, want one relation", tc.body, chunks)
		}
		relation := chunks[0].Relations[0]
		if relation.Type != tc.wantType || relation.Status != tc.wantStatus {
			t.Errorf("Build(%q).Relation = %+v, want type=%s status=%s", tc.body, relation, tc.wantType, tc.wantStatus)
		}
	}
}
