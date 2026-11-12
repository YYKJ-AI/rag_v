package remoteembed_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ragbot/internal/modelconfig"
	"ragbot/internal/remoteembed"
)

func TestEmbedReordersIndexedResponseAndSendsConfiguredModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Errorf("Embed request path/auth = %q/%q, want /embeddings and configured token", r.URL.Path, r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Embed Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
		}
		var request struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("Decode(Embed request) error = %v, want nil", err)
		} else if request.Model != "test-model" || len(request.Input) != 2 || request.Input[0] != "first" || request.Input[1] != "second" {
			t.Errorf("Embed request = %+v, want test-model and ordered inputs", request)
		}
		fmt.Fprint(w, `{"data":[{"index":1,"embedding":[0,1]},{"index":0,"embedding":[1,0]}]}`)
	}))
	defer server.Close()
	client := remoteembed.New(modelconfig.Embedding{Endpoint: server.URL + "/embeddings", Model: "test-model", APIKey: "test-secret", Timeout: time.Second})
	got, err := client.Embed(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatalf("Embed(two inputs) error = %v, want nil", err)
	}
	if len(got) != 2 || got[0][0] != 1 || got[1][1] != 1 {
		t.Errorf("Embed(two inputs) = %v, want vectors reordered by index", got)
	}
}

func TestEmbedRejectsUnauthorizedResponseWithoutLeakingCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"test-secret"}`)
	}))
	defer server.Close()
	client := remoteembed.New(modelconfig.Embedding{Endpoint: server.URL + "/embeddings", Model: "test-model", APIKey: "test-secret", Timeout: time.Second})
	_, err := client.Embed(context.Background(), []string{"first"})
	if err == nil || strings.Contains(err.Error(), "test-secret") {
		t.Errorf("Embed(unauthorized) error = %v, want error without credentials", err)
	}
}

func TestEmbedRejectsMissingIndexedVector(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"index":0,"embedding":[1,0]},{"index":0,"embedding":[1,0]}]}`)
	}))
	defer server.Close()
	client := remoteembed.New(modelconfig.Embedding{Endpoint: server.URL + "/embeddings", Model: "test-model", APIKey: "test-secret", Timeout: time.Second})
	_, err := client.Embed(context.Background(), []string{"first", "second"})
	if err == nil {
		t.Errorf("Embed(duplicate response index) error = nil, want error")
	}
}
