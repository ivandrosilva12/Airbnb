package port

import (
	"context"
	"errors"
	"time"
)

// ErrCacheMiss is the sentinel Cache.Get returns when the key is
// absent. The HTTP layer treats it as "compute and store" — never
// as a hard error. Distinct value (not io.EOF or similar) so a
// linter on errors.Is paths catches typos.
var ErrCacheMiss = errors.New("cache: miss")

// Cache is the port the application layer uses to memoise read-heavy
// responses (S53). Implementations: noop (default — every Get is a
// miss), in-memory TTL store (used in tests + single-node deployments),
// and Redis (multi-node / production).
//
// The interface is deliberately byte-oriented so callers serialise
// once and store the wire bytes — a per-call JSON marshal would defeat
// the purpose of caching at all. Higher-level helpers in handlers
// wrap Get/Set with the right encoding.
type Cache interface {
	// Get returns the cached bytes and true on hit. On a clean miss
	// returns (nil, false, nil). Returns (nil, false, err) only for
	// real backend failures (a Redis outage); the handler treats those
	// as misses too — caching must never break the request.
	Get(ctx context.Context, key string) ([]byte, bool, error)
	// Set stores value under key with the given TTL. A non-positive
	// TTL is a programmer error; implementations may use a sane default
	// (1 minute) rather than store-forever.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	// Delete removes one or more keys. Idempotent — deleting a missing
	// key is not an error. Used on mutation paths to evict the read
	// cache for the affected aggregate.
	Delete(ctx context.Context, keys ...string) error
}
