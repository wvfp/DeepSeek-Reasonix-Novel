// Package async provides simple background task utilities for the novel
// pipeline.  It is intentionally minimal: a single goroutine per indexer
// that drains a buffered channel.
package async

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/novel/db"
)

const (
	maxRetries     = 3
	retryDelayBase = 500 * time.Millisecond
	batchSize      = 8
)

// Embedder turns text into dense vectors.  This is a local copy of the
// rag.Embedder interface to avoid an import cycle (async must not import
// rag because rag already imports tools, and tools imports async).
type Embedder interface {
	Encode(texts []string) ([][]float32, error)
	EncodeSingle(text string) ([]float32, error)
}

// VectorStore persists chapter embeddings.
type VectorStore interface {
	Insert(chapterID string, embedding []float32) error
}

// IndexTask represents one unit of background work: generate an embedding
// for a chapter and store it in the vector table.
type IndexTask struct {
	ChapterID string
	Summary   string // the text to embed (usually chapter summary or content)
}

// Stats holds indexer processing statistics.
type Stats struct {
	Success uint64
	Failed  uint64
	Dropped uint64
}

// Indexer consumes IndexTasks asynchronously.
type Indexer struct {
	embedder    Embedder
	vectorStore VectorStore
	queue       chan IndexTask
	stop        chan struct{}
	shutdown    chan struct{}
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc

	success atomic.Uint64
	failed  atomic.Uint64
	dropped atomic.Uint64
}

// NewIndexer creates an Indexer with a buffered queue and starts the
// background worker.  If embedder is nil, tasks are accepted but
// immediately discarded (useful in tests or when embedding is disabled).
func NewIndexer(embedder Embedder, vectorStore VectorStore, queueSize int) *Indexer {
	ctx, cancel := context.WithCancel(context.Background())
	i := &Indexer{
		embedder:    embedder,
		vectorStore: vectorStore,
		queue:       make(chan IndexTask, queueSize),
		stop:        make(chan struct{}),
		shutdown:    make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
	}
	i.wg.Add(1)
	go i.worker()
	return i
}

// Enqueue adds a task to the background queue.  It never blocks; if the
// queue is full the task is dropped and an error is returned.
func (i *Indexer) Enqueue(task IndexTask) error {
	select {
	case i.queue <- task:
		return nil
	default:
		i.dropped.Add(1)
		log.Printf("async: indexer queue full, task dropped for chapter %s", task.ChapterID)
		return fmt.Errorf("async: indexer queue full, task dropped for chapter %s", task.ChapterID)
	}
}

// BatchEnqueue adds multiple tasks to the background queue.  It never blocks;
// individual tasks that do not fit are dropped and the function returns a
// combined error listing all dropped chapter IDs.
func (i *Indexer) BatchEnqueue(tasks []IndexTask) error {
	var dropped []string
	for _, task := range tasks {
		select {
		case i.queue <- task:
		default:
			i.dropped.Add(1)
			dropped = append(dropped, task.ChapterID)
			log.Printf("async: indexer queue full, task dropped for chapter %s", task.ChapterID)
		}
	}
	if len(dropped) > 0 {
		return fmt.Errorf("async: indexer queue full, dropped tasks for chapters: %s", strings.Join(dropped, ", "))
	}
	return nil
}

// Stats returns a snapshot of the current processing statistics.
func (i *Indexer) Stats() Stats {
	return Stats{
		Success: i.success.Load(),
		Failed:  i.failed.Load(),
		Dropped: i.dropped.Load(),
	}
}

// Stop signals the worker to exit and waits for it to finish.
// Remaining tasks in the queue are drained without processing.
func (i *Indexer) Stop() {
	i.cancel()
	close(i.stop)
	i.wg.Wait()
}

// Shutdown gracefully shuts down the indexer. It stops accepting new tasks,
// waits for the queue to drain and all in-flight tasks to complete, then
// returns the final statistics. The timeout parameter limits how long to wait.
func (i *Indexer) Shutdown(timeout time.Duration) Stats {
	i.cancel()
	close(i.stop)

	done := make(chan struct{})
	go func() {
		i.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(timeout):
		log.Printf("async: indexer shutdown timed out after %v", timeout)
	}

	return i.Stats()
}

func (i *Indexer) worker() {
	defer i.wg.Done()
	batch := make([]IndexTask, 0, batchSize)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		i.handleBatch(batch)
		batch = batch[:0]
	}

	for {
		select {
		case <-i.ctx.Done():
			flush()
			i.drain()
			return
		case <-i.stop:
			flush()
			i.drain()
			return
		case task, ok := <-i.queue:
			if !ok {
				flush()
				return
			}
			batch = append(batch, task)
			if len(batch) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// drain consumes remaining tasks without processing them.
// It must only be called once and only from the worker goroutine.
func (i *Indexer) drain() {
	for {
		select {
		case _, ok := <-i.queue:
			if !ok {
				return
			}
		default:
			return
		}
	}
}

func (i *Indexer) handleBatch(tasks []IndexTask) {
	if i.embedder == nil || i.vectorStore == nil {
		return
	}

	// Separate valid tasks
	valid := make([]IndexTask, 0, len(tasks))
	for _, t := range tasks {
		if t.ChapterID == "" || strings.TrimSpace(t.Summary) == "" {
			i.failed.Add(1)
			log.Printf("async: invalid task for chapter %q, skipping", t.ChapterID)
			continue
		}
		valid = append(valid, t)
	}
	if len(valid) == 0 {
		return
	}

	// Try batch embedding first; fall back to single encoding on failure.
	summaries := make([]string, len(valid))
	for i, t := range valid {
		summaries[i] = t.Summary
	}
	vecs, err := i.embedder.Encode(summaries)
	if err != nil {
		// Fallback: process one by one with retries.
		for _, t := range valid {
			i.handleWithRetry(t)
		}
		return
	}

	for idx, t := range valid {
		if idx >= len(vecs) {
			break
		}
		if err := i.vectorStore.Insert(t.ChapterID, vecs[idx]); err != nil {
			i.failed.Add(1)
			log.Printf("async: vector store insert failed for chapter %s: %v", t.ChapterID, err)
		} else {
			i.success.Add(1)
		}
	}
}

func (i *Indexer) handleWithRetry(task IndexTask) {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		vec, err := i.embedder.EncodeSingle(task.Summary)
		if err != nil {
			lastErr = err
			log.Printf("async: embedding failed for chapter %s (attempt %d/%d): %v", task.ChapterID, attempt+1, maxRetries, err)
			time.Sleep(retryDelayBase * time.Duration(attempt+1))
			continue
		}
		if err := i.vectorStore.Insert(task.ChapterID, vec); err != nil {
			lastErr = err
			log.Printf("async: vector store insert failed for chapter %s (attempt %d/%d): %v", task.ChapterID, attempt+1, maxRetries, err)
			time.Sleep(retryDelayBase * time.Duration(attempt+1))
			continue
		}
		i.success.Add(1)
		return
	}
	i.failed.Add(1)
	log.Printf("async: task permanently failed for chapter %s after %d attempts: %v", task.ChapterID, maxRetries, lastErr)
}

// ---------------------------------------------------------------------------
// Global default indexer (lazy initialised)
// ---------------------------------------------------------------------------

var (
	defaultIndexer     *Indexer
	defaultIndexerOnce sync.Once
)

// SetDefaultIndexer installs the package-level default indexer.
// Safe to call multiple times — the last call wins.
func SetDefaultIndexer(idx *Indexer) {
	defaultIndexer = idx
}

// DefaultIndexer returns the currently-installed default indexer.
func DefaultIndexer() *Indexer { return defaultIndexer }

// initDefaultIndexer is the implementation shared by async.go and this file.
func initDefaultIndexer(d *db.DB, embedder Embedder) (*Indexer, error) {
	if d == nil || d.DB == nil {
		return nil, fmt.Errorf("async: nil db")
	}
	store, err := newSQLiteVecStore(d.DB)
	if err != nil {
		return nil, fmt.Errorf("async: create vec store: %w", err)
	}
	idx := NewIndexer(embedder, store, 64)
	SetDefaultIndexer(idx)
	return idx, nil
}

// ---------------------------------------------------------------------------
// Minimal SQLite vector store (local to async to avoid rag import)
// ---------------------------------------------------------------------------

type sqliteVecStore struct{ db *sql.DB }

func newSQLiteVecStore(db *sql.DB) (*sqliteVecStore, error) {
	if db == nil {
		return nil, fmt.Errorf("async: newSQLiteVecStore: db is nil")
	}
	const createTable = `CREATE TABLE IF NOT EXISTS vec_chapters (
		chapter_id TEXT PRIMARY KEY,
		embedding BLOB NOT NULL
	);`
	if _, err := db.Exec(createTable); err != nil {
		return nil, fmt.Errorf("async: create vec_chapters table: %w", err)
	}
	return &sqliteVecStore{db: db}, nil
}

func (s *sqliteVecStore) Insert(chapterID string, embedding []float32) error {
	if chapterID == "" || len(embedding) == 0 {
		return fmt.Errorf("async: Insert: invalid args")
	}
	blob := encodeFloat32s(embedding)
	const q = `INSERT INTO vec_chapters (chapter_id, embedding) VALUES (?, ?)
			   ON CONFLICT(chapter_id) DO UPDATE SET embedding = excluded.embedding;`
	_, err := s.db.Exec(q, chapterID, blob)
	return err
}

func encodeFloat32s(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}
