package rag

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDashScopeEmbedder_BatchEncode(t *testing.T) {
	wantTexts := []string{"hello", "world"}
	wantVecs := [][]float32{{0.1, 0.2, 0.3}, {0.4, 0.5, 0.6}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("want POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/embeddings") {
			t.Errorf("want path /embeddings, got %s", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Errorf("want Bearer test-key, got %s", auth)
		}

		var req embedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "text-embedding-v4" {
			t.Errorf("want model text-embedding-v4, got %s", req.Model)
		}
		if len(req.Input.Texts) != len(wantTexts) {
			t.Errorf("want %d texts, got %d", len(wantTexts), len(req.Input.Texts))
		}

		resp := embedResponse{
			Data: []embedData{
				{Embedding: wantVecs[0]},
				{Embedding: wantVecs[1]},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	os.Setenv("EMBED_BASE_URL", srv.URL)
	os.Setenv("EMBED_MODEL", "text-embedding-v4")
	os.Setenv("EMBED_API_KEY", "test-key")
	defer os.Unsetenv("EMBED_BASE_URL")
	defer os.Unsetenv("EMBED_MODEL")
	defer os.Unsetenv("EMBED_API_KEY")

	emb, err := newDashScopeEmbedder()
	if err != nil {
		t.Fatalf("newDashScopeEmbedder: %v", err)
	}
	// swap HTTP client to talk to the test server
	emb.http = srv.Client()

	got, err := emb.Encode(wantTexts)
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	if len(got) != len(wantVecs) {
		t.Fatalf("want %d vectors, got %d", len(wantVecs), len(got))
	}
	for i := range wantVecs {
		if len(got[i]) != len(wantVecs[i]) {
			t.Fatalf("vector %d: want len %d, got %d", i, len(wantVecs[i]), len(got[i]))
		}
		for j := range wantVecs[i] {
			if got[i][j] != wantVecs[i][j] {
				t.Errorf("vector %d[%d]: want %v, got %v", i, j, wantVecs[i][j], got[i][j])
			}
		}
	}
}

func TestDashScopeEmbedder_CacheHit(t *testing.T) {
	callCount := atomic.Int32{}
	wantVec := []float32{0.7, 0.8, 0.9}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		resp := embedResponse{
			Data: []embedData{{Embedding: wantVec}},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	os.Setenv("EMBED_BASE_URL", srv.URL)
	os.Setenv("EMBED_MODEL", "text-embedding-v4")
	os.Setenv("EMBED_API_KEY", "test-key")
	defer os.Unsetenv("EMBED_BASE_URL")
	defer os.Unsetenv("EMBED_MODEL")
	defer os.Unsetenv("EMBED_API_KEY")

	emb, err := newDashScopeEmbedder()
	if err != nil {
		t.Fatalf("newDashScopeEmbedder: %v", err)
	}
	emb.http = srv.Client()

	// First call should hit the server.
	if _, err := emb.Encode([]string{"cache-me"}); err != nil {
		t.Fatalf("first Encode error: %v", err)
	}
	if callCount.Load() != 1 {
		t.Fatalf("want 1 server call after first Encode, got %d", callCount.Load())
	}

	// Second call with the same text should be served from cache.
	got, err := emb.Encode([]string{"cache-me"})
	if err != nil {
		t.Fatalf("second Encode error: %v", err)
	}
	if callCount.Load() != 1 {
		t.Fatalf("want 1 server call after cache hit, got %d", callCount.Load())
	}
	if len(got) != 1 || len(got[0]) != len(wantVec) {
		t.Fatalf("unexpected result shape")
	}
	for i := range wantVec {
		if got[0][i] != wantVec[i] {
			t.Errorf("cached vector[%d]: want %v, got %v", i, wantVec[i], got[0][i])
		}
	}
}

func TestDashScopeEmbedder_RetryOnRateLimit(t *testing.T) {
	failures := atomic.Int32{}
	wantVec := []float32{0.1, 0.2}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failures.Add(1) <= 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		resp := embedResponse{
			Data: []embedData{{Embedding: wantVec}},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	os.Setenv("EMBED_BASE_URL", srv.URL)
	os.Setenv("EMBED_MODEL", "text-embedding-v4")
	os.Setenv("EMBED_API_KEY", "test-key")
	defer os.Unsetenv("EMBED_BASE_URL")
	defer os.Unsetenv("EMBED_MODEL")
	defer os.Unsetenv("EMBED_API_KEY")

	emb, err := newDashScopeEmbedder()
	if err != nil {
		t.Fatalf("newDashScopeEmbedder: %v", err)
	}
	emb.http = srv.Client()

	got, err := emb.Encode([]string{"retry"})
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	if failures.Load() != 3 {
		t.Fatalf("want 3 total requests (2 failures + 1 success), got %d", failures.Load())
	}
	if len(got) != 1 || len(got[0]) != len(wantVec) {
		t.Fatalf("unexpected result shape")
	}
}

func TestDashScopeEmbedder_MissingEnv(t *testing.T) {
	os.Unsetenv("EMBED_BASE_URL")
	os.Unsetenv("EMBED_MODEL")
	os.Unsetenv("EMBED_API_KEY")

	_, err := newDashScopeEmbedder()
	if err == nil {
		t.Fatal("expected error for missing env, got nil")
	}
	if !strings.Contains(err.Error(), "EMBED_BASE_URL") {
		t.Errorf("error should mention EMBED_BASE_URL: %v", err)
	}
}
