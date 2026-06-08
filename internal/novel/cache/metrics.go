package cache

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics holds cache performance metrics for a single cache instance.
type Metrics struct {
	Hits      atomic.Int64
	Misses    atomic.Int64
	Evictions atomic.Int64
	// Latency buckets in nanoseconds: store count per bucket index.
	latencyBuckets [6]atomic.Int64 // <1ms, <5ms, <10ms, <25ms, <50ms, >=50ms
}

// RecordHit records a cache hit with the given latency.
func (m *Metrics) RecordHit(latency time.Duration) {
	m.Hits.Add(1)
	m.recordLatency(latency)
}

// RecordMiss records a cache miss with the given latency.
func (m *Metrics) RecordMiss(latency time.Duration) {
	m.Misses.Add(1)
	m.recordLatency(latency)
}

// RecordEviction records a cache eviction.
func (m *Metrics) RecordEviction() {
	m.Evictions.Add(1)
}

func (m *Metrics) recordLatency(latency time.Duration) {
	ns := latency.Nanoseconds()
	switch {
	case ns < 1_000_000: // < 1ms
		m.latencyBuckets[0].Add(1)
	case ns < 5_000_000: // < 5ms
		m.latencyBuckets[1].Add(1)
	case ns < 10_000_000: // < 10ms
		m.latencyBuckets[2].Add(1)
	case ns < 25_000_000: // < 25ms
		m.latencyBuckets[3].Add(1)
	case ns < 50_000_000: // < 50ms
		m.latencyBuckets[4].Add(1)
	default: // >= 50ms
		m.latencyBuckets[5].Add(1)
	}
}

// LatencySnapshot returns a snapshot of latency distribution.
func (m *Metrics) LatencySnapshot() []int64 {
	var s [6]int64
	for i := range m.latencyBuckets {
		s[i] = m.latencyBuckets[i].Load()
	}
	return s[:]
}

// Snapshot returns a MetricsSnapshot with current values.
func (m *Metrics) Snapshot() MetricsSnapshot {
	hits := m.Hits.Load()
	misses := m.Misses.Load()
	total := hits + misses
	hitRate := float64(0)
	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}
	return MetricsSnapshot{
		Hits:       hits,
		Misses:     misses,
		HitRate:    hitRate,
		Evictions:  m.Evictions.Load(),
		Latencies:  m.LatencySnapshot(),
	}
}

// MetricsSnapshot is a point-in-time copy of Metrics.
type MetricsSnapshot struct {
	Hits      int64
	Misses    int64
	HitRate   float64
	Evictions int64
	Latencies []int64
}

// globalRegistry is the process-level cache metrics registry.
type globalRegistry struct {
	mu      sync.RWMutex
	caches  map[string]any // value is typically *MemoryCache[T]
	metrics map[string]*Metrics
}

var (
	registryInstance = &globalRegistry{
		caches:  make(map[string]any),
		metrics: make(map[string]*Metrics),
	}
	registryMu sync.Mutex
)

// RegisterGlobal registers a cache with the global registry and returns its Metrics.
// The cache can be any type; metrics are tracked separately.
func RegisterGlobal(name string, cache any) *Metrics {
	registryMu.Lock()
	defer registryMu.Unlock()

	registryInstance.mu.Lock()
	defer registryInstance.mu.Unlock()

	registryInstance.caches[name] = cache
	m := &Metrics{}
	registryInstance.metrics[name] = m
	return m
}

// GetGlobal retrieves a registered cache by name.
func GetGlobal(name string) (any, bool) {
	registryInstance.mu.RLock()
	defer registryInstance.mu.RUnlock()
	c, ok := registryInstance.caches[name]
	return c, ok
}

// GetGlobalMetrics retrieves the Metrics for a registered cache by name.
func GetGlobalMetrics(name string) (*Metrics, bool) {
	registryInstance.mu.RLock()
	defer registryInstance.mu.RUnlock()
	m, ok := registryInstance.metrics[name]
	return m, ok
}

// UnregisterGlobal removes a cache from the global registry.
func UnregisterGlobal(name string) {
	registryMu.Lock()
	defer registryMu.Unlock()

	registryInstance.mu.Lock()
	defer registryInstance.mu.Unlock()
	delete(registryInstance.caches, name)
	delete(registryInstance.metrics, name)
}

// GlobalNames returns all registered cache names.
func GlobalNames() []string {
	registryInstance.mu.RLock()
	defer registryInstance.mu.RUnlock()
	names := make([]string, 0, len(registryInstance.caches))
	for name := range registryInstance.caches {
		names = append(names, name)
	}
	return names
}

// GlobalSnapshot returns a snapshot of all registered metrics.
func GlobalSnapshot() map[string]MetricsSnapshot {
	registryInstance.mu.RLock()
	defer registryInstance.mu.RUnlock()

	result := make(map[string]MetricsSnapshot, len(registryInstance.metrics))
	for name, m := range registryInstance.metrics {
		result[name] = m.Snapshot()
	}
	return result
}

// GlobalReport returns a human-readable report of all registered cache metrics.
func GlobalReport() string {
	snap := GlobalSnapshot()
	if len(snap) == 0 {
		return "No caches registered."
	}

	var totalHits, totalMisses int64
	report := strings.Builder{}
	report.WriteString("Cache Metrics Report:\n")
	for name, s := range snap {
		report.WriteString(fmt.Sprintf("  %s: hits=%d misses=%d hitRate=%.2f%% evictions=%d\n",
			name, s.Hits, s.Misses, s.HitRate*100, s.Evictions))
		totalHits += s.Hits
		totalMisses += s.Misses
	}

	total := totalHits + totalMisses
	overallRate := float64(0)
	if total > 0 {
		overallRate = float64(totalHits) / float64(total)
	}
	report.WriteString(fmt.Sprintf("  Overall: hits=%d misses=%d hitRate=%.2f%%",
		totalHits, totalMisses, overallRate*100))
	return report.String()
}

// PrometheusExport returns metrics in a simple Prometheus-like text format.
// This is a lightweight implementation; for production use consider github.com/prometheus/client_golang.
func PrometheusExport() string {
	snap := GlobalSnapshot()
	if len(snap) == 0 {
		return ""
	}

	b := strings.Builder{}
	for name, s := range snap {
		labels := fmt.Sprintf(`cache="%s"`, name)
		b.WriteString(fmt.Sprintf("cache_hits_total{%s} %d\n", labels, s.Hits))
		b.WriteString(fmt.Sprintf("cache_misses_total{%s} %d\n", labels, s.Misses))
		b.WriteString(fmt.Sprintf("cache_evictions_total{%s} %d\n", labels, s.Evictions))
		b.WriteString(fmt.Sprintf("cache_hit_rate{%s} %.6f\n", labels, s.HitRate))

		buckets := []string{"1ms", "5ms", "10ms", "25ms", "50ms", "+Inf"}
		for i, count := range s.Latencies {
			b.WriteString(fmt.Sprintf("cache_latency_bucket{%s,le=\"%s\"} %d\n", labels, buckets[i], count))
		}
	}
	return b.String()
}
