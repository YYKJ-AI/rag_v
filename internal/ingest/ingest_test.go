package ingest_test

import (
	"strings"
	"testing"

	"ragbot/internal/ingest"
	"ragbot/internal/simhash"
)

func parseOK(t *testing.T, raw string) (*ingest.DocMeta, []ingest.SectionDraft) {
	t.Helper()
	meta, drafts, err := ingest.Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	return meta, drafts
}

const sampleDoc = `---
id: art-guide
version: v1
title: 社区艺术节活动申请指南
publish_date: 2025-03-01
effective_date: 2025-03-15
scope: 全体居民
nature: 基础规范
---

# 指南

## 申请条件
申请人须为年满十八周岁的社区居民。申请方须提交活动方案及安全预案。所有材料须加盖公章。

## 申请时间
申请须于活动开始前三十个工作日提交。
`

func TestParseFrontMatter(t *testing.T) {
	meta, drafts := parseOK(t, sampleDoc)
	if meta.ID != "art-guide" || meta.Version != "v1" || meta.Title == "" {
		t.Fatalf("元数据解析不完整: %+v", meta)
	}
	if len(drafts) != 2 {
		t.Fatalf("应解析出 2 个节，实际 %d", len(drafts))
	}
	if !strings.Contains(drafts[0].Paras[0], "十八周岁") {
		t.Fatalf("正文内容缺失")
	}
}

func TestParseRejectsMissingFields(t *testing.T) {
	_, _, err := ingest.Parse([]byte("没有front-matter的文档"))
	if err == nil {
		t.Fatal("无 front-matter 应报错")
	}
	noDate := strings.Replace(sampleDoc, "effective_date: 2025-03-15\n", "", 1)
	_, _, err = ingest.Parse([]byte(noDate))
	if err == nil {
		t.Fatal("缺 effective_date 应报错（版本裁决依赖）")
	}
}

func TestImportRejectsMalformedMetadataAndEmptyBody(t *testing.T) {
	for _, tt := range []struct {
		name, raw string
	}{
		{name: "unclosed front matter", raw: "---\nid: broken\nversion: v1\n"},
		{name: "missing id", raw: strings.Replace(sampleDoc, "id: art-guide\n", "", 1)},
		{name: "empty body", raw: "---\nid: empty\nversion: v1\ntitle: 空文档\neffective_date: 2025-01-01\n---\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := ingest.Parse([]byte(tt.raw)); err == nil {
				t.Errorf("Parse(%s) error = nil, want error", tt.name)
			}
		})
	}
	for _, date := range []string{"2025-02-30", "not-a-date"} {
		t.Run(date, func(t *testing.T) {
			raw := strings.Replace(sampleDoc, "publish_date: 2025-03-01", "publish_date: "+date, 1)
			meta, drafts := parseOK(t, raw)
			if _, _, _, err := ingest.Build(meta, drafts); err == nil {
				t.Errorf("Build(publish_date=%q) error = nil, want error", date)
			}
		})
	}
}

func TestBuildRejectsInvalidValidityWindow(t *testing.T) {
	for _, tt := range []struct {
		name, until string
	}{
		{"bad date", "2025-02-30"},
		{"before effective", "2025-03-14"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw := strings.Replace(sampleDoc, "effective_date: 2025-03-15", "effective_date: 2025-03-15\nvalid_until: "+tt.until, 1)
			meta, drafts := parseOK(t, raw)
			if _, _, _, err := ingest.Build(meta, drafts); err == nil {
				t.Errorf("Build(valid_until=%s) error = nil, want error", tt.until)
			}
		})
	}
}

func TestBuildSimHashAndChunks(t *testing.T) {
	meta, drafts := parseOK(t, sampleDoc)
	doc, secs, chunks, err := ingest.Build(meta, drafts)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	if doc.Fingerprint == 0 {
		t.Fatal("文档级指纹未生成")
	}
	if len(secs) != 2 || len(chunks) == 0 {
		t.Fatalf("父子分块数量异常: secs=%d chunks=%d", len(secs), len(chunks))
	}
	// 子块 simhash 与文档指纹都应非零
	for _, c := range chunks {
		if c.SimHash == 0 {
			t.Fatal("子块指纹未生成")
		}
	}
}

func TestFingerprintIdempotent(t *testing.T) {
	a := simhash.Text("申请人须为年满十八周岁的社区居民，提交活动方案及安全预案。")
	b := simhash.Text("申请人须为年满十八周岁的社区居民，提交活动方案及安全预案。")
	if a != b {
		t.Fatal("同文本指纹应相同（幂等导入基础）")
	}
	// 微小改动 → 指纹接近（汉明距离小）→ 识别为版本演进而非完全重复
	c := simhash.Text("申请人须为年满十八周岁的社区居民，提交活动方案及安全预案和保险证明。")
	if simhash.Hamming(a, c) > 10 {
		t.Fatalf("近似文本汉明距离应较小，实际 %d", simhash.Hamming(a, c))
	}
	if simhash.Hamming(a, b) != 0 {
		t.Fatal("相同文本距离应为 0")
	}
}
