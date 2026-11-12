// Package hybrid combines existing BM25 parent hits with pgvector chunk hits.
package hybrid

import (
	"context"
	"fmt"
	"sort"

	"ragbot/internal/remoteembed"
	"ragbot/internal/retriever"
	"ragbot/internal/types"
	"ragbot/internal/vectorstore"
)

const rrfK = 60

type Service struct {
	Embeddings *remoteembed.Client
	Vectors    *vectorstore.Store
	Retriever  *retriever.Retriever
}

type SyncStats struct {
	Eligible int
	Embedded int
	Skipped  int
	Pruned   int
	Rows     int
}

type pending struct {
	ID   string
	Hash string
	Text string
}

// Sync embeds only certified chunks that changed in the current JSON snapshot.
// Each batch is validated before its rows are upserted. A rerun with identical
// input makes zero model calls and keeps the same row count.
func (s *Service) Sync(ctx context.Context, docs []*types.Document, secs []*types.Section, chunks []*types.Chunk) (SyncStats, error) {
	var stats SyncStats
	titles := make(map[string]string, len(docs))
	for _, doc := range docs {
		if doc.Status != "needs_review" && doc.IdentityKind != "synthetic" {
			titles[doc.ID+"@"+doc.Version] = doc.Title
		}
	}
	sections := make(map[string]*types.Section, len(secs))
	for _, sec := range secs {
		sections[sec.ID] = sec
	}
	var work []pending
	currentIDs := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		sec := sections[chunk.SectionID]
		if sec == nil || chunk.Text == "" {
			continue
		}
		title, ok := titles[sec.DocID+"@"+sec.Version]
		if !ok {
			continue
		}
		stats.Eligible++
		currentIDs = append(currentIDs, chunk.ID)
		input := embeddingInput(title, sec.SectionPath, chunk.Text)
		hash := vectorstore.Hash(input)
		exists, err := s.Vectors.Has(ctx, chunk.ID, hash)
		if err != nil {
			return stats, err
		}
		if exists {
			stats.Skipped++
			continue
		}
		work = append(work, pending{ID: chunk.ID, Hash: hash, Text: input})
	}
	for start := 0; start < len(work); start += 16 {
		end := start + 16
		if end > len(work) {
			end = len(work)
		}
		inputs := make([]string, 0, end-start)
		for _, row := range work[start:end] {
			inputs = append(inputs, row.Text)
		}
		vectors, err := s.Embeddings.Embed(ctx, inputs)
		if err != nil {
			return stats, err
		}
		for i, vector := range vectors {
			row := work[start+i]
			if err := s.Vectors.Upsert(ctx, row.ID, row.Hash, vector); err != nil {
				return stats, err
			}
			stats.Embedded++
		}
	}
	var err error
	stats.Pruned, err = s.Vectors.DeleteAbsent(ctx, currentIDs)
	if err != nil {
		return stats, err
	}
	stats.Rows, err = s.Vectors.Count(ctx)
	return stats, err
}

// Search keeps three independent ranked lists. RRF is a rank-fusion score,
// not a calibrated probability or a sufficient-evidence decision.
func (s *Service) Search(ctx context.Context, query string, pool, limit int) (lexical, semantic, fused []types.Hit, err error) {
	if query == "" || pool < 1 || limit < 1 {
		return nil, nil, nil, fmt.Errorf("查询、候选池与结果数必须有效")
	}
	lexical = s.Retriever.SearchVerified(query, pool)
	vectors, err := s.Embeddings.Embed(ctx, []string{query})
	if err != nil {
		return lexical, nil, nil, err
	}
	matches, err := s.Vectors.Search(ctx, vectors[0], min(pool*5, 1000))
	if err != nil {
		return lexical, nil, nil, err
	}
	seen := map[string]bool{}
	titles := make(map[string]string)
	for _, doc := range s.Retriever.VerifiedDocs() {
		titles[doc.ID+"@"+doc.Version] = doc.Title
	}
	for _, match := range matches {
		hit, ok := s.Retriever.VerifiedChunkHit(match.ChunkID)
		if !ok || seen[hit.Section.ID] || !matchesSnapshot(match, hit, titles) {
			continue
		}
		seen[hit.Section.ID] = true
		hit.Score = match.Distance // only for display; Fuse replaces it with the RRF score.
		semantic = append(semantic, hit)
		if len(semantic) >= pool {
			break
		}
	}
	fused = Fuse(lexical, semantic, limit)
	return lexical, semantic, fused, nil
}

// matchesSnapshot rejects a vector written for an older body at the same chunk ID.
func matchesSnapshot(match vectorstore.Match, hit types.Hit, titles map[string]string) bool {
	title, ok := titles[hit.Section.DocID+"@"+hit.Section.Version]
	if !ok {
		return false
	}
	input := embeddingInput(title, hit.Section.SectionPath, hit.Chunk.Text)
	return match.SourceHash == vectorstore.Hash(input)
}

func embeddingInput(title, sectionPath, chunkText string) string {
	return title + "\n" + sectionPath + "\n" + chunkText
}

// Fuse deduplicates by parent section, then sums reciprocal ranks with k=60.
// The retained chunk is the best BM25 chunk when both lists contain a parent.
func Fuse(lexical, semantic []types.Hit, limit int) []types.Hit {
	type entry struct {
		hit   types.Hit
		score float64
	}
	bySection := map[string]*entry{}
	for _, list := range [][]types.Hit{lexical, semantic} {
		seen := map[string]bool{}
		rank := 0
		for _, hit := range list {
			id := hit.Section.ID
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			rank++
			item := bySection[id]
			if item == nil {
				item = &entry{hit: hit}
				bySection[id] = item
			}
			item.score += 1.0 / float64(rrfK+rank)
		}
	}
	result := make([]types.Hit, 0, len(bySection))
	for _, item := range bySection {
		item.hit.Score = item.score
		result = append(result, item.hit)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		return result[i].Section.ID < result[j].Section.ID
	})
	if limit > 0 && len(result) > limit {
		return result[:limit]
	}
	return result
}
