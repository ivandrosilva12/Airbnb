package cache

import (
	"context"
	"sync"
	"time"

	"github.com/airhost/backend/internal/application/port"
)

// Memory is an in-memory TTL-aware port.Cache. Goroutine-safe under
// the per-entry expiry check; the map mutex protects layout, the
// expiry comparison happens on read so a stale entry is detected
// without a background sweeper goroutine (which would race on
// shutdown).
//
// Used in:
//   - the e2e test harness, where the noop default would defeat
//     cache-hit assertions
//   - single-node deployments without Redis
//
// NOT a fit for multi-node deployments — each replica has its own
// store, so a cache invalidation on one node doesn't reach the
// others. Use the Redis adapter in that case.
type Memory struct {
	mu sync.RWMutex
	m  map[string]memoryEntry
}

type memoryEntry struct {
	bytes  []byte
	expiry time.Time // zero = never expires (unused; Set requires a TTL)
}

// NewMemory builds an empty Memory cache.
func NewMemory() *Memory {
	return &Memory{m: map[string]memoryEntry{}}
}

var _ port.Cache = (*Memory)(nil)

// Get returns the cached value if present AND unexpired. An expired
// entry is treated as a miss and lazily evicted on the next write
// (we don't pay the write-lock cost on the read path).
func (c *Memory) Get(_ context.Context, key string) ([]byte, bool, error) {
	c.mu.RLock()
	e, ok := c.m[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false, nil
	}
	if !e.expiry.IsZero() && time.Now().After(e.expiry) {
		return nil, false, nil
	}
	// Return a copy so a caller mutating the slice can't corrupt the
	// stored value. Cheap (these are small JSON payloads) and rules
	// out a subtle aliasing bug.
	out := make([]byte, len(e.bytes))
	copy(out, e.bytes)
	return out, true, nil
}

// Set stores value under key with the given ttl. ttl <= 0 falls
// back to 1 minute so a programmer typo doesn't become a leak.
func (c *Memory) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = time.Minute
	}
	// Defensive copy so an external caller mutating the source slice
	// later cannot change what's cached.
	stored := make([]byte, len(value))
	copy(stored, value)
	c.mu.Lock()
	c.m[key] = memoryEntry{bytes: stored, expiry: time.Now().Add(ttl)}
	c.mu.Unlock()
	return nil
}

// Delete removes the listed keys; no-op for keys not present.
func (c *Memory) Delete(_ context.Context, keys ...string) error {
	c.mu.Lock()
	for _, k := range keys {
		delete(c.m, k)
	}
	c.mu.Unlock()
	return nil
}
