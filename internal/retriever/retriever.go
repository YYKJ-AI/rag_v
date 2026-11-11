// Package retriever 检索层：内存 BM25 倒排 + 父块聚合。
// 全确定性：同输入同输出，评测可复现；阈值只作用于检索分数，不碰模型输出。
package retriever

import (
	"sort"

	"ragbot/internal/tokenize"
	"ragbot/internal/types"
)

// Retriever indexes imported chunks and returns their parent sections for citation.
type Retriever struct {
	docs     []*types.Document
	verified *Retriever // 正式问答独立索引，待审核资料不影响其 BM25 统计
	secs     map[string]*types.Section
	inverted map[string]map[int]int // term -> chunkIdx -> tf
	chunks   []*types.Chunk
	docLen   float64
}

func New() *Retriever {
	return &Retriever{secs: map[string]*types.Section{}, inverted: map[string]map[int]int{}}
}

// Index replaces the index from a complete store snapshot; repeated calls do not duplicate data.
func (r *Retriever) Index(docs []*types.Document, secs []*types.Section, chunks []*types.Chunk) {
	r.index(docs, secs, chunks)
	verifiedDocs := make([]*types.Document, 0, len(docs))
	for _, doc := range docs {
		if doc.Status != "needs_review" && doc.IdentityKind != "synthetic" {
			verifiedDocs = append(verifiedDocs, doc)
		}
	}
	r.verified = New()
	r.verified.index(verifiedDocs, secs, chunks)
}

func (r *Retriever) index(docs []*types.Document, secs []*types.Section, chunks []*types.Chunk) {
	r.docs = append([]*types.Document(nil), docs...)
	r.secs = make(map[string]*types.Section, len(secs))
	for _, section := range secs {
		copy := *section
		r.secs[copy.ID] = &copy
	}
	r.chunks = make([]*types.Chunk, 0, len(chunks))
	for _, chunk := range chunks {
		section := r.secs[chunk.SectionID]
		if section == nil || r.docOf(section) == nil || chunk.Text == "" {
			continue
		}
		copy := *chunk
		r.chunks = append(r.chunks, &copy)
	}
	r.rebuild()
}

// SearchVerified searches the certified-only index used for answers and evaluation.
func (r *Retriever) SearchVerified(query string, n int) []types.Hit {
	if r.verified == nil {
		return nil
	}
	return r.verified.Search(query, n)
}

func (r *Retriever) rebuild() {
	r.inverted = map[string]map[int]int{}
	titleOf := map[string]string{}
	for _, d := range r.docs {
		titleOf[d.ID+"@"+d.Version] = d.Title
	}
	for ci, chunk := range r.chunks {
		section := r.secs[chunk.SectionID]
		// Keep title and section path signals, but index only this real chunk's body.
		text := titleOf[section.DocID+"@"+section.Version] + " " + section.SectionPath + " " + chunk.Text
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
	type chunkScore struct {
		score float64
		chunk types.Chunk
	}
	best := map[string]chunkScore{} // 父块聚合：同父块留最高分
	for ci, sc := range scores {
		if sc <= 0 {
			continue
		}
		sid := r.chunks[ci].SectionID
		previous, exists := best[sid]
		if !exists || sc > previous.score || (sc == previous.score && r.chunks[ci].ID < previous.chunk.ID) {
			best[sid] = chunkScore{score: sc, chunk: *r.chunks[ci]}
		}
	}
	var hits []types.Hit
	for sid, selected := range best {
		s := r.secs[sid]
		doc := r.docOf(s)
		if doc == nil {
			continue
		}
		sc := selected.score
		if doc.Status == "needs_review" {
			sc *= 0.5
		}
		hits = append(hits, types.Hit{Section: *s, Chunk: selected.chunk, Score: sc})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Section.ID < hits[j].Section.ID
	})
	if n > 0 && len(hits) > n {
		hits = hits[:n]
	}
	return hits
}

// Docs 供引擎做版本裁决
func (r *Retriever) Docs() []*types.Document { return r.docs }

// VerifiedDocs returns documents eligible for formal answers and version checks.
func (r *Retriever) VerifiedDocs() []*types.Document {
	if r.verified == nil {
		return nil
	}
	return r.verified.docs
}

// Section returns the exact stored parent block for a parsed pointer target.
func (r *Retriever) Section(id string) (types.Section, bool) {
	section := r.secs[id]
	if section == nil {
		return types.Section{}, false
	}
	return *section, true
}

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
