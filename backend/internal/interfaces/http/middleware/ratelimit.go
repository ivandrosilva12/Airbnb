package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// RateLimit returns a middleware that throttles requests per client IP using an
// in-memory token bucket: each IP starts with `burst` tokens, refilled at `rps`
// tokens/second, and a request that finds no token gets HTTP 429.
//
// It suits a single instance (e.g. guarding the unauthenticated webhook route);
// a multi-instance deployment would back the same limiter with a shared store.
func RateLimit(rps float64, burst int) gin.HandlerFunc {
	lim := newRateLimiter(rps, float64(burst))
	return func(c *gin.Context) {
		if !lim.allow(c.ClientIP()) {
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
