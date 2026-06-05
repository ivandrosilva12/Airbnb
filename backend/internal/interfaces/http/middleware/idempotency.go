package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/airhost/backend/internal/domain/idempotency"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// IdempotencyHeader is the RFC-style header clients set to dedupe a
// mutation across retries (S160). Opt-in: an absent header means the
// middleware is a no-op and the request falls through to the handler.
const IdempotencyHeader = "Idempotency-Key"

// maxIdempotencyKeyLen caps the header length so a misbehaving client
// cannot dump unbounded data into the storage column. 255 is generous
// for a UUID/ULID/hex token (the typical clients) while still putting
// a hard ceiling on the column footprint.
const maxIdempotencyKeyLen = 255

// Idempotency returns a middleware that implements RFC-style request
// idempotency on mutating verbs (POST/PATCH/PUT/DELETE). It is a
// no-op for any other method, and for any request without an
// Idempotency-Key header — the feature is opt-in by the client.
//
// On a hit (same userID + key + method + path + body_hash) the
// captured (status, content-type, body) is replayed verbatim, so a
// mobile retry on a flaky network sees the same response the original
// write produced and the underlying state machine is touched exactly
// once.
//
// On a hit with a different (method, path, body_hash) the middleware
// returns 409 Conflict — the client recycled the key for a different
// request, and we will not let one mutation's response leak into
// another's reply.
//
// On a miss the middleware wraps c.Writer to capture the response,
// runs the handler, and on completion stores the captured response if
// the status is < 500. Server errors are deliberately NOT stored so
// the client can retry cleanly after we recover.
//
// The middleware MUST sit after the auth middleware (it reads
// CurrentUser to scope the namespace) and BEFORE the handler.
func Idempotency(repo idempotency.Repository) gin.HandlerFunc {
	if repo == nil {
		// Defensive: a nil repo would NPE on the hot path. Fall back
		// to a transparent pass-through so the API stays up even if
		// the composition root forgot to wire one.
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		// Scope: mutating verbs only. Reads are naturally idempotent
		// and we don't want to burn storage on the entire GET surface.
		if !isMutatingMethod(c.Request.Method) {
			c.Next()
			return
		}

		key := c.GetHeader(IdempotencyHeader)
		if key == "" {
			c.Next()
			return
		}
		if len(key) > maxIdempotencyKeyLen {
			response.FailMessage(c, http.StatusBadRequest, "Idempotency-Key exceeds maximum length")
			return
		}

		u, ok := CurrentUser(c)
		if !ok || u == nil {
			// No authenticated caller means we cannot scope the
			// namespace. Auth middleware should have rejected the
			// request already; if it didn't, just pass through and
			// let the handler 401 on its own — we never want this
			// middleware to be the surface that says "auth required".
			c.Next()
			return
		}

		// Read the body in one pass so we can both hash it AND replay
		// it to the handler downstream. The hash lets us tell a true
		// retry (same key, same body) from a key reuse (same key,
		// different body) and answer the latter with 409.
		bodyBytes, err := readAndRestoreBody(c)
		if err != nil {
			response.FailMessage(c, http.StatusBadRequest, "could not read request body")
			return
		}
		sum := sha256.Sum256(bodyBytes)
		bodyHash := sum[:]

		// Lookup phase: a hit on (user, key) leads us into one of two
		// branches — replay or conflict. A miss falls through to the
		// store-then-pass branch below.
		existing, err := repo.Get(c.Request.Context(), u.ID, key)
		if err == nil && existing != nil {
			if existing.Method == c.Request.Method &&
				existing.Path == c.Request.URL.Path &&
				bytes.Equal(existing.BodyHash, bodyHash) {
				// Pure retry → replay the captured response. The
				// stored content-type is replayed too so the client
				// gets exactly the same envelope.
				if existing.ResponseContentType != "" {
					c.Writer.Header().Set("Content-Type", existing.ResponseContentType)
				}
				c.Writer.Header().Set("Idempotent-Replayed", "true")
				c.Writer.WriteHeader(existing.StatusCode)
				if len(existing.ResponseBody) > 0 {
					_, _ = c.Writer.Write(existing.ResponseBody)
				}
				c.Abort()
				return
			}
			// Same key, different request → key recycled by mistake.
			response.FailMessage(c, http.StatusConflict, "Idempotency-Key already used for a different request")
			return
		}
		if err != nil && !errors.Is(err, shared.ErrNotFound) {
			// Storage failure on the lookup path is logged but not
			// fatal — the worst case is a duplicate write, not a
			// dropped one, so we'd rather serve the request than
			// refuse it on a transient repo blip.
			slog.Default().Warn("idempotency: lookup failed", "err", err, "user_id", u.ID, "key", key)
		}

		// Miss path: capture the response, run the handler, then
		// store on success.
		buf := &bytes.Buffer{}
		captured := &captureWriter{ResponseWriter: c.Writer, body: buf}
		c.Writer = captured

		c.Next()

		status := captured.statusCode
		if status == 0 {
			// Gin defaults to 200 when no explicit Status was set.
			status = http.StatusOK
		}
		// Server errors (5xx) are NOT cached: the client should be
		// able to retry cleanly once we've recovered. Anything < 500
		// (success OR client error) IS cached — a 4xx response is the
		// authoritative answer for "this request, repeated".
		if status >= 500 {
			return
		}

		rec, err := idempotency.New(
			key, u.ID,
			c.Request.Method, c.Request.URL.Path,
			bodyHash, status, buf.Bytes(),
			captured.Header().Get("Content-Type"),
		)
		if err != nil {
			// A validation error here means the inputs we just
			// captured are malformed — log and move on rather than
			// disturbing the response the handler already sent.
			slog.Default().Warn("idempotency: record build failed", "err", err, "user_id", u.ID, "key", key)
			return
		}
		// Use a fresh, short-lived context so a client disconnect
		// during the write does not also cancel the persistence.
		// 5 seconds is plenty for an INSERT.
		putCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := repo.Put(putCtx, rec); err != nil {
			slog.Default().Warn("idempotency: store failed", "err", err, "user_id", u.ID, "key", key)
		}
	}
}

// isMutatingMethod reports whether m is a verb that can change server
// state. The middleware only acts on these; GET / HEAD / OPTIONS fall
// straight through.
func isMutatingMethod(m string) bool {
	switch m {
	case http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete:
		return true
	}
	return false
}

// readAndRestoreBody drains c.Request.Body into a byte slice and
// re-installs a NopCloser around the bytes so the handler still sees
// the original payload. A nil body is treated as empty bytes — the
// hash of "no body" is a stable, well-defined value.
func readAndRestoreBody(c *gin.Context) ([]byte, error) {
	if c.Request.Body == nil {
		return []byte{}, nil
	}
	b, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, err
	}
	_ = c.Request.Body.Close()
	c.Request.Body = io.NopCloser(bytes.NewReader(b))
	return b, nil
}

// captureWriter wraps gin's ResponseWriter, mirroring every write to
// an internal buffer so the middleware can record the exact bytes the
// handler emitted. Implements gin.ResponseWriter by embedding the
// underlying writer; the only overridden methods are the ones that
// touch the body or the status.
type captureWriter struct {
	gin.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

func (w *captureWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *captureWriter) Write(p []byte) (int, error) {
	if w.statusCode == 0 {
		// Gin implicitly sets 200 on the first Write; mirror that
		// here so we capture it for the replay.
		w.statusCode = http.StatusOK
	}
	w.body.Write(p)
	return w.ResponseWriter.Write(p)
}

func (w *captureWriter) WriteString(s string) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}
