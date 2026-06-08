// Package rag provides retrieval-augmented generation support for the novel
// pipeline, including text embedding via DashScope.
package rag

import (
	"database/sql"

	"reasonix/internal/novel/fallback"
)

// NewEmbedder creates the default Embedder implementation backed by the
// DashScope text-embedding API. Configuration is read from environment
// variables (EMBED_BASE_URL, EMBED_MODEL, EMBED_API_KEY).
// An optional EmbeddingFallback can be injected via SetFallback on the
// returned concrete type.
func NewEmbedder() (Embedder, error) {
	return newDashScopeEmbedder()
}

// NewEmbedderWithFallback creates an Embedder and wires an
// EmbeddingFallback backed by the supplied DB.
func NewEmbedderWithFallback(db *sql.DB) (Embedder, error) {
	e, err := newDashScopeEmbedder()
	if err != nil {
		return nil, err
	}
	if db != nil {
		e.fb = fallback.NewEmbeddingFallback(db)
	}
	return e, nil
}

// NewReranker creates the default Reranker implementation backed by the
// DashScope text-rerank API. Configuration is read from environment
// variables (RERANK_BASE_URL, RERANK_MODEL, RERANK_API_KEY).
func NewReranker() (Reranker, error) {
	return newDashScopeRerankerFromEnv()
}

// NewRerankerWithFallback creates a Reranker and wires a RerankFallback
// backed by the supplied embedder.
func NewRerankerWithFallback(embedder fallback.Embedder) (Reranker, error) {
	r, err := newDashScopeRerankerFromEnv()
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, nil
	}
	dr, ok := r.(*DashScopeReranker)
	if !ok {
		return r, nil
	}
	if embedder != nil {
		dr.fb = fallback.NewRerankFallback(embedder)
	}
	return dr, nil
}

// NewVectorStore creates the default VectorStore implementation backed by
// SQLite. It ensures the vec_chapters table exists.
func NewVectorStore(db *sql.DB) (VectorStore, error) {
	return NewSQLiteVecStore(db)
}
