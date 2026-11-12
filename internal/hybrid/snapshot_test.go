package hybrid

import (
	"testing"

	"ragbot/internal/types"
	"ragbot/internal/vectorstore"
)

func TestMatchesSnapshotRejectsStaleVectorAtSameChunkID(t *testing.T) {
	titles := map[string]string{"guide@v1": "Guide"}
	hit := types.Hit{
		Section: types.Section{DocID: "guide", Version: "v1", SectionPath: "Section 1"},
		Chunk:   types.Chunk{ID: "guide@v1#chunk-1", Text: "Current text"},
	}
	currentHash := vectorstore.Hash("Guide\nSection 1\nCurrent text")
	match := vectorstore.Match{ChunkID: hit.Chunk.ID, SourceHash: currentHash}
	if !matchesSnapshot(match, hit, titles) {
		t.Errorf("matchesSnapshot(current hash, current hit) = false, want true")
	}
	match.SourceHash = vectorstore.Hash("Guide\nSection 1\nOld text")
	if matchesSnapshot(match, hit, titles) {
		t.Errorf("matchesSnapshot(stale hash, current hit) = true, want false")
	}
	match.SourceHash = currentHash
	if matchesSnapshot(match, hit, map[string]string{}) {
		t.Errorf("matchesSnapshot(current hash, missing source title) = true, want false")
	}
}
