package fallback

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/provider"
	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// EmbeddingFallback tests
// ---------------------------------------------------------------------------

func TestEmbeddingFallback_PrimarySuccess(t *testing.T) {
	fb := NewEmbeddingFallback(nil)
	primary := func() ([][]float32, error) {
		return [][]float32{{1.0, 2.0}}, nil
	}
	res, err := fb.ExecuteEncode(1, "", primary)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 1 || len(res[0]) != 2 {
		t.Fatalf("unexpected result: %v", res)
	}
}

func TestEmbeddingFallback_Trigger429(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	fb := NewEmbeddingFallback(db)
	primary := func() ([][]float32, error) {
		return nil, &provider.APIError{Provider: "test", Status: http.StatusTooManyRequests, Body: "rate limit"}
	}
	res, err := fb.ExecuteEncode(2, "query", primary)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 empty slices, got %v", res)
	}
}

func TestEmbeddingFallback_Trigger503(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	fb := NewEmbeddingFallback(db)
	primary := func() ([][]float32, error) {
		return nil, &provider.APIError{Provider: "test", Status: http.StatusServiceUnavailable, Body: "down"}
	}
	res, err := fb.ExecuteEncode(1, "query", primary)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 empty slice, got %v", res)
	}
}

func TestEmbeddingFallback_NonTriggerError(t *testing.T) {
	fb := NewEmbeddingFallback(nil)
	primary := func() ([][]float32, error) {
		return nil, errors.New("some random error")
	}
	_, err := fb.ExecuteEncode(1, "", primary)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEmbeddingFallback_FallbackFTS5_WithQuery(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create chapters table and insert sample data.
	if _, err := db.Exec(`CREATE TABLE chapters (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL,
		arc_id TEXT,
		volume INTEGER NOT NULL DEFAULT 1,
		chapter_number INTEGER NOT NULL DEFAULT 1,
		title TEXT NOT NULL,
		slug TEXT NOT NULL,
		content TEXT,
		word_count INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'draft',
		created_at INTEGER NOT NULL DEFAULT 0,
		modified_at INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO chapters (id, project_id, title, slug, content) VALUES
		('c1', 'p1', 'Chapter One', 'ch1', 'The hero wakes up in a strange land'),
		('c2', 'p1', 'Chapter Two', 'ch2', 'The villain reveals his master plan')`); err != nil {
		t.Fatal(err)
	}

	fb := NewEmbeddingFallback(db)
	primary := func() ([][]float32, error) {
		return nil, &provider.APIError{Provider: "test", Status: http.StatusTooManyRequests, Body: "rate limit"}
	}
	res, err := fb.ExecuteEncode(2, "hero", primary)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 results, got %d", len(res))
	}
	// Result should be zero vectors (empty slices) indicating fallback.
	for i, v := range res {
		if len(v) != 0 {
			t.Fatalf("expected zero vector at index %d, got %v", i, v)
		}
	}
}

func TestEmbeddingFallback_FallbackFTS5_NoDB(t *testing.T) {
	fb := NewEmbeddingFallback(nil)
	primary := func() ([][]float32, error) {
		return nil, &provider.APIError{Provider: "test", Status: http.StatusTooManyRequests, Body: "rate limit"}
	}
	_, err := fb.ExecuteEncode(1, "query", primary)
	if err == nil {
		t.Fatal("expected error when DB is nil")
	}
}

// ---------------------------------------------------------------------------
// RerankFallback tests
// ---------------------------------------------------------------------------

type fakeEmbedder struct {
	vecs [][]float32
	err  error
}

func (f *fakeEmbedder) Encode(texts []string) ([][]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.vecs, nil
}

func TestRerankFallback_PrimarySuccess(t *testing.T) {
	fb := NewRerankFallback(nil)
	primary := func() ([]RerankResult, error) {
		return []RerankResult{{Document: "doc1", Score: 0.9, Index: 0}}, nil
	}
	res, err := fb.Execute(primary)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 1 || res[0].Document != "doc1" {
		t.Fatalf("unexpected result: %v", res)
	}
}

func TestRerankFallback_TriggerTimeout(t *testing.T) {
	fb := NewRerankFallback(nil)
	primary := func() ([]RerankResult, error) {
		return nil, context.DeadlineExceeded
	}
	_, err := fb.Execute(primary)
	if err == nil {
		t.Fatal("expected error from default fallback")
	}
}

func TestRerankFallback_FallbackWithDocuments(t *testing.T) {
	embedder := &fakeEmbedder{
		vecs: [][]float32{
			{1, 0}, // query
			{1, 0}, // doc0
			{0, 1}, // doc1
		},
	}
	fb := NewRerankFallback(embedder)
	res, err := fb.FallbackWithDocuments("query", []string{"doc0", "doc1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 results, got %d", len(res))
	}
	// doc0 should be first because it is identical to query.
	if res[0].Index != 0 {
		t.Fatalf("expected doc0 first, got index %d", res[0].Index)
	}
	if res[0].Score <= res[1].Score {
		t.Fatalf("expected doc0 score > doc1 score, got %v %v", res[0].Score, res[1].Score)
	}
}

func TestRerankFallback_FallbackWithDocuments_EmbedderError(t *testing.T) {
	embedder := &fakeEmbedder{err: errors.New("embed fail")}
	fb := NewRerankFallback(embedder)
	_, err := fb.FallbackWithDocuments("q", []string{"d"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRerankFallback_ExecuteRerank_PrimarySuccess(t *testing.T) {
	fb := NewRerankFallback(nil)
	res, err := fb.ExecuteRerank("q", []string{"d"}, func() ([]RerankResult, error) {
		return []RerankResult{{Document: "d", Score: 0.9, Index: 0}}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 1 || res[0].Document != "d" {
		t.Fatalf("unexpected result: %v", res)
	}
}

func TestRerankFallback_ExecuteRerank_Fallback(t *testing.T) {
	embedder := &fakeEmbedder{
		vecs: [][]float32{
			{1, 0}, // query
			{1, 0}, // doc0
			{0, 1}, // doc1
		},
	}
	fb := NewRerankFallback(embedder)
	res, err := fb.ExecuteRerank("query", []string{"doc0", "doc1"}, func() ([]RerankResult, error) {
		return nil, &provider.APIError{Provider: "test", Status: http.StatusTooManyRequests, Body: "rate limit"}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 results, got %d", len(res))
	}
	if res[0].Index != 0 {
		t.Fatalf("expected doc0 first, got index %d", res[0].Index)
	}
}

// ---------------------------------------------------------------------------
// CircuitBreaker tests
// ---------------------------------------------------------------------------

func TestCircuitBreaker_ClosedThenOpen(t *testing.T) {
	cb := NewCircuitBreaker(3, 5*time.Second)
	fail := func() (string, error) {
		return "", errors.New("fail")
	}

	// 3 failures should open the circuit.
	for i := 0; i < 3; i++ {
		_, err := cb.Execute(fail)
		if err == nil {
			t.Fatalf("expected error on attempt %d", i+1)
		}
	}

	if cb.State() != CircuitOpen {
		t.Fatalf("expected circuit open, got %v", cb.State())
	}

	// Next call should fast-fail.
	_, err := cb.Execute(func() (string, error) { return "ok", nil })
	if err == nil {
		t.Fatal("expected fast-fail error")
	}
	if !strings.Contains(err.Error(), "circuit breaker: open") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCircuitBreaker_HalfOpenThenClose(t *testing.T) {
	cb := NewCircuitBreaker(2, 50*time.Millisecond)
	fail := func() (string, error) {
		return "", errors.New("fail")
	}

	for i := 0; i < 2; i++ {
		cb.Execute(fail)
	}
	if cb.State() != CircuitOpen {
		t.Fatal("expected circuit open")
	}

	// Wait for cooldown.
	time.Sleep(100 * time.Millisecond)

	// Success should close the circuit.
	res, err := cb.Execute(func() (string, error) { return "recovered", nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != "recovered" {
		t.Fatalf("unexpected result: %s", res)
	}
	if cb.State() != CircuitClosed {
		t.Fatalf("expected circuit closed, got %v", cb.State())
	}
}

func TestCircuitBreaker_SuccessResetsFailures(t *testing.T) {
	cb := NewCircuitBreaker(5, 5*time.Second)
	// First call fails.
	_, _ = cb.Execute(func() (string, error) {
		return "", errors.New("transient")
	})
	if cb.State() != CircuitClosed {
		t.Fatal("expected circuit still closed after single failure")
	}
	// Second call succeeds and resets failures.
	_, err := cb.Execute(func() (string, error) {
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cb.State() != CircuitClosed {
		t.Fatal("expected circuit closed after success")
	}
}

// ---------------------------------------------------------------------------
// TimeoutFallback tests
// ---------------------------------------------------------------------------

func TestTimeoutFallback_PrimaryFast(t *testing.T) {
	fb := NewTimeoutFallback(2*time.Second, func() (string, error) {
		return "fallback", nil
	})
	res, err := fb.Execute(func() (string, error) {
		return "fast", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != "fast" {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestTimeoutFallback_PrimaryTimeout(t *testing.T) {
	fb := NewTimeoutFallback(50*time.Millisecond, func() (string, error) {
		return "fallback", nil
	})
	res, err := fb.Execute(func() (string, error) {
		time.Sleep(200 * time.Millisecond)
		return "slow", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != "fallback" {
		t.Fatalf("expected fallback result, got %s", res)
	}
}

func TestTimeoutFallback_PrimaryTimeout_NoFallbackFunc(t *testing.T) {
	fb := NewTimeoutFallback[string](50*time.Millisecond, nil)
	_, err := fb.Execute(func() (string, error) {
		time.Sleep(200 * time.Millisecond)
		return "slow", nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// LLMFallback tests
// ---------------------------------------------------------------------------

func TestLLMFallback_PrimarySuccess(t *testing.T) {
	fb := NewLLMFallback()
	primary := func() (string, error) {
		return "hello", nil
	}
	res, err := fb.Execute(primary)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != "hello" {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestLLMFallback_RetryThenSuccess(t *testing.T) {
	fb := NewLLMFallback()
	fb.MaxRetries = 2
	attempts := 0
	primary := func() (string, error) {
		attempts++
		if attempts < 2 {
			return "", errors.New("transient")
		}
		return "success", nil
	}
	res, err := fb.Execute(primary)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != "success" {
		t.Fatalf("unexpected result: %s", res)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestLLMFallback_ExhaustedRetries(t *testing.T) {
	fb := NewLLMFallback()
	fb.MaxRetries = 2
	primary := func() (string, error) {
		return "", errors.New("persistent")
	}
	_, err := fb.Execute(primary)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, fmt.Errorf("llm fallback: exhausted after %d retries: %w", 2, errors.New("persistent"))) {
		// Just check it contains the right message.
		if !contains(err.Error(), "exhausted after 2 retries") {
			t.Fatalf("unexpected error message: %v", err)
		}
	}
}

func TestLLMFallback_BackoffTiming(t *testing.T) {
	fb := NewLLMFallback()
	fb.MaxRetries = 2
	fb.MaxJitter = 0 // disable jitter for deterministic timing
	start := time.Now()
	attempts := 0
	primary := func() (string, error) {
		attempts++
		return "", errors.New("fail")
	}
	_, _ = fb.Execute(primary)
	elapsed := time.Since(start)
	// Expected delays: 1s + 2s = 3s (plus tiny overhead).
	if elapsed < 2*time.Second {
		t.Fatalf("expected at least 2s of backoff, got %v", elapsed)
	}
}

func TestLLMFallback_JitterDesync(t *testing.T) {
	fb := NewLLMFallback()
	fb.MaxRetries = 1
	fb.MaxJitter = 100 * time.Millisecond

	var totalDelay int64
	for i := 0; i < 10; i++ {
		start := time.Now()
		fb.Execute(func() (string, error) {
			return "", errors.New("fail")
		})
		d := time.Since(start)
		atomic.AddInt64(&totalDelay, int64(d))
	}
	avg := time.Duration(totalDelay / 10)
	// With jitter each run should take at least 1s; average should be >= 1s.
	if avg < 1*time.Second {
		t.Fatalf("expected average delay >= 1s with jitter, got %v", avg)
	}
}

func TestIsRetryableLLMError(t *testing.T) {
	if IsRetryableLLMError(nil) {
		t.Fatal("nil should not be retryable")
	}
	if !IsRetryableLLMError(errors.New("any")) {
		t.Fatal("any error should be retryable")
	}
	if IsRetryableLLMError(context.Canceled) {
		t.Fatal("context.Canceled should not be retryable")
	}
}

func TestHTTPStatusFromError(t *testing.T) {
	if HTTPStatusFromError(nil) != 0 {
		t.Fatal("expected 0")
	}
	if HTTPStatusFromError(errors.New("plain")) != 0 {
		t.Fatal("expected 0")
	}
	if HTTPStatusFromError(&provider.APIError{Status: 429}) != 429 {
		t.Fatal("expected 429")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || findSubstr(s, substr))
}

func findSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
