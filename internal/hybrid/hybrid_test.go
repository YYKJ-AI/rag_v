package hybrid_test

import (
	"testing"

	"ragbot/internal/hybrid"
	"ragbot/internal/types"
)

func hit(section, chunk string) types.Hit {
	return types.Hit{Section: types.Section{ID: section}, Chunk: types.Chunk{ID: chunk}}
}

func TestFuseCombinesRanksDeduplicatesParentsAndKeepsLexicalChunk(t *testing.T) {
	lexical := []types.Hit{hit("a", "a-lex"), hit("b", "b-lex"), hit("a", "a-duplicate")}
	semantic := []types.Hit{hit("b", "b-vector"), hit("a", "a-vector"), hit("c", "c-vector")}
	got := hybrid.Fuse(lexical, semantic, 3)
	if len(got) != 3 {
		t.Fatalf("Fuse(%v, %v, 3) length = %d, want 3", lexical, semantic, len(got))
	}
	for i, want := range []struct{ section, chunk string }{
		{"a", "a-lex"}, {"b", "b-lex"}, {"c", "c-vector"},
	} {
		if got[i].Section.ID != want.section || got[i].Chunk.ID != want.chunk {
			t.Errorf("Fuse(...)[%d] = %s/%s, want %s/%s", i, got[i].Section.ID, got[i].Chunk.ID, want.section, want.chunk)
		}
	}
	if got[0].Score != got[1].Score || got[1].Score <= got[2].Score {
		t.Errorf("Fuse(...) scores = %.6f, %.6f, %.6f, want first two tied above third", got[0].Score, got[1].Score, got[2].Score)
	}
}

func TestFuseLimitsResultsWithoutMutatingInputs(t *testing.T) {
	lexical := []types.Hit{hit("a", "a"), hit("b", "b")}
	semantic := []types.Hit{hit("c", "c")}
	got := hybrid.Fuse(lexical, semantic, 2)
	if len(got) != 2 || lexical[0].Score != 0 || semantic[0].Score != 0 {
		t.Errorf("Fuse(..., 2) = %+v, input scores = %.3f/%.3f, want 2 results and unchanged inputs", got, lexical[0].Score, semantic[0].Score)
	}
}
