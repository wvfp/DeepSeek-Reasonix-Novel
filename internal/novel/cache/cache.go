package cache

import (
	"fmt"
	"sync"
	"time"
)

// CacheStats holds cache performance statistics.
type CacheStats struct {
	Hits       int64
	Misses     int64
	HitRate    float64
	Evictions  int64
	Size       int
}

// Cache is a generic in-memory cache interface.
type Cache[T any] interface {
	Get(key string) (value T, hit bool)
	Set(key string, value T)
	Delete(key string)
	Stats() CacheStats
}

// entry wraps a cached value with an optional expiration time.
type entry[T any] struct {
	value      T
	expiresAt  time.Time
	hasExpiry  bool
	createdAt  time.Time
}

// MemoryCache is a thread-safe in-memory cache with optional TTL support,
// automatic background cleanup, and max size eviction.
type MemoryCache[T any] struct {
	mu         sync.RWMutex
	items      map[string]*entry[T]
	ttl        time.Duration
	maxSize    int
	hits       int64
	misses     int64
	evictions  int64
	stopCh     chan struct{}
	stopOnce   sync.Once
}

// NewMemoryCache creates a new MemoryCache.
// If ttl > 0, entries expire after the given duration.
// If maxSize > 0, oldest entries are evicted when the limit is reached.
func NewMemoryCache[T any](ttl time.Duration, maxSize int) *MemoryCache[T] {
	c := &MemoryCache[T]{
		items:   make(map[string]*entry[T]),
		ttl:     ttl,
		maxSize: maxSize,
		stopCh:  make(chan struct{}),
	}

	if ttl > 0 {
		go c.cleanupLoop()
	}

	return c
}

// cleanupLoop runs a background goroutine that periodically removes expired entries.
func (c *MemoryCache[T]) cleanupLoop() {
	ticker := time.NewTicker(c.ttl / 2)
	if c.ttl < time.Second {
		ticker.Reset(time.Second)
	}
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.evictExpired()
		case <-c.stopCh:
			return
		}
	}
}

// evictExpired removes all expired entries.
func (c *MemoryCache[T]) evictExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, e := range c.items {
		if e.hasExpiry && now.After(e.expiresAt) {
			delete(c.items, key)
			c.evictions++
		}
	}
}

// evictOldest removes the oldest entry (by createdAt) when over capacity.
func (c *MemoryCache[T]) evictOldest() {
	if c.maxSize <= 0 || len(c.items) <= c.maxSize {
		return
	}

	var oldestKey string
	var oldestTime time.Time
	first := true
	for key, e := range c.items {
		if first || e.createdAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = e.createdAt
			first = false
		}
	}
	if oldestKey != "" {
		delete(c.items, oldestKey)
		c.evictions++
	}
}

// Get retrieves a value by key.
// It returns the value and true if the key exists and has not expired.
func (c *MemoryCache[T]) Get(key string) (T, bool) {
	c.mu.RLock()
	e, ok := c.items[key]
	c.mu.RUnlock()

	var zero T
	if !ok {
		c.mu.Lock()
		c.misses++
		c.mu.Unlock()
		return zero, false
	}

	if e.hasExpiry && time.Now().After(e.expiresAt) {
		c.mu.Lock()
		// Double-check after acquiring write lock.
		if ee, stillOk := c.items[key]; stillOk && ee.hasExpiry && time.Now().After(ee.expiresAt) {
			delete(c.items, key)
			c.evictions++
		}
		c.misses++
		c.mu.Unlock()
		return zero, false
	}

	c.mu.Lock()
	c.hits++
	c.mu.Unlock()
	return e.value, true
}

// Set stores a value under the given key.
// If TTL is configured, the entry will expire after that duration.
// If maxSize is configured, oldest entries may be evicted.
func (c *MemoryCache[T]) Set(key string, value T) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e := &entry[T]{
		value:     value,
		createdAt: time.Now(),
	}
	if c.ttl > 0 {
		e.hasExpiry = true
		e.expiresAt = time.Now().Add(c.ttl)
	}
	c.items[key] = e

	if c.maxSize > 0 && len(c.items) > c.maxSize {
		c.evictOldest()
	}
}

// Delete removes a key from the cache.
func (c *MemoryCache[T]) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

// Stats returns a snapshot of cache statistics.
func (c *MemoryCache[T]) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := c.hits + c.misses
	hitRate := float64(0)
	if total > 0 {
		hitRate = float64(c.hits) / float64(total)
	}
	return CacheStats{
		Hits:      c.hits,
		Misses:    c.misses,
		HitRate:   hitRate,
		Evictions: c.evictions,
		Size:      len(c.items),
	}
}

// Reset clears all items and statistics.
func (c *MemoryCache[T]) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*entry[T])
	c.hits = 0
	c.misses = 0
	c.evictions = 0
}

// Stop stops the background cleanup goroutine.
func (c *MemoryCache[T]) Stop() {
	c.stopOnce.Do(func() {
		close(c.stopCh)
	})
}

// String returns a human-readable summary of the cache state.
func (c *MemoryCache[T]) String() string {
	stats := c.Stats()
	return fmt.Sprintf("MemoryCache(hits=%d misses=%d hitRate=%.2f%% evictions=%d size=%d)",
		stats.Hits, stats.Misses, stats.HitRate*100, stats.Evictions, stats.Size)
}
