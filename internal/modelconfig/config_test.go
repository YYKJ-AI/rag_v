package modelconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadVectorAcceptsBaseEndpointAndKeepsSecretsOutOfErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	data := strings.Join([]string{
		"RAGBOT_EMBEDDING_ENABLED=true",
		"RAGBOT_EMBEDDING_PROVIDER=remote_http",
		"RAGBOT_EMBEDDING_ENDPOINT_URL=https://example.invalid/compatible-mode/v1",
		"RAGBOT_EMBEDDING_MODEL=test-model",
		"RAGBOT_EMBEDDING_API_KEY=test-secret",
		"RAGBOT_PG_DSN=postgres://example:test-password@localhost:5432/ragbot",
	}, "\n")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
	}
	got, err := LoadVector(path)
	if err != nil {
		t.Fatalf("LoadVector(%q) error = %v, want nil", path, err)
	}
	if got.Embedding.Endpoint != "https://example.invalid/compatible-mode/v1/embeddings" {
		t.Errorf("LoadVector(%q).Endpoint = %q, want /embeddings suffix", path, got.Embedding.Endpoint)
	}
	t.Setenv("RAGBOT_EMBEDDING_ENDPOINT_URL", "https://example.invalid/private?token=test-secret")
	_, err = LoadVector(path)
	if err == nil || strings.Contains(err.Error(), "test-secret") {
		t.Errorf("LoadVector(invalid URL) error = %v, want sanitized error", err)
	}
}

func TestLoadVectorRequiresExplicitEnablement(t *testing.T) {
	t.Setenv("RAGBOT_EMBEDDING_ENABLED", "false")
	_, err := LoadVector(filepath.Join(t.TempDir(), "missing.env"))
	if err == nil {
		t.Errorf("LoadVector(disabled embedding) error = nil, want error")
	}
}
