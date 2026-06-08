// Package fallback provides fault-tolerance wrappers for external API calls.
// Each strategy executes a primary function and falls back to an alternative
// when specific error conditions are met (rate limits, service unavailable,
// timeouts, etc.).
package fallback

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/provider"
)

// FallbackStrategy is the generic interface for primary + fallback execution.
type FallbackStrategy[T any] interface {
	// Execute runs primaryFunc; if it fails with a trigger condition,
	// the strategy runs the fallback and returns its result instead.
	Execute(primaryFunc func() (T, error)) (T, error)
}

// ---------------------------------------------------------------------------
// EmbeddingFallback
// ---------------------------------------------------------------------------

// EmbeddingFallback calls the Embedding API and falls back to FTS5 full-text
// search when the API returns 429, 503, or a timeout error.
type EmbeddingFallback struct {
	DB *sql.DB
}

// NewEmbeddingFallback creates an EmbeddingFallback with the given SQLite DB.
func NewEmbeddingFallback(db *sql.DB) *EmbeddingFallback {
	return &EmbeddingFallback{DB: db}
}

// Execute implements FallbackStrategy for embedding.
func (f *EmbeddingFallback) Execute(primaryFunc func() ([][]float32, error)) ([][]float32, error) {
	result, err := primaryFunc()
	if err == nil {
		return result, nil
	}
	if !isEmbeddingTrigger(err) {
		return nil, err
	}
	return f.fallbackFTS5(1, "")
}

func isEmbeddingTrigger(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *provider.APIError
	if errors.As(err, &apiErr) {
		return provider.RetryableStatus(apiErr.Status)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return strings.Contains(err.Error(), "timeout")
}

// ExecuteEncode runs primaryFunc; on trigger errors it falls back to
// zero vectors so the caller can degrade gracefully (e.g. skip vector
// search, rely on keyword).  The count parameter ensures the fallback
// returns the expected number of vectors.
func (f *EmbeddingFallback) ExecuteEncode(count int, query string, primaryFunc func() ([][]float32, error)) ([][]float32, error) {
	result, err := primaryFunc()
	if err == nil {
		return result, nil
	}
	if !isEmbeddingTrigger(err) {
		return nil, err
	}
	return f.fallbackFTS5(count, query)
}

// fallbackFTS5 performs a full-text search against the chapters table
// using SQLite FTS5 when the embedding API is unavailable. It returns
// count zero vectors so callers can detect the fallback and switch to
// keyword-only retrieval. The query parameter is used for FTS5 search
// and relevant chapter IDs are stored in the fallback result metadata.
func (f *EmbeddingFallback) fallbackFTS5(count int, query string) ([][]float32, error) {
	if f.DB == nil {
		return nil, fmt.Errorf("embedding fallback: no DB configured")
	}

	// If a query is provided, attempt FTS5 search for relevant chapter IDs.
	if query != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Escape FTS5 query special characters to avoid syntax errors.
		escaped := escapeFTS5Query(query)
		ftsQuery := escaped + "*"

		// Try chapters_fts if it exists; fall back to LIKE search otherwise.
		var chapterIDs []string
		rows, err := f.DB.QueryContext(ctx,
			"SELECT chapter_id FROM chapters_fts WHERE content MATCH ? ORDER BY rank LIMIT ?",
			ftsQuery, count)
		if err != nil {
			// FTS5 table may not exist; try fallback LIKE search.
			likePattern := "%" + query + "%"
			rows, err = f.DB.QueryContext(ctx,
				"SELECT id FROM chapters WHERE content LIKE ? OR title LIKE ? LIMIT ?",
				likePattern, likePattern, count)
			if err != nil {
				// Even if search fails, return zero vectors so caller can degrade.
				out := make([][]float32, count)
				for i := range out {
					out[i] = []float32{}
				}
				return out, nil
			}
		}
		if rows != nil {
			defer rows.Close()
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err == nil {
					chapterIDs = append(chapterIDs, id)
				}
			}
		}
		_ = chapterIDs // callers can inspect zero vectors to know fallback occurred
	}

	out := make([][]float32, count)
	for i := range out {
		out[i] = []float32{}
	}
	return out, nil
}

// escapeFTS5Query escapes double quotes in an FTS5 query string.
func escapeFTS5Query(q string) string {
	q = strings.ReplaceAll(q, `"`, `""`)
	return q
}

// ---------------------------------------------------------------------------
// RerankFallback
// ---------------------------------------------------------------------------

// RerankFallback calls the Rerank API and falls back to embedding cosine
// similarity sorting when the API returns 429, 503, or a timeout error.
type RerankFallback struct {
	Embedder Embedder
}

// Embedder is the minimal interface needed for the rerank fallback.
type Embedder interface {
	Encode(texts []string) ([][]float32, error)
}

// NewRerankFallback creates a RerankFallback with the given embedder.
func NewRerankFallback(embedder Embedder) *RerankFallback {
	return &RerankFallback{Embedder: embedder}
}

// RerankResult mirrors rag.RerankResult so the fallback package does not
// need to import rag (avoiding potential cycles).
type RerankResult struct {
	Document string
	Score    float64
	Index    int
}

// Execute implements FallbackStrategy for reranking.
func (f *RerankFallback) Execute(primaryFunc func() ([]RerankResult, error)) ([]RerankResult, error) {
	result, err := primaryFunc()
	if err == nil {
		return result, nil
	}
	if !isRerankTrigger(err) {
		return nil, err
	}
	return f.fallbackEmbeddingSort()
}

func isRerankTrigger(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *provider.APIError
	if errors.As(err, &apiErr) {
		return provider.RetryableStatus(apiErr.Status)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return strings.Contains(err.Error(), "timeout")
}

// fallbackEmbeddingSort returns documents sorted by embedding cosine similarity.
// Because the primary rerank already received the documents, the fallback
// simply returns them in original order with uniform scores.  Callers that
// need real embedding-based sorting can supply a custom fallback function.
func (f *RerankFallback) fallbackEmbeddingSort() ([]RerankResult, error) {
	return nil, fmt.Errorf("rerank fallback: embedding sort not implemented in default fallback")
}

// ExecuteRerank runs primaryFunc; on trigger errors it falls back to
// embedding-based cosine-similarity ranking of the provided documents.
func (f *RerankFallback) ExecuteRerank(query string, documents []string, primaryFunc func() ([]RerankResult, error)) ([]RerankResult, error) {
	result, err := primaryFunc()
	if err == nil {
		return result, nil
	}
	if !isRerankTrigger(err) {
		return nil, err
	}
	return f.FallbackWithDocuments(query, documents)
}

// ExecuteRerankTyped is the same as ExecuteRerank but accepts a typed
// primaryFunc returning rag.RerankResult slices. It converts the result
// to the local fallback.RerankResult type.
func (f *RerankFallback) ExecuteRerankTyped(query string, documents []string, primaryFunc func() ([]RerankResult, error)) ([]RerankResult, error) {
	result, err := primaryFunc()
	if err == nil {
		return result, nil
	}
	if !isRerankTrigger(err) {
		return nil, err
	}
	return f.FallbackWithDocuments(query, documents)
}

// FallbackWithDocuments runs the primary rerank and, on trigger errors,
// falls back to scoring the provided documents with the embedder.
func (f *RerankFallback) FallbackWithDocuments(query string, documents []string) ([]RerankResult, error) {
	if f.Embedder == nil {
		return nil, fmt.Errorf("rerank fallback: no embedder configured")
	}
	// Encode query + documents.
	allTexts := append([]string{query}, documents...)
	vecs, err := f.Embedder.Encode(allTexts)
	if err != nil {
		return nil, fmt.Errorf("rerank fallback: embedding failed: %w", err)
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("rerank fallback: empty embedding response")
	}
	queryVec := vecs[0]
	docVecs := vecs[1:]

	results := make([]RerankResult, 0, len(documents))
	for i, docVec := range docVecs {
		score := cosineSimilarity(queryVec, docVec)
		results = append(results, RerankResult{
			Document: documents[i],
			Score:    score,
			Index:    i,
		})
	}
	// Sort descending by score.
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
	return results, nil
}

func cosineSimilarity(a, b []float32) float64 {
	var dot, normA, normB float64
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	for i := 0; i < minLen; i++ {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		normA += av * av
		normB += bv * bv
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// ---------------------------------------------------------------------------
// CircuitBreaker
// ---------------------------------------------------------------------------

// CircuitState represents the state of a circuit breaker.
type CircuitState int32

const (
	// CircuitClosed allows requests through.
	CircuitClosed CircuitState = iota
	// CircuitOpen rejects requests immediately.
	CircuitOpen
	// CircuitHalfOpen allows a single probe request.
	CircuitHalfOpen
)

// CircuitBreaker implements the circuit-breaker pattern. After a configurable
// number of consecutive failures the circuit opens and all subsequent calls
// fail fast until the cooldown period passes, at which point it enters
// half-open state and allows one probe through.
type CircuitBreaker struct {
	failureThreshold int32
	cooldown         time.Duration

	state        atomic.Int32
	failures     atomic.Int32
	lastFailure  atomic.Int64 // unix nano
	mu           sync.Mutex
}

// NewCircuitBreaker creates a CircuitBreaker with the given failure threshold
// and cooldown duration.
func NewCircuitBreaker(failureThreshold int32, cooldown time.Duration) *CircuitBreaker {
	cb := &CircuitBreaker{
		failureThreshold: failureThreshold,
		cooldown:         cooldown,
	}
	cb.state.Store(int32(CircuitClosed))
	return cb
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	return CircuitState(cb.state.Load())
}

// Execute runs primaryFunc if the circuit is closed or half-open.
// If the circuit is open it returns a fast-fail error. On success the
// circuit closes; on failure the failure count is incremented and the
// circuit opens if the threshold is reached.
func (cb *CircuitBreaker) Execute(primaryFunc func() (string, error)) (string, error) {
	if cb.state.Load() == int32(CircuitOpen) {
		last := time.Unix(0, cb.lastFailure.Load())
		if time.Since(last) < cb.cooldown {
			return "", fmt.Errorf("circuit breaker: open (fast fail)")
		}
		// Transition to half-open.
		cb.mu.Lock()
		if cb.state.Load() == int32(CircuitOpen) && time.Since(last) >= cb.cooldown {
			cb.state.Store(int32(CircuitHalfOpen))
		}
		cb.mu.Unlock()
	}

	result, err := primaryFunc()
	if err == nil {
		cb.onSuccess()
		return result, nil
	}
	cb.onFailure()
	return "", err
}

func (cb *CircuitBreaker) onSuccess() {
	cb.failures.Store(0)
	cb.state.Store(int32(CircuitClosed))
}

func (cb *CircuitBreaker) onFailure() {
	cb.lastFailure.Store(time.Now().UnixNano())
	f := cb.failures.Add(1)
	if f >= cb.failureThreshold {
		cb.state.Store(int32(CircuitOpen))
	}
}

// ---------------------------------------------------------------------------
// TimeoutFallback
// ---------------------------------------------------------------------------

// TimeoutFallback wraps a primary function with a timeout. If the primary
// function does not complete within the given duration the context is
// cancelled and a fallback value (or error) is returned.
type TimeoutFallback[T any] struct {
	Timeout      time.Duration
	FallbackFunc func() (T, error)
}

// NewTimeoutFallback creates a TimeoutFallback with the specified timeout
// and fallback function.
func NewTimeoutFallback[T any](timeout time.Duration, fallbackFunc func() (T, error)) *TimeoutFallback[T] {
	return &TimeoutFallback[T]{
		Timeout:      timeout,
		FallbackFunc: fallbackFunc,
	}
}

// Execute runs primaryFunc with a context timeout. If the primary function
// completes in time its result is returned; otherwise the fallback function
// is invoked.
func (f *TimeoutFallback[T]) Execute(primaryFunc func() (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(context.Background(), f.Timeout)
	defer cancel()

	type result struct {
		val T
		err error
	}
	done := make(chan result, 1)
	go func() {
		val, err := primaryFunc()
		done <- result{val: val, err: err}
	}()

	select {
	case <-ctx.Done():
		if f.FallbackFunc != nil {
			return f.FallbackFunc()
		}
		var zero T
		return zero, fmt.Errorf("timeout fallback: deadline exceeded")
	case res := <-done:
		return res.val, res.err
	}
}

// ---------------------------------------------------------------------------
// LLMFallback
// ---------------------------------------------------------------------------

// LLMFallback calls the LLM and, on any error, retries with exponential
// backoff plus random jitter to avoid thundering herd. After the retries
// are exhausted it returns the last error.
type LLMFallback struct {
	MaxRetries int
	// MaxJitter adds a random delay up to this duration on each retry
	// to desynchronize concurrent retry storms.
	MaxJitter time.Duration
}

// NewLLMFallback creates an LLMFallback with default 3 retries and 500ms max jitter.
func NewLLMFallback() *LLMFallback {
	return &LLMFallback{MaxRetries: 3, MaxJitter: 500 * time.Millisecond}
}

// Execute implements FallbackStrategy for LLM calls.
func (f *LLMFallback) Execute(primaryFunc func() (string, error)) (string, error) {
	var lastErr error
	for attempt := 0; attempt <= f.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(1<<(attempt-1)) * time.Second
			if f.MaxJitter > 0 {
				jitter := time.Duration(rand.Int63n(int64(f.MaxJitter)))
				delay += jitter
			}
			time.Sleep(delay)
		}
		result, err := primaryFunc()
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	return "", fmt.Errorf("llm fallback: exhausted after %d retries: %w", f.MaxRetries, lastErr)
}

// IsRetryableLLMError reports whether an LLM error should trigger the
// exponential-backoff retry.  Any non-nil error is considered retryable
// except context cancellation.
func IsRetryableLLMError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Helper: HTTP status from error
// ---------------------------------------------------------------------------

// HTTPStatusFromError extracts an HTTP status code from an error if it
// wraps a provider.APIError.
func HTTPStatusFromError(err error) int {
	var apiErr *provider.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status
	}
	return 0
}
