package rag

import (
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"time"

	"reasonix/internal/novel/cache"
)

// SearchResult holds one chapter's similarity score from a vector search.
type SearchResult struct {
	ChapterID string  // chapter identifier
	Score     float64 // cosine similarity (higher = more similar)
}

// VectorStore persists and queries chapter embeddings.
type VectorStore interface {
	// Insert stores the embedding for a single chapter.
	Insert(chapterID string, embedding []float32) error
	// Search returns the top-K most similar chapters to the query vector.
	Search(query []float32, topK int) ([]SearchResult, error)
	// Delete removes the embedding for a chapter.
	Delete(chapterID string) error
}

// SQLiteVecStore implements VectorStore using SQLite with BLOB storage.
// It stores float32 embeddings as little-endian binary blobs and performs
// cosine similarity in Go (full-table scan). This is acceptable for ~1000
// chapters.
type SQLiteVecStore struct {
	db *sql.DB

	// searchCache caches query_hash → topK search results.
	searchCache *cache.MemoryCache[[]SearchResult]
	// summaryCache caches chapter_id → *ChapterSummary.
	summaryCache *cache.MemoryCache[*ChapterSummary]
}

// NewSQLiteVecStore creates a VectorStore backed by the given SQLite DB.
// It ensures the vec_chapters table exists.
func NewSQLiteVecStore(db *sql.DB) (*SQLiteVecStore, error) {
	if db == nil {
		return nil, fmt.Errorf("rag: NewSQLiteVecStore: db is nil")
	}
	const createTable = `CREATE TABLE IF NOT EXISTS vec_chapters (
		chapter_id TEXT PRIMARY KEY,
		embedding BLOB NOT NULL
	);`
	if _, err := db.Exec(createTable); err != nil {
		return nil, fmt.Errorf("rag: create vec_chapters table: %w", err)
	}

	searchCache := cache.NewMemoryCache[[]SearchResult](5 * time.Minute, 1000)
	summaryCache := cache.NewMemoryCache[*ChapterSummary](10 * time.Minute, 1000)
	cache.RegisterGlobal("vector_search", searchCache)
	cache.RegisterGlobal("vector_summary", summaryCache)

	return &SQLiteVecStore{
		db:           db,
		searchCache:  searchCache,
		summaryCache: summaryCache,
	}, nil
}

// Insert stores a single chapter embedding.
func (s *SQLiteVecStore) Insert(chapterID string, embedding []float32) error {
	if chapterID == "" {
		return fmt.Errorf("rag: Insert: chapterID is empty")
	}
	if len(embedding) == 0 {
		return fmt.Errorf("rag: Insert: embedding is empty")
	}
	blob := encodeFloat32s(embedding)
	const q = `INSERT INTO vec_chapters (chapter_id, embedding) VALUES (?, ?)
			   ON CONFLICT(chapter_id) DO UPDATE SET embedding = excluded.embedding;`
	if _, err := s.db.Exec(q, chapterID, blob); err != nil {
		return fmt.Errorf("rag: Insert %q: %w", chapterID, err)
	}
	// Invalidate summary cache for this chapter since embedding changed.
	if s.summaryCache != nil {
		s.summaryCache.Delete(chapterID)
	}
	return nil
}

// InsertBatch stores multiple chapter embeddings in a single transaction.
func (s *SQLiteVecStore) InsertBatch(items map[string][]float32) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("rag: InsertBatch: begin tx: %w", err)
	}
	defer tx.Rollback() // safe no-op after commit

	const q = `INSERT INTO vec_chapters (chapter_id, embedding) VALUES (?, ?)
			   ON CONFLICT(chapter_id) DO UPDATE SET embedding = excluded.embedding;`
	stmt, err := tx.Prepare(q)
	if err != nil {
		return fmt.Errorf("rag: InsertBatch: prepare: %w", err)
	}
	defer stmt.Close()

	for chapterID, embedding := range items {
		if chapterID == "" || len(embedding) == 0 {
			continue
		}
		blob := encodeFloat32s(embedding)
		if _, err := stmt.Exec(chapterID, blob); err != nil {
			return fmt.Errorf("rag: InsertBatch %q: %w", chapterID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("rag: InsertBatch: commit: %w", err)
	}
	// Invalidate summary cache for updated chapters.
	if s.summaryCache != nil {
		for chapterID := range items {
			s.summaryCache.Delete(chapterID)
		}
	}
	return nil
}

// queryHash returns a stable hash key for a query vector + topK.
func queryHash(query []float32, topK int) string {
	h := sha256.New()
	for _, v := range query {
		_ = binary.Write(h, binary.LittleEndian, v)
	}
	_ = binary.Write(h, binary.LittleEndian, int64(topK))
	return hex.EncodeToString(h.Sum(nil))
}

// Search computes cosine similarity between the query vector and all stored
// embeddings, returning the topK results sorted by descending score.
func (s *SQLiteVecStore) Search(query []float32, topK int) ([]SearchResult, error) {
	if len(query) == 0 {
		return nil, fmt.Errorf("rag: Search: query is empty")
	}
	if topK <= 0 {
		return nil, nil
	}

	// Check search cache.
	qhash := queryHash(query, topK)
	if s.searchCache != nil {
		if cached, hit := s.searchCache.Get(qhash); hit {
			// Return a copy to prevent external mutation.
			out := make([]SearchResult, len(cached))
			copy(out, cached)
			return out, nil
		}
	}

	rows, err := s.db.Query(`SELECT chapter_id, embedding FROM vec_chapters`)
	if err != nil {
		return nil, fmt.Errorf("rag: Search: query: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var chapterID string
		var blob []byte
		if err := rows.Scan(&chapterID, &blob); err != nil {
			return nil, fmt.Errorf("rag: Search: scan: %w", err)
		}
		vec, err := decodeFloat32s(blob)
		if err != nil {
			return nil, fmt.Errorf("rag: Search: decode embedding for %q: %w", chapterID, err)
		}
		if len(vec) == 0 {
			continue
		}
		score := cosineSimilarity(query, vec)
		results = append(results, SearchResult{ChapterID: chapterID, Score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rag: Search: rows error: %w", err)
	}

	// Sort descending by score.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > topK {
		results = results[:topK]
	}

	// Store in cache.
	if s.searchCache != nil {
		// Store a copy to prevent external mutation.
		cached := make([]SearchResult, len(results))
		copy(cached, results)
		s.searchCache.Set(qhash, cached)
	}

	return results, nil
}

// Delete removes the embedding for a chapter.
func (s *SQLiteVecStore) Delete(chapterID string) error {
	if chapterID == "" {
		return fmt.Errorf("rag: Delete: chapterID is empty")
	}
	const q = `DELETE FROM vec_chapters WHERE chapter_id = ?;`
	if _, err := s.db.Exec(q, chapterID); err != nil {
		return fmt.Errorf("rag: Delete %q: %w", chapterID, err)
	}
	// Invalidate caches for this chapter.
	if s.summaryCache != nil {
		s.summaryCache.Delete(chapterID)
	}
	return nil
}

// GetSummary returns a cached ChapterSummary for a chapter if available.
func (s *SQLiteVecStore) GetSummary(chapterID string) (*ChapterSummary, bool) {
	if s.summaryCache == nil || chapterID == "" {
		return nil, false
	}
	cs, hit := s.summaryCache.Get(chapterID)
	if !hit {
		return nil, false
	}
	return cs, true
}

// SetSummary stores a ChapterSummary in the cache.
func (s *SQLiteVecStore) SetSummary(chapterID string, cs *ChapterSummary) {
	if s.summaryCache == nil || chapterID == "" || cs == nil {
		return
	}
	s.summaryCache.Set(chapterID, cs)
}

// SearchStats returns the search cache statistics.
func (s *SQLiteVecStore) SearchStats() cache.CacheStats {
	if s.searchCache == nil {
		return cache.CacheStats{}
	}
	return s.searchCache.Stats()
}

// SummaryStats returns the summary cache statistics.
func (s *SQLiteVecStore) SummaryStats() cache.CacheStats {
	if s.summaryCache == nil {
		return cache.CacheStats{}
	}
	return s.summaryCache.Stats()
}

// --- helpers ---

// encodeFloat32s converts a float32 slice to a little-endian binary blob.
func encodeFloat32s(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// decodeFloat32s converts a little-endian binary blob to a float32 slice.
func decodeFloat32s(b []byte) ([]float32, error) {
	if len(b)%4 != 0 {
		return nil, fmt.Errorf("blob length %d is not a multiple of 4", len(b))
	}
	n := len(b) / 4
	v := make([]float32, n)
	for i := 0; i < n; i++ {
		bits := binary.LittleEndian.Uint32(b[i*4:])
		v[i] = math.Float32frombits(bits)
	}
	return v, nil
}

// cosineSimilarity returns the cosine similarity of a and b.
// Vectors are assumed to be non-empty; the caller checks this.
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
