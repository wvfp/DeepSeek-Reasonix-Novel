// Package rag implements retrieval-augmented generation helpers for the novel
// subsystem, including embedding and reranking clients.
package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"reasonix/internal/netclient"
	"reasonix/internal/novel/fallback"
	"reasonix/internal/provider"
)

// RerankResult holds one document's relevance score and its original index.
type RerankResult struct {
	Document string  // the document text
	Score    float64 // relevance score (higher = more relevant)
	Index    int     // original position in the input slice
}

// Reranker scores a list of documents against a query.
type Reranker interface {
	Rerank(query string, documents []string) ([]RerankResult, error)
}

// DashScopeReranker calls the DashScope qwen3-rerank API.
type DashScopeReranker struct {
	baseURL string
	model   string
	apiKey  string
	http    *http.Client
	fb      *fallback.RerankFallback
}

// rerankRequest is the wire format for DashScope text-rerank.
type rerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n,omitempty"`
}

// rerankResponse is the wire format returned by DashScope.
type rerankResponse struct {
	Output struct {
		Results []struct {
			Index       int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
			Document    string  `json:"document,omitempty"`
		} `json:"results"`
	} `json:"output"`
	RequestID string `json:"request_id,omitempty"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
}

// NewDashScopeReranker builds a reranker from explicit settings.
func NewDashScopeReranker(baseURL, model, apiKey string) (*DashScopeReranker, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("dashscope reranker: baseURL is required")
	}
	if model == "" {
		return nil, fmt.Errorf("dashscope reranker: model is required")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("dashscope reranker: apiKey is required")
	}
	httpClient, err := netclient.NewHTTPClient(netclient.ProxySpec{}, netclient.TransportOptions{
		DialTimeout:           10 * time.Second,
		KeepAlive:             30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("dashscope reranker: network: %w", err)
	}
	return &DashScopeReranker{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		apiKey:  apiKey,
		http:    httpClient,
	}, nil
}

// Rerank scores documents against query and returns them sorted by descending score.
// When a fallback is configured and the API returns a trigger error (429/503/timeout),
// it degrades to embedding cosine-similarity ranking.
func (r *DashScopeReranker) Rerank(query string, documents []string) ([]RerankResult, error) {
	if len(documents) == 0 {
		return nil, nil
	}
	if r.fb == nil {
		return r.rerankPrimary(query, documents)
	}
	results, err := r.fb.ExecuteRerankTyped(query, documents, func() ([]fallback.RerankResult, error) {
		primary, err := r.rerankPrimary(query, documents)
		if err != nil {
			return nil, err
		}
		out := make([]fallback.RerankResult, len(primary))
		for i, r := range primary {
			out[i] = fallback.RerankResult{
				Document: r.Document,
				Score:    r.Score,
				Index:    r.Index,
			}
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]RerankResult, len(results))
	for i, r := range results {
		out[i] = RerankResult{
			Document: r.Document,
			Score:    r.Score,
			Index:    r.Index,
		}
	}
	return out, nil
}

// rerankPrimary performs the actual HTTP call to the DashScope rerank API.
func (r *DashScopeReranker) rerankPrimary(query string, documents []string) ([]RerankResult, error) {
	body, err := json.Marshal(rerankRequest{
		Model:     r.model,
		Query:     query,
		Documents: documents,
		TopN:      len(documents),
	})
	if err != nil {
		return nil, fmt.Errorf("dashscope rerank: marshal request: %w", err)
	}

	newReq := func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+r.apiKey)
		return req, nil
	}

	resp, err := provider.SendWithRetry(context.Background(), r.http, "dashscope-rerank", "", newReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("dashscope rerank: read body: %w", err)
	}

	var rr rerankResponse
	if err := json.Unmarshal(data, &rr); err != nil {
		return nil, fmt.Errorf("dashscope rerank: decode response: %w", err)
	}
	if rr.Code != "" {
		return nil, fmt.Errorf("dashscope rerank: %s (code=%s request_id=%s)", rr.Message, rr.Code, rr.RequestID)
	}

	results := make([]RerankResult, 0, len(rr.Output.Results))
	for _, res := range rr.Output.Results {
		doc := res.Document
		if doc == "" && res.Index >= 0 && res.Index < len(documents) {
			doc = documents[res.Index]
		}
		results = append(results, RerankResult{
			Document: doc,
			Score:    res.RelevanceScore,
			Index:    res.Index,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results, nil
}

// newDashScopeRerankerFromEnv builds a Reranker from environment variables
// (RERANK_BASE_URL, RERANK_MODEL, RERANK_API_KEY). It returns nil when the
// base URL is empty, allowing callers to treat reranking as optional.
func newDashScopeRerankerFromEnv() (Reranker, error) {
	baseURL := os.Getenv("RERANK_BASE_URL")
	if baseURL == "" {
		return nil, nil
	}
	model := os.Getenv("RERANK_MODEL")
	if model == "" {
		model = "qwen3-rerank"
	}
	apiKey := os.Getenv("RERANK_API_KEY")
	return NewDashScopeReranker(baseURL, model, apiKey)
}
