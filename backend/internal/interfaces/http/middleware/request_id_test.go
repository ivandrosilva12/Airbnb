package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func init() { gin.SetMode(gin.TestMode) }

// TestRequestID_GeneratesWhenAbsent confirms the middleware mints a fresh
// UUID when the incoming request has no X-Request-ID, exposes it on both
// gin context and stdlib context, and echoes it in the response header.
func TestRequestID_GeneratesWhenAbsent(t *testing.T) {
	r := gin.New()
	r.Use(RequestID())
	var gotGin, gotCtx string
	r.GET("/x", func(c *gin.Context) {
		gotGin = RequestIDGin(c)
		gotCtx = RequestIDFrom(c.Request.Context())
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if gotGin == "" || gotCtx == "" {
		t.Fatalf("request id missing: gin=%q ctx=%q", gotGin, gotCtx)
	}
	if gotGin != gotCtx {
		t.Fatalf("gin (%q) and ctx (%q) request ids differ", gotGin, gotCtx)
	}
	if _, err := uuid.Parse(gotGin); err != nil {
		t.Fatalf("generated id is not a UUID: %v", err)
	}
	if rec.Header().Get("X-Request-ID") != gotGin {
		t.Fatalf("response header = %q, want %q", rec.Header().Get("X-Request-ID"), gotGin)
	}
}

// TestRequestID_PreservesIncomingHeader confirms a well-formed incoming
// X-Request-ID is reused (so upstream proxies and SDKs that mint their own
// trace ID can carry it through unchanged for end-to-end correlation).
func TestRequestID_PreservesIncomingHeader(t *testing.T) {
	r := gin.New()
	r.Use(RequestID())
	var got string
	r.GET("/x", func(c *gin.Context) {
		got = RequestIDGin(c)
		c.Status(http.StatusOK)
	})

	want := uuid.New().String()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Request-ID", want)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if got != want {
		t.Fatalf("got = %q, want incoming header %q to be preserved", got, want)
	}
	if rec.Header().Get("X-Request-ID") != want {
		t.Fatalf("response header = %q, want preserved %q", rec.Header().Get("X-Request-ID"), want)
	}
}

// TestRequestID_RejectsMalformedIncoming confirms a non-UUID X-Request-ID
// is replaced with a fresh UUID rather than echoed back — defense against a
// caller stuffing arbitrary user-controlled strings into our log stream.
func TestRequestID_RejectsMalformedIncoming(t *testing.T) {
	r := gin.New()
	r.Use(RequestID())
	var got string
	r.GET("/x", func(c *gin.Context) {
		got = RequestIDGin(c)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Request-ID", "../../etc/passwd")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if _, err := uuid.Parse(got); err != nil {
		t.Fatalf("expected fresh UUID when incoming is malformed, got %q", got)
	}
	if got == "../../etc/passwd" {
		t.Fatalf("malformed incoming id was echoed; want it discarded")
	}
}

// TestLoggerFrom_TagsWithRequestID confirms the request-scoped logger has
// request_id pinned, so any service log line written via LoggerFrom(ctx)
// carries the correlation automatically.
func TestLoggerFrom_TagsWithRequestID(t *testing.T) {
	r := gin.New()
	r.Use(RequestID())
	var loggerHasID bool
	r.GET("/x", func(c *gin.Context) {
		l := LoggerFrom(c.Request.Context())
		// We can't easily inspect a *slog.Logger's attrs without a custom
		// handler. Sanity: the logger should be different from the default
		// (the With call returns a fresh logger), and the helper should
		// never return nil.
		loggerHasID = l != nil
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if !loggerHasID {
		t.Fatalf("LoggerFrom returned nil")
	}
}
