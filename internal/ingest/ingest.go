// Package ingest 文档导入：front-matter 打标 → 结构解析 → 父子分块 → 双指纹幂等。
// 全确定性；解析失败返回 error，不静默成功。
package ingest

import (
	"fmt"
	"strings"
	"time"

	"ragbot/internal/simhash"
	"ragbot/internal/tokenize"
	"ragbot/internal/types"
)

// DocMeta front-matter 元数据
type DocMeta struct {
	ID            string   `json:"id"`
	Version       string   `json:"version"`
	Title         string   `json:"title"`
	PublishDate   string   `json:"publish_date"`
	EffectiveDate string   `json:"effective_date"`
	Scope         string   `json:"scope"`
	Nature        string   `json:"nature"`
	Replaces      []string `json:"replaces"`
}

// Result 单次导入结果
type Result struct {
	Action   string           `json:"action"` // created | duplicate | superseded | error
	Doc      *types.Document  `json:"doc,omitempty"`
	Sections int              `json:"sections"`
	Chunks   int              `json:"chunks"`
	Reason   string           `json:"reason,omitempty"`
}

// Parse 解析 Markdown 文档：front-matter + 标题层级 + 段落
func Parse(raw []byte) (*DocMeta, []SectionDraft, error) {
	text := string(raw)
	meta := &DocMeta{}

	// --- front-matter（--- 包裹的 key: value 块）---
	if !strings.HasPrefix(text, "---") {
		return nil, nil, fmt.Errorf("缺少 front-matter（文档必须以 --- 开头声明元数据）")
	}
	end := strings.Index(text[3:], "\n---")
	if end < 0 {
		return nil, nil, fmt.Errorf("front-matter 未闭合")
	}
	fm := text[3 : 3+end]
	body := text[3+end+4:]
	if i := strings.Index(body, "\n"); i >= 0 {
		body = body[i+1:]
	}

	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, ":") {
			continue
		}
		k, v, _ := strings.Cut(line, ":")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "id":
			meta.ID = v
		case "version":
			meta.Version = v
		case "title":
			meta.Title = v
		case "publish_date":
			meta.PublishDate = v
		case "effective_date":
			meta.EffectiveDate = v
		case "scope":
			meta.Scope = v
		case "nature":
			meta.Nature = v
		case "replaces":
			for _, r := range strings.Split(v, ",") {
				if r = strings.TrimSpace(r); r != "" {
					meta.Replaces = append(meta.Replaces, r)
				}
			}
		}
	}
	if meta.ID == "" || meta.Version == "" || meta.Title == "" {
		return nil, nil, fmt.Errorf("front-matter 缺少必要字段：id/version/title")
	}
	if meta.EffectiveDate == "" {
		return nil, nil, fmt.Errorf("front-matter 缺少 effective_date（版本裁决依赖业务生效时间）")
	}

	// --- 正文：## 标题切父块，段切子块 ---
	var sections []SectionDraft
	var cur *SectionDraft
	paraStart := 1
	appendPara := func(s *SectionDraft, p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		s.Paras = append(s.Paras, p)
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "## ") {
			if cur != nil && len(cur.Paras) > 0 {
				sections = append(sections, *cur)
			}
			cur = &SectionDraft{
				Path:     strings.TrimSpace(strings.TrimPrefix(line, "## ")),
				ParaIndex: paraStart,
			}
			paraStart++
			continue
		}
		if strings.HasPrefix(line, "# ") {
			continue // 一级标题=文档名，不参与正文
		}
		if cur == nil {
			// 标题前的引言归入"概述"节
			cur = &SectionDraft{Path: "概述", ParaIndex: paraStart}
			paraStart++
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		appendPara(cur, line)
	}
	if cur != nil && len(cur.Paras) > 0 {
		sections = append(sections, *cur)
	}
	if len(sections) == 0 {
		return nil, nil, fmt.Errorf("正文无有效内容")
	}
	return meta, sections, nil
}

type SectionDraft struct {
	Path      string
	ParaIndex int
	Paras     []string
}

// Build 把解析结果组装成 Document/Section/Chunk（含双指纹、块级去重）
func Build(meta *DocMeta, drafts []SectionDraft) (*types.Document, []*types.Section, []*types.Chunk, error) {
	doc := &types.Document{
		ID:     meta.ID,
		Version: meta.Version,
		Title:  meta.Title,
		Scope:  meta.Scope,
		Nature: meta.Nature,
		Replaces: meta.Replaces,
		Status: "active",
		ActivatedAt: time.Now(),
	}
	var err error
	if meta.PublishDate != "" {
		doc.PublishDate, err = time.Parse("2006-01-02", meta.PublishDate)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("publish_date 格式应为 2006-01-02: %w", err)
		}
	}
	doc.EffectiveDate, err = time.Parse("2006-01-02", meta.EffectiveDate)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("effective_date 格式应为 2006-01-02: %w", err)
	}

	// 文档级指纹：标题+全部正文拼接
	var all strings.Builder
	all.WriteString(meta.Title)
	for _, d := range drafts {
		for _, p := range d.Paras {
			all.WriteString(p)
		}
	}
	doc.Fingerprint = simhash.Text(all.String())

	// 父块与子块
	var sections []*types.Section
	var chunks []*types.Chunk
	seenChunk := map[uint64]bool{} // 块级近重复去重
	seq := 0
	for _, d := range drafts {
		sec := &types.Section{
			ID:          fmt.Sprintf("%s@%s#sec-%d", meta.ID, meta.Version, d.ParaIndex),
			DocID:       meta.ID,
			Version:     meta.Version,
			SectionPath: fmt.Sprintf("第%d节 %s", d.ParaIndex, d.Path),
			ParaIndex:   d.ParaIndex,
			Text:        strings.Join(d.Paras, "\n"),
		}
		sec.SimHash = simhash.Text(sec.Text)
		if len(d.Paras) > 0 {
			first := d.Paras[0]
			if len(first) > 60 {
				first = first[:60] + "…"
			}
			sec.Summary = sec.SectionPath + "：" + first
		}
		sections = append(sections, sec)

		// 子块：父块内按 ~300 字滑窗
		for _, c := range chunkParas(d.Paras, 300, 50) {
			toks := tokenize.Tokenize(c)
			fp := simhash.Fingerprint(toks)
			if seenChunk[fp] {
				continue // 近重复块跳索引
			}
			seenChunk[fp] = true
			seq++
			chunks = append(chunks, &types.Chunk{
				ID:        fmt.Sprintf("%s@%s#chunk-%d", meta.ID, meta.Version, seq),
				SectionID: sec.ID,
				Text:      c,
				Tokens:    toks,
				SimHash:   fp,
			})
		}
	}
	return doc, sections, chunks, nil
}

// chunkParas 段聚合滑窗切块：目标 target 字，重叠 overlap 字
func chunkParas(paras []string, target, overlap int) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		}
	}
	for _, p := range paras {
		// 超长单段硬切
		for len([]rune(p)) > target {
			r := []rune(p)
			out = append(out, string(r[:target]))
			p = string(r[target-overlap:])
		}
		if cur.Len() > 0 && cur.Len()+len([]rune(p)) > target {
			flush()
			// 带上前段尾部重叠
			prev := out[len(out)-1]
			pr := []rune(prev)
			if len(pr) > overlap {
				cur.WriteString(string(pr[len(pr)-overlap:]))
			}
		}
		if cur.Len() > 0 {
			cur.WriteString(" ")
		}
		cur.WriteString(p)
	}
	flush()
	return out
}
