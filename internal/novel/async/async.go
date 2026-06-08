// Package async provides simple background task utilities for the novel
// pipeline.  It is intentionally minimal: a single goroutine per indexer
// that drains a buffered channel.
//
// The package entry point is InitDefaultIndexer, which wires up the
// global default indexer used by tools such as chapter_write.
package async

import (
	"reasonix/internal/novel/db"
)

// InitDefaultIndexer creates a new Indexer from the given DB and
// embedder, registers it as the package default, and returns it.
// If the default indexer is already set this overwrites it (last call wins).
//
// Typical usage during application boot:
//
//	idx, err := async.InitDefaultIndexer(db, embedder)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer idx.Stop()
func InitDefaultIndexer(d *db.DB, embedder Embedder) (*Indexer, error) {
	return initDefaultIndexer(d, embedder)
}
