package store_test

import (
	"path/filepath"
	"testing"

	"ragbot/internal/ingest"
	"ragbot/internal/store"
	"ragbot/internal/types"
)

func reviewDocument(t *testing.T, body, name string) (*types.Document, []*types.Section, []*types.Chunk) {
	t.Helper()
	meta, drafts, err := ingest.ParseUnstructured([]byte(body), name)
	if err != nil {
		t.Fatalf("ParseUnstructured(%q) error = %v", name, err)
	}
	doc, secs, chunks, err := ingest.Build(meta, drafts)
	if err != nil {
		t.Fatalf("Build(%q) error = %v", name, err)
	}
	return doc, secs, chunks
}

func TestUnstructuredDeduplicationAndNameConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	st := store.New(path)
	first, secs, chunks := reviewDocument(t, "## 条件\n申请人须提供证明。", "guide.md")
	if action, err := st.Ingest(first, secs, chunks); err != nil || action != "created" {
		t.Fatalf("Ingest(first) = %q, %v, want created", action, err)
	}
	if first.Status != "needs_review" || !first.EffectiveDate.IsZero() {
		t.Errorf("Ingest(first) status/date = %q/%v, want needs_review/zero", first.Status, first.EffectiveDate)
	}
	alias, secs, chunks := reviewDocument(t, "## 条件\n申请人须提供证明。", "copy.md")
	if action, err := st.Ingest(alias, secs, chunks); err != nil || action != "duplicate" {
		t.Errorf("Ingest(alias) = %q, %v, want duplicate", action, err)
	}
	if len(st.Docs) != 1 || len(st.Docs[0].Aliases) != 1 || st.Docs[0].Aliases[0] != "copy.md" {
		t.Fatalf("Ingest(alias) docs/aliases = %d/%v, want 1/[copy.md]", len(st.Docs), st.Docs[0].Aliases)
	}
	changed, secs, chunks := reviewDocument(t, "## 条件\n申请人须提供新的证明。", "guide.md")
	if action, err := st.Ingest(changed, secs, chunks); err != nil || action != "created" {
		t.Fatalf("Ingest(changed) = %q, %v, want independent created document", action, err)
	}
	if len(st.Docs) != 2 || st.Docs[0].ID == st.Docs[1].ID || len(changed.MetadataWarnings) == 0 || changed.MetadataWarnings[len(changed.MetadataWarnings)-1] != "possible_duplicate_name" {
		t.Errorf("Ingest(changed) docs/IDs/warnings = %d/%q,%q/%v, want two IDs and name warning", len(st.Docs), first.ID, changed.ID, changed.MetadataWarnings)
	}
	if err := st.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	reloaded := store.New(path)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(reloaded.Docs) != 2 || len(reloaded.Docs[0].Aliases) != 1 {
		t.Errorf("Load() docs/aliases = %d/%v, want 2/[copy.md]", len(reloaded.Docs), reloaded.Docs[0].Aliases)
	}
}
