package engine_test

import (
	"testing"
	"time"

	"ragbot/internal/engine"
	"ragbot/internal/ingest"
	"ragbot/internal/retriever"
	"ragbot/internal/store"
)

// fixture：用测试语料建引擎（模拟 v1→v2 版本替代 + 未裁决冲突对）
func buildEngine(t *testing.T) (*engine.Engine, *store.Store) {
	t.Helper()
	raw1 := []byte(`---
id: guide
version: v1
title: 申请指南
effective_date: 2025-01-01
---

# 指南

## 申请条件
申请人须提交三项材料并加盖公章。申请须于活动开始前三十个工作日提交。

## 保证金
使用舞台区域须缴纳保证金两千元。
`)
	raw2 := []byte(`---
id: guide
version: v2
title: 申请指南修订版
effective_date: 2026-01-01
replaces: guide@v1
---

# 修订

## 申请条件（修订）
申请人须提交四项材料并加盖公章，新增公众责任保险投保证明。申请须于活动开始前二十五个工作日提交。
`)
	confA := []byte(`---
id: funding-cul
version: v1
title: 补助办法文化站稿
effective_date: 2026-04-01
---

# 补助

## 补助标准
单项补助金额上限为五千元。
`)
	confB := []byte(`---
id: funding-fin
version: v1
title: 补助办法财政所稿
effective_date: 2026-04-01
---

# 补助

## 补助标准
单项补助金额上限为八千元。
`)

	st := store.New(t.TempDir() + "/store.json")
	ret := retriever.New()
	for _, raw := range [][]byte{raw1, raw2, confA, confB} {
		meta, drafts, err := ingest.Parse(raw)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		doc, secs, chunks, err := ingest.Build(meta, drafts)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if _, err := st.Ingest(doc, secs, chunks); err != nil {
			t.Fatalf("ingest: %v", err)
		}
	}
	ret.Index(st.Docs, st.Sections, st.Chunks)
	return engine.New(ret), st
}

func TestVersionSupersede(t *testing.T) {
	_, st := buildEngine(t)
	v1Active := false
	for _, d := range st.Docs {
		if d.ID == "guide" && d.Version == "v1" {
			if d.Status != "superseded" || d.SupersededBy != "guide@v2" {
				t.Fatalf("v1 应被 v2 关窗替代: %+v", d)
			}
		}
		if d.ID == "guide" && d.Version == "v2" && d.Status == "active" {
			v1Active = true
		}
	}
	if !v1Active {
		t.Fatal("v2 应为 active")
	}
}

func TestAnswerCurrentVersion(t *testing.T) {
	e, _ := buildEngine(t)
	ans := e.Answer("现在申请要提交哪些材料", nil)
	if ans.Status != "answered" {
		t.Fatalf("应回答: %+v", ans)
	}
	if len(ans.Citations) == 0 {
		t.Fatal("回答必须带 citations")
	}
	for _, c := range ans.Citations {
		if c.DocumentID == "" || c.Version == "" || c.Location == "" || c.Excerpt == "" {
			t.Fatalf("citation 字段不完整: %+v", c)
		}
	}
}

func TestConflictDetectedNotSilentlyMerged(t *testing.T) {
	e, _ := buildEngine(t)
	ans := e.Answer("艺术节专项经费补助上限是多少", nil)
	if ans.Status != "conflict" {
		t.Fatalf("未裁决冲突应返回 conflict，实际 %s: %+v", ans.Status, ans)
	}
	if len(ans.Citations) < 2 {
		t.Fatal("冲突展示必须同时引用双方来源")
	}
}

func TestRefusalWhenNoEvidence(t *testing.T) {
	e, _ := buildEngine(t)
	ans := e.Answer("艺术节可以带宠物入场吗", nil)
	if ans.Status != "insufficient_evidence" {
		t.Fatalf("无证据应拒答，实际 %s", ans.Status)
	}
	if len(ans.Citations) != 0 {
		t.Fatal("拒答不得捏造出处")
	}
}

func TestAsOfTimePointQuery(t *testing.T) {
	e, _ := buildEngine(t)
	// 时间点在 v1 生效期、v2 未生效 → 应回答 v1 口径（三十个工作日）
	asOf := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	ans := e.Answer("申请要提前多少个工作日提交", &asOf)
	if ans.Status != "answered" {
		t.Fatalf("历史时间点应可回答: %+v", ans)
	}
	foundV1 := false
	for _, c := range ans.Citations {
		if c.Version == "v1" {
			foundV1 = true
		}
		if c.Version == "v2" {
			t.Fatal("as_of=2025-06-01 不应引用未生效的 v2")
		}
	}
	if !foundV1 {
		t.Fatalf("应引用 v1: %+v", ans.Citations)
	}
}

func TestIdempotentIngest(t *testing.T) {
	_, st := buildEngine(t)
	raw := []byte(`---
id: guide
version: v2
title: 申请指南修订版
effective_date: 2026-01-01
replaces: guide@v1
---

# 修订

## 申请条件（修订）
申请人须提交四项材料并加盖公章，新增公众责任保险投保证明。申请须于活动开始前二十五个工作日提交。
`)
	meta, drafts, err := ingest.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	doc, secs, chunks, err := ingest.Build(meta, drafts)
	if err != nil {
		t.Fatal(err)
	}
	action, err := st.Ingest(doc, secs, chunks)
	if err != nil {
		t.Fatal(err)
	}
	if action != "duplicate" {
		t.Fatalf("重复导入应幂等跳过，实际 %s", action)
	}
	if len(st.Docs) != 4 {
		t.Fatalf("幂等导入不应增加文档数，实际 %d", len(st.Docs))
	}
}

func TestDeterministicAnswers(t *testing.T) {
	e, _ := buildEngine(t)
	q := "申请要提前多少个工作日提交"
	a1 := e.Answer(q, nil)
	a2 := e.Answer(q, nil)
	if a1.Status != a2.Status || len(a1.Citations) != len(a2.Citations) {
		t.Fatal("同输入必须同输出（确定性）")
	}
	for i := range a1.Citations {
		if a1.Citations[i] != a2.Citations[i] {
			t.Fatal("citations 顺序与内容必须确定")
		}
	}
}
