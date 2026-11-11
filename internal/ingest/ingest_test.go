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

func TestParseFrontMatterWithUTF8BOM(t *testing.T) {
	meta, drafts, err := ingest.Parse([]byte("\uFEFF" + sampleDoc))
	if err != nil || meta.ID != "art-guide" || len(drafts) != 2 {
		t.Errorf("Parse(BOM + sampleDoc) = %+v, %d sections, %v; want art-guide, 2 sections, nil", meta, len(drafts), err)
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

func TestParseUnstructuredCreatesReviewIdentity(t *testing.T) {
	raw := []byte("\uFEFF# 社区活动指南\n\n## 申请条件\n申请人须提交申请表。\n\n2025年版说明。")
	meta, drafts, err := ingest.ParseUnstructured(raw, "guide.md")
	if err != nil {
		t.Fatalf("ParseUnstructured(%q) error = %v", "guide.md", err)
	}
	if meta.IdentityKind != "synthetic" || meta.Version != "unversioned" || meta.ID == "" {
		t.Fatalf("ParseUnstructured(%q) identity = %+v, want synthetic unversioned identity", "guide.md", meta)
	}
	if meta.Title != "guide" || meta.ContentHash == "" || meta.VersionHint != "2025年版" {
		t.Fatalf("ParseUnstructured(%q) metadata = %+v, want filename title, hash and version hint", "guide.md", meta)
	}
	if meta.EffectiveDate != "" || len(drafts) != 1 {
		t.Fatalf("ParseUnstructured(%q) effective date/sections = %q/%d, want empty/1", "guide.md", meta.EffectiveDate, len(drafts))
	}
}

func TestParseUnstructuredRejectsEmptyBody(t *testing.T) {
	if _, _, err := ingest.ParseUnstructured([]byte("# empty\n\n"), "empty.md"); err == nil {
		t.Error("ParseUnstructured(empty.md) error = nil, want empty body error")
	}
}

func TestParseUnstructuredPartialFrontMatter(t *testing.T) {
	raw := []byte("---\ntitle: 补充通知\nversion: v2\neffective_date: 2025-03-01\n---\n\n## 事项\n提交证明材料。")
	meta, drafts, err := ingest.ParseUnstructured(raw, "notice.md")
	if err != nil {
		t.Fatalf("ParseUnstructured(notice.md) error = %v", err)
	}
	if meta.Title != "补充通知" || meta.Version != "unversioned" || meta.EffectiveDate != "" || len(drafts) != 1 {
		t.Errorf("ParseUnstructured(notice.md) = %+v, %d sections, want title, unversioned, unknown date and 1 section", meta, len(drafts))
	}
	badDate := strings.Replace(string(raw), "2025-03-01", "2025-02-30", 1)
	if _, _, err := ingest.ParseUnstructured([]byte(badDate), "notice.md"); err == nil {
		t.Error("ParseUnstructured(notice.md with invalid date) error = nil, want error")
	}
	if _, _, err := ingest.ParseUnstructured([]byte("---\ntitle: broken"), "broken.md"); err == nil {
		t.Error("ParseUnstructured(broken.md with unclosed front matter) error = nil, want error")
	}
	if _, _, err := ingest.ParseUnstructured([]byte("---\ntitle 补充通知\n---\n## 事项\n提交材料。"), "broken.md"); err == nil {
		t.Error("ParseUnstructured(broken.md with malformed front matter line) error = nil, want error")
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
