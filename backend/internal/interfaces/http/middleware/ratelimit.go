package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// KeyFunc derives the bucket key from a request. The default prefers the
// authenticated user's stable ID (subject) and falls back to ClientIP() when
// no user is in scope (public endpoints, pre-auth, anonymous browsing). A
// custom KeyFunc can be supplied to RateLimitWithKey for special cases (e.g.
// the unauthenticated webhook route, which must stay IP-keyed even after
// upstream auth changes).
type KeyFunc func(c *gin.Context) string

// defaultKeyFunc keys per authenticated user when one is on the context,
// otherwise per client IP. The "u:" / "ip:" prefixes keep the two namespaces
// disjoint so an attacker controlling a user-id-shaped IP string cannot
// collide with someone else's bucket.
func defaultKeyFunc(c *gin.Context) string {
	if u, ok := CurrentUser(c); ok && u != nil {
		return "u:" + u.ID.String()
	}
	return "ip:" + c.ClientIP()
}

// RateLimit returns a middleware that throttles requests using an in-memory
// token bucket: each key starts with `burst` tokens, refilled at `rps`
// tokens/second, and a request that finds no token gets HTTP 429. onReject,
// when non-nil, is called once per rejection (e.g. to bump a metric).
//
// Keys are derived by defaultKeyFunc: the authenticated user's ID when one
// is on the context (so corporate NAT does not share a bucket and CGNAT
// cannot evade limits by rotating IPs), falling back to ClientIP() for
// anonymous traffic. Use RateLimitWithKey to override this (e.g. for routes
// that must remain strictly IP-keyed).
//
// It suits a single instance (e.g. guarding the unauthenticated webhook
// route); a multi-instance deployment would back the same limiter with a
// shared store.
func RateLimit(rps float64, burst int, onReject func()) gin.HandlerFunc {
	return RateLimitWithKey(rps, burst, onReject, defaultKeyFunc)
}

// RateLimitWithKey is RateLimit with an explicit key derivation function.
func RateLimitWithKey(rps float64, burst int, onReject func(), key KeyFunc) gin.HandlerFunc {
	if key == nil {
		key = defaultKeyFunc
	}
	lim := newRateLimiter(rps, float64(burst))
	return func(c *gin.Context) {
		if !lim.allow(key(c)) {
			if onReject != nil {
				onReject()
			}
			response.FailMessage(c, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		c.Next()
	}
}

type tokenBucket struct {
	tokens float64
	last   time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	rps     float64
	burst   float64
	lastGC  time.Time
}

func newRateLimiter(rps, burst float64) *rateLimiter {
	if rps <= 0 {
		rps = 1
	}
	if burst < 1 {
		burst = 1
	}
	return &rateLimiter{buckets: map[string]*tokenBucket{}, rps: rps, burst: burst, lastGC: time.Now()}
}

// allow consumes a token for key, refilling based on elapsed time.
func (l *rateLimiter) allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	l.gc(now)

	b, ok := l.buckets[key]
	if !ok {
		l.buckets[key] = &tokenBucket{tokens: l.burst - 1, last: now}
		return true
	}
	// Refill, then try to spend one token.
	b.tokens += now.Sub(b.last).Seconds() * l.rps
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// gc drops buckets idle long enough to have fully refilled, bounding memory.
// Caller must hold the lock.
func (l *rateLimiter) gc(now time.Time) {
	if now.Sub(l.lastGC) < time.Minute {
		return
	}
	l.lastGC = now
	idle := time.Duration(l.burst/l.rps*float64(time.Second)) + time.Minute
	for k, b := range l.buckets {
		if now.Sub(b.last) > idle {
			delete(l.buckets, k)
		}
	}
}
