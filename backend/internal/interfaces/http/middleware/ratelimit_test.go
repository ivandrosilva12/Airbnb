package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRateLimiter_BurstThenThrottleThenRefill(t *testing.T) {
	l := newRateLimiter(10, 3) // 10 tokens/sec, burst 3

	// The burst of 3 is allowed immediately.
	for i := 0; i < 3; i++ {
		if !l.allow("1.2.3.4") {
			t.Fatalf("request %d within burst should be allowed", i+1)
		}
	}
	// The 4th is throttled.
	if l.allow("1.2.3.4") {
		t.Fatal("4th request should be throttled")
	}

	// A different client has its own bucket.
	if !l.allow("5.6.7.8") {
		t.Fatal("a different IP should not be throttled")
	}

	// After enough time to refill (~0.2s for 2 tokens at 10/s), it allows again.
	b := l.buckets["1.2.3.4"]
	b.last = time.Now().Add(-300 * time.Millisecond)
	if !l.allow("1.2.3.4") {
		t.Fatal("request after refill window should be allowed")
	}
}

func TestRateLimit_Middleware429AndCallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rejects := 0
	r := gin.New()
	r.GET("/x", RateLimit(1, 1, func() { rejects++ }), func(c *gin.Context) { c.Status(http.StatusOK) })

	call := func() int {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := call(); code != http.StatusOK {
		t.Fatalf("first request = %d, want 200", code)
	}
	if code := call(); code != http.StatusTooManyRequests {
		t.Fatalf("second request = %d, want 429", code)
	}
	if rejects != 1 {
		t.Fatalf("onReject called %d times, want 1", rejects)
	}
}
