package cache

import (
	"context"
	"errors"
	"time"

	"github.com/airhost/backend/internal/application/port"
	"github.com/redis/go-redis/v9"
)

// Redis is a go-redis-backed port.Cache. Suitable for multi-node
// production: every replica points at the same instance/cluster, so
// a write on one node and an invalidation are seen by every reader.
//
// Backend errors are returned with the standard (nil, false, err)
// shape — handlers treat them as misses, so a Redis outage degrades
// gracefully into "everything is uncached" rather than 5xx-ing the
// API surface.
type Redis struct {
	client *redis.Client
}

// NewRedis wires a client from a redis://host:port URL. Ping is NOT
// performed here — the rest of the boot does enough remote work
// already; an unreachable Redis manifests as Get/Set errors at
// runtime, which the handler treats as cache misses.
func NewRedis(url string) (*Redis, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	return &Redis{client: redis.NewClient(opts)}, nil
}

// NewRedisFromClient lets tests inject a pre-built client (e.g. a
// miniredis-based test server).
func NewRedisFromClient(c *redis.Client) *Redis { return &Redis{client: c} }

var _ port.Cache = (*Redis)(nil)

// Get returns the cached bytes. A clean miss is (nil, false, nil);
// only real backend errors return non-nil err.
func (r *Redis) Get(ctx context.Context, key string) ([]byte, bool, error) {
	b, err := r.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return b, true, nil
}

// Set stores the value under key with the given ttl. A non-positive
// ttl falls back to 1 minute (matches Memory adapter).
func (r *Redis) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = time.Minute
	}
	return r.client.Set(ctx, key, value, ttl).Err()
}

// Delete removes the listed keys. No-op when keys is empty; Redis
// DEL on a missing key is idempotent.
func (r *Redis) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return r.client.Del(ctx, keys...).Err()
}

// Close releases the underlying connection pool. Composition root
// MUST defer Close on shutdown.
func (r *Redis) Close() error { return r.client.Close() }
