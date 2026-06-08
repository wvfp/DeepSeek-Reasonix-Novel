package rag

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
)

func TestDashScopeReranker_BatchScoring(t *testing.T) {
	wantQuery := "query text"
	wantDocs := []string{"doc a", "doc b", "doc c"}

	 srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("want POST, got %s", r.Method)
		}
		if h := r.Header.Get("Authorization"); h != "Bearer test-key" {
			t.Errorf("want Authorization=Bearer test-key, got %s", h)
		}

		var req rerankRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Query != wantQuery {
			t.Errorf("query=%q, want %q", req.Query, wantQuery)
		}
		if len(req.Documents) != len(wantDocs) {
			t.Errorf("len(documents)=%d, want %d", len(req.Documents), len(wantDocs))
		}

		resp := rerankResponse{}
		resp.Output.Results = []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
			Document       string  `json:"document,omitempty"`
		}{
			{Index: 0, RelevanceScore: 0.1, Document: "doc a"},
			{Index: 1, RelevanceScore: 0.9, Document: "doc b"},
			{Index: 2, RelevanceScore: 0.5, Document: "doc c"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	r, err := NewDashScopeReranker(srv.URL, "qwen3-rerank", "test-key")
	if err != nil {
		t.Fatalf("new reranker: %v", err)
	}

	results, err := r.Rerank(wantQuery, wantDocs)
	if err != nil {
		t.Fatalf("rerank: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("want 3 results, got %d", len(results))
	}

	// Should be sorted descending by score: 0.9, 0.5, 0.1
	if results[0].Score != 0.9 || results[0].Index != 1 {
		t.Errorf("result[0] = {score:%.1f index:%d}, want {0.9 1}", results[0].Score, results[0].Index)
	}
	if results[1].Score != 0.5 || results[1].Index != 2 {
		t.Errorf("result[1] = {score:%.1f index:%d}, want {0.5 2}", results[1].Score, results[1].Index)
	}
	if results[2].Score != 0.1 || results[2].Index != 0 {
		t.Errorf("result[2] = {score:%.1f index:%d}, want {0.1 0}", results[2].Score, results[2].Index)
	}
}

func TestDashScopeReranker_EmptyDocuments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called for empty documents")
	}))
	defer srv.Close()

	r, _ := NewDashScopeReranker(srv.URL, "m", "k")
	results, err := r.Rerank("q", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("want 0 results, got %d", len(results))
	}
}

func TestDashScopeReranker_RetryOnRateLimit(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		resp := rerankResponse{}
		resp.Output.Results = []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
			Document       string  `json:"document,omitempty"`
		}{
			{Index: 0, RelevanceScore: 0.8, Document: "doc"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	r, _ := NewDashScopeReranker(srv.URL, "m", "k")
	results, err := r.Rerank("q", []string{"doc"})
	if err != nil {
		t.Fatalf("rerank: %v", err)
	}
	if calls.Load() != 3 {
		t.Errorf("want 3 calls, got %d", calls.Load())
	}
	if len(results) != 1 || results[0].Score != 0.8 {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestDashScopeReranker_RetryOnServiceUnavailable(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		resp := rerankResponse{}
		resp.Output.Results = []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
			Document       string  `json:"document,omitempty"`
		}{
			{Index: 0, RelevanceScore: 0.6, Document: "doc"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	r, _ := NewDashScopeReranker(srv.URL, "m", "k")
	results, err := r.Rerank("q", []string{"doc"})
	if err != nil {
		t.Fatalf("rerank: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("want 2 calls, got %d", calls.Load())
	}
	if len(results) != 1 || results[0].Score != 0.6 {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestDashScopeReranker_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := rerankResponse{
			Code:    "InvalidParameter",
			Message: "model not found",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	r, _ := NewDashScopeReranker(srv.URL, "m", "k")
	_, err := r.Rerank("q", []string{"doc"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNewReranker_FromEnv(t *testing.T) {
	t.Setenv("RERANK_BASE_URL", "https://example.com/rerank")
	t.Setenv("RERANK_MODEL", "qwen3-rerank")
	t.Setenv("RERANK_API_KEY", "key123")

	reranker, err := newDashScopeRerankerFromEnv()
	if err != nil {
		t.Fatalf("newDashScopeRerankerFromEnv: %v", err)
	}
	if reranker == nil {
		t.Fatal("expected non-nil reranker")
	}
	dr, ok := reranker.(*DashScopeReranker)
	if !ok {
		t.Fatalf("expected *DashScopeReranker, got %T", reranker)
	}
	if dr.baseURL != "https://example.com/rerank" {
		t.Errorf("baseURL=%q, want %q", dr.baseURL, "https://example.com/rerank")
	}
	if dr.model != "qwen3-rerank" {
		t.Errorf("model=%q, want %q", dr.model, "qwen3-rerank")
	}
	if dr.apiKey != "key123" {
		t.Errorf("apiKey=%q, want %q", dr.apiKey, "key123")
	}
}

func TestNewReranker_MissingBaseURL(t *testing.T) {
	os.Unsetenv("RERANK_BASE_URL")
	os.Unsetenv("RERANK_MODEL")
	os.Unsetenv("RERANK_API_KEY")

	reranker, err := newDashScopeRerankerFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reranker != nil {
		t.Fatal("expected nil reranker when base URL is missing")
	}
}

func TestDashScopeReranker_FallbackDocument(t *testing.T) {
	// API returns empty Document field; we should fall back to input slice.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := rerankResponse{}
		resp.Output.Results = []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
			Document       string  `json:"document,omitempty"`
		}{
			{Index: 0, RelevanceScore: 0.7, Document: ""},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	r, _ := NewDashScopeReranker(srv.URL, "m", "k")
	results, err := r.Rerank("q", []string{"original doc"})
	if err != nil {
		t.Fatalf("rerank: %v", err)
	}
	if len(results) != 1 || results[0].Document != "original doc" {
		t.Fatalf("unexpected document: %+v", results)
	}
}

func TestDashScopeReranker_TopNEqualsLen(t *testing.T) {
	var captured rerankRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}
		resp := rerankResponse{}
		resp.Output.Results = []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
			Document       string  `json:"document,omitempty"`
		}{
			{Index: 0, RelevanceScore: 0.5, Document: "d"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	r, _ := NewDashScopeReranker(srv.URL, "m", "k")
	_, _ = r.Rerank("q", []string{"d"})
	if captured.TopN != 1 {
		t.Errorf("top_n=%d, want 1", captured.TopN)
	}
}

func TestDashScopeReranker_NewValidation(t *testing.T) {
	_, err := NewDashScopeReranker("", "m", "k")
	if err == nil {
		t.Error("expected error for empty baseURL")
	}
	_, err = NewDashScopeReranker("http://x", "", "k")
	if err == nil {
		t.Error("expected error for empty model")
	}
	_, err = NewDashScopeReranker("http://x", "m", "")
	if err == nil {
		t.Error("expected error for empty apiKey")
	}
}

func TestDashScopeReranker_DefaultModelFromEnv(t *testing.T) {
	t.Setenv("RERANK_BASE_URL", "https://example.com/rerank")
	os.Unsetenv("RERANK_MODEL")
	t.Setenv("RERANK_API_KEY", "key")

	reranker, err := newDashScopeRerankerFromEnv()
	if err != nil {
		t.Fatalf("newDashScopeRerankerFromEnv: %v", err)
	}
	dr := reranker.(*DashScopeReranker)
	if dr.model != "qwen3-rerank" {
		t.Errorf("model=%q, want default qwen3-rerank", dr.model)
	}
}

func BenchmarkDashScopeReranker_Sort(b *testing.B) {
	docs := make([]string, 100)
	for i := range docs {
		docs[i] = "document " + strconv.Itoa(i)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := rerankResponse{}
		resp.Output.Results = make([]struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
			Document       string  `json:"document,omitempty"`
		}, len(docs))
		for i := range docs {
			resp.Output.Results[i] = struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
			Document       string  `json:"document,omitempty"`
		}{Index: i, RelevanceScore: float64(i) * 0.01, Document: docs[i]}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}))
	defer srv.Close()

	r, _ := NewDashScopeReranker(srv.URL, "m", "k")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := r.Rerank("query", docs)
		if err != nil {
			b.Fatal(err)
		}
	}
}
