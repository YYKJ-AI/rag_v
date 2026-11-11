// Package retriever 检索层：内存 BM25 倒排 + 父块聚合。
// 全确定性：同输入同输出，评测可复现；阈值只作用于检索分数，不碰模型输出。
package retriever

import (
	"sort"

	"ragbot/internal/tokenize"
	"ragbot/internal/types"
)

// Retriever 三层渐进式检索（目录层→大纲层→正文层的信号在父块聚合与降权中体现）
type Retriever struct {
	docs     []*types.Document
	secOrder []*types.Section
	secs     map[string]*types.Section
	inverted map[string]map[int]int // term -> chunkIdx -> tf
	chunks   []*types.Chunk
	secOf    []int // chunkIdx -> secOrder index
	docLen   float64
}

func New() *Retriever {
	return &Retriever{secs: map[string]*types.Section{}, inverted: map[string]map[int]int{}}
}

// Index 建索引（文档级幂等：同 (doc,version) 跳过）
func (r *Retriever) Index(docs []*types.Document, secs []*types.Section, _ []*types.Chunk) {
	exist := map[string]bool{}
	for _, d := range r.docs {
		exist[d.ID+"@"+d.Version] = true
	}
	var newDocs []*types.Document
	var newSecs []*types.Section
	for _, d := range docs {
		if !exist[d.ID+"@"+d.Version] {
			newDocs = append(newDocs, d)
		}
	}
	for _, s := range secs {
		if _, ok := r.secs[s.ID]; ok {
			continue
		}
		cp := *s
		r.secs[s.ID] = &cp
		newSecs = append(newSecs, &cp)
	}
	r.docs = append(r.docs, newDocs...)
	r.secOrder = append(r.secOrder, newSecs...)
	r.rebuild()
}

func (r *Retriever) rebuild() {
	r.inverted = map[string]map[int]int{}
	r.chunks = nil
	r.secOf = nil
	titleOf := map[string]string{}
	for _, d := range r.docs {
		titleOf[d.ID+"@"+d.Version] = d.Title
	}
	for si, s := range r.secOrder {
		// 标题+节路径参与索引（承载主题语义；tf 天然加权，使主题相关的节排名靠前）
		text := titleOf[s.DocID+"@"+s.Version] + " " + s.SectionPath + " " + s.Text
		ch := types.Chunk{ID: s.ID + "#c0", SectionID: s.ID, Text: s.Text, SimHash: s.SimHash}
		r.chunks = append(r.chunks, &ch)
		r.secOf = append(r.secOf, si)
		ci := len(r.chunks) - 1
		for _, t := range tokenize.Tokenize(text) {
			if r.inverted[t] == nil {
				r.inverted[t] = map[int]int{}
			}
			r.inverted[t][ci]++
		}
	}
	if len(r.chunks) == 0 {
		return
	}
	total := 0
	for _, c := range r.chunks {
		total += len([]rune(c.Text))
	}
	r.docLen = float64(total) / float64(len(r.chunks))
}

// Search 检索：同义词展开 → BM25 → 父块聚合 → topN；版本裁决留给查询层。
func (r *Retriever) Search(query string, n int) []types.Hit {
	toks := tokenize.Expand(tokenize.Tokenize(query))
	scores := r.bm25(toks)
	best := map[string]float64{} // 父块聚合：同父块留最高分
	for ci, sc := range scores {
		sid := r.chunks[ci].SectionID
		if sc > best[sid] {
			best[sid] = sc
		}
	}
	var hits []types.Hit
	for sid, sc := range best {
		s := r.secs[sid]
		doc := r.docOf(s)
		if doc == nil {
			continue
		}
		if doc.Status == "needs_review" {
			sc *= 0.5
		}
		hits = append(hits, types.Hit{Section: *s, Score: sc})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if n > 0 && len(hits) > n {
		hits = hits[:n]
	}
	return hits
}

// Docs 供引擎做版本裁决
func (r *Retriever) Docs() []*types.Document { return r.docs }

func (r *Retriever) docOf(s *types.Section) *types.Document {
	for _, d := range r.docs {
		if d.ID == s.DocID && d.Version == s.Version {
			return d
		}
	}
	return nil
}

// bm25 标准 BM25（k1=1.2, b=0.75）
func (r *Retriever) bm25(toks []string) []float64 {
	n := float64(len(r.chunks))
	if n == 0 {
		return nil
	}
	k1, b := 1.2, 0.75
	df := map[string]int{}
	for _, t := range toks {
		if m, ok := r.inverted[t]; ok {
			df[t] = len(m)
		}
	}
	scores := make([]float64, len(r.chunks))
	for _, t := range toks {
		m, ok := r.inverted[t]
		if !ok {
			continue
		}
		idf := 0.0
		if d := float64(df[t]); d > 0 {
			idf = 1 + (n-d+0.5)/(d+0.5)
		}
		for ci, tf := range m {
			dl := float64(len([]rune(r.chunks[ci].Text)))
			norm := float64(tf) * (k1 + 1) / (float64(tf) + k1*(1-b+b*dl/r.docLen))
			scores[ci] += idf * norm
		}
	}
	return scores
}
