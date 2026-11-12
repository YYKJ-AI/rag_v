package vectorstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"
)

// TestStoreIntegration runs only when explicitly given a disposable database
// connection. It creates and cleans up its own collection.
func TestStoreIntegration(t *testing.T) {
	dsn := os.Getenv("RAGBOT_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("RAGBOT_TEST_PG_DSN 未设置")
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("rand.Read(collection suffix) error = %v, want nil", err)
	}
	collection := "ragbot-test-" + hex.EncodeToString(suffix[:])
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, err := Open(ctx, dsn, collection, "test-embedding-model")
	if err != nil {
		t.Fatalf("Open(test collection) error = %v, want nil", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := s.conn.Exec(cleanupCtx, `DELETE FROM ragbot_chunk_vectors WHERE collection=$1`, collection); err != nil {
			t.Errorf("cleanup vectors(%q) error = %v, want nil", collection, err)
		}
		if _, err := s.conn.Exec(cleanupCtx, `DELETE FROM ragbot_vector_collections WHERE collection=$1`, collection); err != nil {
			t.Errorf("cleanup collection(%q) error = %v, want nil", collection, err)
		}
		if err := s.Close(cleanupCtx); err != nil {
			t.Errorf("Close(test collection) error = %v, want nil", err)
		}
	})
	if err := s.Upsert(ctx, "chunk-a", "hash-a", []float32{1, 0}); err != nil {
		t.Fatalf("Upsert(chunk-a) error = %v, want nil", err)
	}
	if err := s.Upsert(ctx, "chunk-b", "hash-b", []float32{0, 1}); err != nil {
		t.Fatalf("Upsert(chunk-b) error = %v, want nil", err)
	}
	if err := s.Upsert(ctx, "chunk-a", "hash-a", []float32{1, 0}); err != nil {
		t.Errorf("Upsert(duplicate chunk-a) error = %v, want nil", err)
	}
	if count, err := s.Count(ctx); err != nil || count != 2 {
		t.Errorf("Count(after repeated Upsert) = %d, %v, want 2, nil", count, err)
	}
	if found, err := s.Has(ctx, "chunk-a", "hash-a"); err != nil || !found {
		t.Errorf("Has(chunk-a, hash-a) = %t, %v, want true, nil", found, err)
	}
	if found, err := s.Has(ctx, "chunk-a", "wrong-hash"); err != nil || found {
		t.Errorf("Has(chunk-a, wrong-hash) = %t, %v, want false, nil", found, err)
	}
	matches, err := s.Search(ctx, []float32{1, 0}, 2)
	if err != nil || len(matches) != 2 || matches[0].ChunkID != "chunk-a" || matches[0].SourceHash != "hash-a" {
		t.Errorf("Search([1,0], 2) = %v, %v, want chunk-a with hash-a first", matches, err)
	}
	if err := s.Upsert(ctx, "bad-dimension", "hash", []float32{1, 0, 0}); err == nil {
		t.Errorf("Upsert(3 dimensions) error = nil, want mismatch error")
	}
	if removed, err := s.DeleteAbsent(ctx, []string{"chunk-a"}); err != nil || removed != 1 {
		t.Errorf("DeleteAbsent([chunk-a]) = %d, %v, want 1, nil", removed, err)
	}
	if count, err := s.Count(ctx); err != nil || count != 1 {
		t.Errorf("Count(after prune) = %d, %v, want 1, nil", count, err)
	}
	if _, err := Open(ctx, dsn, collection, "other-model"); err == nil {
		t.Errorf("Open(existing collection, other-model) error = nil, want mismatch error")
	}
}
