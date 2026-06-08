package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"reasonix/internal/netclient"
	"reasonix/internal/novel/cache"
	"reasonix/internal/novel/fallback"
	"reasonix/internal/provider"
)

// Embedder turns text into dense vectors.
type Embedder interface {
	Encode(texts []string) ([][]float32, error)
	EncodeSingle(text string) ([]float32, error)
}

// dashScopeEmbedder calls the DashScope text-embedding API with in-memory
// deduplication caching and exponential-backoff retries.
type dashScopeEmbedder struct {
	baseURL string
	model   string
	apiKey  string
	http    *http.Client

	embedCache *cache.MemoryCache[[]float32]
	fb         *fallback.EmbeddingFallback
}

// newDashScopeEmbedder builds an embedder from environment variables.
func newDashScopeEmbedder() (*dashScopeEmbedder, error) {
	baseURL := os.Getenv("EMBED_BASE_URL")
	model := os.Getenv("EMBED_MODEL")
	apiKey := os.Getenv("EMBED_API_KEY")

	if baseURL == "" {
		return nil, fmt.Errorf("rag: EMBED_BASE_URL is required")
	}
	if model == "" {
		return nil, fmt.Errorf("rag: EMBED_MODEL is required")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("rag: EMBED_API_KEY is required")
	}

	httpClient, err := netclient.NewHTTPClient(netclient.ProxySpec{}, netclient.TransportOptions{
		DialTimeout:           10 * time.Second,
		KeepAlive:             30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("rag: network: %w", err)
	}

	embedCache := cache.NewMemoryCache[[]float32](0, 0)
	cache.RegisterGlobal("embed", embedCache)

	return &dashScopeEmbedder{
		baseURL:    strings.TrimRight(baseURL, "/"),
		model:      model,
		apiKey:     apiKey,
		http:       httpClient,
		embedCache: embedCache,
	}, nil
}

// EncodeSingle embeds one text (convenience wrapper).
func (e *dashScopeEmbedder) EncodeSingle(text string) ([]float32, error) {
	vecs, err := e.Encode([]string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("rag: empty embedding response")
	}
	return vecs[0], nil
}

// Encode embeds a batch of texts, returning vectors in the same order.
// It deduplicates inputs against the local cache and only calls the API for
// unseen texts.  API failures that match the fallback trigger (429/503/timeout)
// degrade to empty vectors so the caller can switch to keyword retrieval.
func (e *dashScopeEmbedder) Encode(texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// 1. Check cache.
	out := make([][]float32, len(texts))
	missingIdx := make([]int, 0, len(texts))
	missingTexts := make([]string, 0, len(texts))

	for i, t := range texts {
		if vec, hit := e.embedCache.Get(t); hit {
			out[i] = vec
			continue
		}
		missingIdx = append(missingIdx, i)
		missingTexts = append(missingTexts, t)
	}

	if len(missingTexts) == 0 {
		return out, nil
	}

	// 2. Call API for missing texts (with optional fallback).
	vecs, err := e.encodeBatchWithFallback(missingTexts)
	if err != nil {
		return nil, err
	}

	// 3. Populate results and cache.
	for j, idx := range missingIdx {
		vec := vecs[j]
		out[idx] = vec
		if len(vec) > 0 {
			e.embedCache.Set(texts[idx], vec)
		}
	}

	return out, nil
}

// encodeBatchWithFallback wraps the raw API call with the EmbeddingFallback
// strategy.  When the fallback activates it returns empty vectors instead of
// failing the whole request.
func (e *dashScopeEmbedder) encodeBatchWithFallback(texts []string) ([][]float32, error) {
	if e.fb == nil {
		return e.encodeBatch(texts)
	}
	return e.fb.ExecuteEncode(len(texts), "", func() ([][]float32, error) {
		return e.encodeBatch(texts)
	})
}

// encodeBatch performs the actual HTTP call with retry logic.
func (e *dashScopeEmbedder) encodeBatch(texts []string) ([][]float32, error) {
	payload := embedRequest{
		Model: e.model,
		Input: embedInput{Texts: texts},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("rag: marshal request: %w", err)
	}

	newReq := func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
		return req, nil
	}

	resp, err := provider.SendWithRetry(context.Background(), e.http, "dashscope-embed", "EMBED_API_KEY", newReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("rag: decode response: %w", err)
	}
	if result.Error != nil && result.Error.Message != "" {
		return nil, fmt.Errorf("rag: %s", result.Error.Message)
	}

	vecs := make([][]float32, 0, len(result.Data))
	for _, d := range result.Data {
		vec := make([]float32, len(d.Embedding))
		copy(vec, d.Embedding)
		vecs = append(vecs, vec)
	}
	if len(vecs) != len(texts) {
		return nil, fmt.Errorf("rag: expected %d embeddings, got %d", len(texts), len(vecs))
	}
	return vecs, nil
}

// --- DashScope wire protocol ---

type embedRequest struct {
	Model string     `json:"model"`
	Input embedInput `json:"input"`
}

type embedInput struct {
	Texts []string `json:"texts"`
}

type embedResponse struct {
	Data  []embedData `json:"data"`
	Error *embedError `json:"error,omitempty"`
}

type embedData struct {
	Embedding []float32 `json:"embedding"`
}

type embedError struct {
	Message string `json:"message"`
}
