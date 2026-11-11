package retriever_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"ragbot/internal/ingest"
	"ragbot/internal/retriever"
	"ragbot/internal/types"
)

func TestSearchUsesIngestedChunksForLongProcessAndTable(t *testing.T) {
	longIntro := strings.Repeat("普通事项由办公室登记并存档。", 90)
	raw := fmt.Sprintf(`---
id: handbook
version: v1
title: 项目操作手册
effective_date: 2025-01-01
---
# 项目操作手册
## 售后条款
%s
质保期为十八个月。
## 审批流程
第一步登记申请，第二步核验材料。
最终由财务主管复核签字。
## 设备价格表
| 型号 | 单价 |
| B7型设备 | 七百八十九元 |
`, longIntro)
	meta, drafts, err := ingest.Parse([]byte(raw))
	if err != nil {
		t.Fatalf("ingest.Parse(long document) error = %v, want nil", err)
	}
	doc, sections, chunks, err := ingest.Build(meta, drafts)
	if err != nil {
		t.Fatalf("ingest.Build(long document) error = %v, want nil", err)
	}
	if len(chunks) <= len(sections) {
		t.Fatalf("ingest.Build(long document) chunks = %d, want more than %d sections", len(chunks), len(sections))
	}
	r := retriever.New()
	r.Index([]*types.Document{doc}, sections, chunks)
	for _, tt := range []struct {
		query, wantText, wantPath string
	}{
		{"质保期十八个月", "质保期为十八个月", "售后条款"},
		{"财务主管复核签字", "财务主管复核签字", "审批流程"},
		{"B7型设备七百八十九元", "B7型设备", "设备价格表"},
	} {
		t.Run(tt.wantPath, func(t *testing.T) {
			hits := r.Search(tt.query, 5)
			if len(hits) == 0 {
				t.Fatalf("Search(%q) = no hits, want %s", tt.query, tt.wantPath)
			}
			h := hits[0]
			if !strings.Contains(h.Section.SectionPath, tt.wantPath) || !strings.Contains(h.Chunk.Text, tt.wantText) || !strings.HasPrefix(h.Chunk.ID, "handbook@v1#chunk-") {
				t.Errorf("Search(%q)[0] = section=%q chunk=%q id=%q, want path %q and real chunk containing %q", tt.query, h.Section.SectionPath, h.Chunk.Text, h.Chunk.ID, tt.wantPath, tt.wantText)
			}
		})
	}
}

func TestSearchAggregatesBestChunkAndReindexIsIdempotent(t *testing.T) {
	doc := &types.Document{ID: "manual", Version: "v1", Title: "申请规则", EffectiveDate: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}
	section := &types.Section{ID: "manual@v1#sec-1", DocID: "manual", Version: "v1", SectionPath: "申请方式", Text: "登记窗口。申请渠道为云台。隐藏词海蓝晶。"}
	chunks := []*types.Chunk{
		{ID: "manual@v1#chunk-1", SectionID: section.ID, Text: "登记窗口。"},
		{ID: "manual@v1#chunk-2", SectionID: section.ID, Text: "申请渠道为云台。"},
	}
	r := retriever.New()
	for i := 0; i < 2; i++ {
		r.Index([]*types.Document{doc}, []*types.Section{section}, chunks)
		hits := r.Search("申请渠道云台", 5)
		if len(hits) != 1 || hits[0].Chunk.ID != chunks[1].ID {
			t.Errorf("Search(after Index #%d) = %+v, want one parent hit via %s", i+1, hits, chunks[1].ID)
		}
		if got := r.Search("海蓝晶", 5); len(got) != 0 {
			t.Errorf("Search(parent-only term after Index #%d) = %+v, want no chunk hit", i+1, got)
		}
	}
}

func TestSearchTieOrderIsStable(t *testing.T) {
	date := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	docs := []*types.Document{
		{ID: "b", Version: "v1", Title: "事项", EffectiveDate: date},
		{ID: "a", Version: "v1", Title: "事项", EffectiveDate: date},
	}
	sections := []*types.Section{
		{ID: "b@v1#sec-1", DocID: "b", Version: "v1", SectionPath: "登记", Text: "登记申请。"},
		{ID: "a@v1#sec-1", DocID: "a", Version: "v1", SectionPath: "登记", Text: "登记申请。"},
	}
	chunks := []*types.Chunk{
		{ID: "b@v1#chunk-1", SectionID: sections[0].ID, Text: "登记申请。"},
		{ID: "a@v1#chunk-1", SectionID: sections[1].ID, Text: "登记申请。"},
	}
	r := retriever.New()
	r.Index(docs, sections, chunks)
	hits := r.Search("登记申请", 2)
	if len(hits) != 2 || hits[0].Section.ID != sections[1].ID || hits[1].Section.ID != sections[0].ID {
		t.Errorf("Search(tied scores) = %+v, want a then b by section ID", hits)
	}
}
