package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/airhost/backend/internal/domain/user"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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

// TestRateLimit_AuthenticatedUsersHaveIndependentBuckets exercises the S159
// fix: two distinct authenticated users issuing requests from the *same*
// client IP must not share a quota. Previously the limiter keyed solely on
// ClientIP(), which made corporate NAT a false-positive minefield (one
// noisy neighbor throttled everyone behind the gateway).
func TestRateLimit_AuthenticatedUsersHaveIndependentBuckets(t *testing.T) {
	gin.SetMode(gin.TestMode)

	userA := &user.User{ID: uuid.New()}
	userB := &user.User{ID: uuid.New()}

	// A burst of 1 means the second request from the same key is throttled.
	// We install a tiny middleware in front of RateLimit that injects the
	// chosen user onto the context, mirroring what auth.go does in prod.
	var active *user.User
	r := gin.New()
	r.GET("/x",
		func(c *gin.Context) {
			if active != nil {
				SetCurrentUser(c, active)
			}
			c.Next()
		},
		RateLimit(1, 1, nil),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	call := func(u *user.User, ip string) int {
		active = u
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.RemoteAddr = ip + ":12345"
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	// Same IP, two different users — both should get their first request
	// through, since each has its own user-keyed bucket.
	if code := call(userA, "10.0.0.1"); code != http.StatusOK {
		t.Fatalf("userA first request = %d, want 200", code)
	}
	if code := call(userB, "10.0.0.1"); code != http.StatusOK {
		t.Fatalf("userB first request from same IP = %d, want 200 (independent bucket)", code)
	}

	// And each user is still individually rate-limited on their own bucket.
	if code := call(userA, "10.0.0.1"); code != http.StatusTooManyRequests {
		t.Fatalf("userA second request = %d, want 429", code)
	}
	if code := call(userB, "10.0.0.1"); code != http.StatusTooManyRequests {
		t.Fatalf("userB second request = %d, want 429", code)
	}
}

// TestRateLimit_AnonymousFallsBackToIP confirms the default KeyFunc still
// keys by IP when no user is attached to the context (public endpoints,
// pre-auth requests, the webhook route). Two distinct IPs get independent
// buckets, but two requests from the *same* anonymous IP share one.
func TestRateLimit_AnonymousFallsBackToIP(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.GET("/x",
		RateLimit(1, 1, nil),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	call := func(ip string) int {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.RemoteAddr = ip + ":12345"
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := call("10.0.0.1"); code != http.StatusOK {
		t.Fatalf("first anonymous request = %d, want 200", code)
	}
	if code := call("10.0.0.1"); code != http.StatusTooManyRequests {
		t.Fatalf("second anonymous request from same IP = %d, want 429", code)
	}
	if code := call("10.0.0.2"); code != http.StatusOK {
		t.Fatalf("first request from a different IP = %d, want 200 (independent bucket)", code)
	}
}

// TestRateLimit_UserAndIPNamespacesAreDisjoint guards against a subtle
// collision: if the key had been the bare user-id or bare IP, a user whose
// ID happened to look like an IP (or vice versa) could share a bucket with
// an unrelated anonymous caller. The "u:" / "ip:" prefixes keep them apart.
func TestRateLimit_UserAndIPNamespacesAreDisjoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	u := &user.User{ID: uuid.New()}

	var withUser bool
	r := gin.New()
	r.GET("/x",
		func(c *gin.Context) {
			if withUser {
				SetCurrentUser(c, u)
			}
			c.Next()
		},
		RateLimit(1, 1, nil),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	call := func(asUser bool) int {
		withUser = asUser
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	// Anonymous first request consumes the IP bucket.
	if code := call(false); code != http.StatusOK {
		t.Fatalf("anonymous first request = %d, want 200", code)
	}
	// Authenticated request from the same IP has its own user bucket and
	// should be allowed — the IP bucket being drained must not affect it.
	if code := call(true); code != http.StatusOK {
		t.Fatalf("authenticated request from same IP = %d, want 200 (disjoint namespace)", code)
	}
}
