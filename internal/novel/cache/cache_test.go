package cache

import (
	"sync"
	"testing"
	"time"
)

func TestMemoryCache_GetSet(t *testing.T) {
	c := NewMemoryCache[any](0, 0)

	// Miss
	val, hit := c.Get("key1")
	if hit {
		t.Fatal("expected cache miss")
	}
	if val != nil {
		t.Fatalf("expected nil, got %v", val)
	}

	// Set
	c.Set("key1", "value1")
	val, hit = c.Get("key1")
	if !hit {
		t.Fatal("expected cache hit")
	}
	if val != "value1" {
		t.Fatalf("expected value1, got %v", val)
	}

	// Stats
	stats := c.Stats()
	if stats.Hits != 1 {
		t.Fatalf("expected 1 hit, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Fatalf("expected 1 miss, got %d", stats.Misses)
	}
	if stats.HitRate != 0.5 {
		t.Fatalf("expected hit rate 0.5, got %f", stats.HitRate)
	}
}

func TestMemoryCache_Delete(t *testing.T) {
	c := NewMemoryCache[string](0, 0)
	c.Set("key1", "value1")

	_, hit := c.Get("key1")
	if !hit {
		t.Fatal("expected cache hit before delete")
	}

	c.Delete("key1")
	_, hit = c.Get("key1")
	if hit {
		t.Fatal("expected cache miss after delete")
	}
}

func TestMemoryCache_TTLExpiration(t *testing.T) {
	c := NewMemoryCache[string](50*time.Millisecond, 0)
	c.Set("key1", "value1")

	val, hit := c.Get("key1")
	if !hit {
		t.Fatal("expected cache hit before TTL expiration")
	}
	if val != "value1" {
		t.Fatalf("expected value1, got %v", val)
	}

	time.Sleep(100 * time.Millisecond)

	_, hit = c.Get("key1")
	if hit {
		t.Fatal("expected cache miss after TTL expiration")
	}
}

func TestMemoryCache_ConcurrentAccess(t *testing.T) {
	c := NewMemoryCache[int](0, 0)
	const workers = 100
	const iterations = 1000

	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := string(rune('a' + (id % 26)))
				c.Set(key, id)
				c.Get(key)
				if j%10 == 0 {
					c.Delete(key)
				}
			}
		}(i)
	}

	wg.Wait()

	stats := c.Stats()
	if stats.Hits+stats.Misses != int64(workers*iterations) {
		t.Fatalf("expected %d total accesses, got %d", workers*iterations, stats.Hits+stats.Misses)
	}
}

func TestMemoryCache_Reset(t *testing.T) {
	c := NewMemoryCache[string](0, 0)
	c.Set("key1", "value1")
	c.Get("key1")

	c.Reset()

	_, hit := c.Get("key1")
	if hit {
		t.Fatal("expected cache miss after reset")
	}

	stats := c.Stats()
	if stats.Hits != 0 || stats.Misses != 1 {
		t.Fatalf("expected hits=0 misses=1 after reset, got hits=%d misses=%d", stats.Hits, stats.Misses)
	}
}

func TestMemoryCache_String(t *testing.T) {
	c := NewMemoryCache[string](0, 0)
	c.Set("key1", "value1")
	c.Get("key1")
	c.Get("key2") // miss

	s := c.String()
	expected := "MemoryCache(hits=1 misses=1 hitRate=50.00% evictions=0 size=1)"
	if s != expected {
		t.Fatalf("expected %q, got %q", expected, s)
	}
}
