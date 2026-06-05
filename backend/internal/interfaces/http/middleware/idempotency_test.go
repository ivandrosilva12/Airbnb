package middleware

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airhost/backend/internal/domain/idempotency"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// stubIdemRepo is a tiny in-test idempotency.Repository implementation
// so the middleware tests can assert exact Put / Get behaviour without
// pulling in the memory adapter (which is in a different package).
type stubIdemRepo struct {
	mu      sync.Mutex
	records map[string]*idempotency.Record
	puts    int
}

func newStubRepo() *stubIdemRepo {
	return &stubIdemRepo{records: map[string]*idempotency.Record{}}
}

func (s *stubIdemRepo) key(u uuid.UUID, k string) string { return u.String() + "|" + k }

func (s *stubIdemRepo) Get(_ context.Context, u uuid.UUID, k string) (*idempotency.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[s.key(u, k)]
	if !ok {
		return nil, shared.ErrNotFound
	}
	cp := *r
	return &cp, nil
}

func (s *stubIdemRepo) Put(_ context.Context, r *idempotency.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.puts++
	if _, ok := s.records[s.key(r.UserID, r.Key)]; ok {
		return nil
	}
	cp := *r
	s.records[s.key(r.UserID, r.Key)] = &cp
	return nil
}

func (s *stubIdemRepo) Cleanup(_ context.Context, _ time.Time) (int64, error) { return 0, nil }

// newTestRouter wires the Idempotency middleware behind a synthetic
// "auth" middleware that just stuffs the provided user into the gin
// context, mirroring the real auth path without standing up Keycloak.
func newTestRouter(repo idempotency.Repository, u *user.User, handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if u != nil {
			SetCurrentUser(c, u)
		}
		c.Next()
	})
	r.Use(Idempotency(repo))
	r.POST("/x", handler)
	r.PATCH("/x", handler)
	r.GET("/x", handler)
	return r
}

func mustUser() *user.User {
	return &user.User{ID: uuid.New(), Email: "a@b.com", IsActive: true}
}

// (a) Absent header → pass-through; nothing stored.
func TestIdempotency_AbsentHeader_PassThrough(t *testing.T) {
	repo := newStubRepo()
	handlerCalls := 0
	r := newTestRouter(repo, mustUser(), func(c *gin.Context) {
		handlerCalls++
		c.JSON(http.StatusCreated, gin.H{"ok": true})
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"a":1}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if handlerCalls != 1 {
		t.Fatalf("handler calls = %d, want 1", handlerCalls)
	}
	if repo.puts != 0 {
		t.Fatalf("repo.puts = %d, want 0 (no header → no store)", repo.puts)
	}
}

// (b) Hit with the same body → replay captured response, handler not
// invoked again.
func TestIdempotency_Replay(t *testing.T) {
	repo := newStubRepo()
	u := mustUser()
	handlerCalls := 0
	r := newTestRouter(repo, u, func(c *gin.Context) {
		handlerCalls++
		c.JSON(http.StatusCreated, gin.H{"id": "booking-1"})
	})

	body := `{"property":"abc"}`
	do := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
		req.Header.Set(IdempotencyHeader, "k-1")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	first := do()
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want 201", first.Code)
	}
	second := do()
	if second.Code != http.StatusCreated {
		t.Fatalf("replay status = %d, want 201", second.Code)
	}
	if second.Body.String() != first.Body.String() {
		t.Fatalf("replay body mismatch\nfirst:  %q\nsecond: %q", first.Body.String(), second.Body.String())
	}
	if second.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay marker header missing")
	}
	if handlerCalls != 1 {
		t.Fatalf("handler calls = %d, want 1 (second was a replay)", handlerCalls)
	}
}

// (c) Hit with the same key but a different body → 409 Conflict.
func TestIdempotency_ConflictOnKeyReuse(t *testing.T) {
	repo := newStubRepo()
	u := mustUser()
	r := newTestRouter(repo, u, func(c *gin.Context) {
		c.JSON(http.StatusCreated, gin.H{"id": uuid.NewString()})
	})

	doWith := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
		req.Header.Set(IdempotencyHeader, "k-2")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	if first := doWith(`{"v":1}`); first.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want 201", first.Code)
	}
	second := doWith(`{"v":2}`)
	if second.Code != http.StatusConflict {
		t.Fatalf("second status = %d, want 409", second.Code)
	}
}

// (d) Miss → handler runs, response is stored. Verified by a follow-up
// Get returning the captured record.
func TestIdempotency_MissStoresOnSuccess(t *testing.T) {
	repo := newStubRepo()
	u := mustUser()
	r := newTestRouter(repo, u, func(c *gin.Context) {
		c.JSON(http.StatusCreated, gin.H{"x": 1})
	})

	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"a":1}`))
	req.Header.Set(IdempotencyHeader, "k-3")
	r.ServeHTTP(httptest.NewRecorder(), req)

	got, err := repo.Get(context.Background(), u.ID, "k-3")
	if err != nil {
		t.Fatalf("Get after store: %v", err)
	}
	if got.StatusCode != http.StatusCreated {
		t.Fatalf("stored status = %d, want 201", got.StatusCode)
	}
	if !strings.Contains(string(got.ResponseBody), `"x":1`) {
		t.Fatalf("stored body = %q, want to contain x:1", string(got.ResponseBody))
	}
	if !strings.HasPrefix(got.ResponseContentType, "application/json") {
		t.Fatalf("stored content-type = %q, want application/json", got.ResponseContentType)
	}
}

// (e) Handler returns 5xx → nothing stored (so the client can retry
// cleanly after we recover).
func TestIdempotency_ServerErrorNotStored(t *testing.T) {
	repo := newStubRepo()
	u := mustUser()
	r := newTestRouter(repo, u, func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{"err": "boom"})
	})

	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{}`))
	req.Header.Set(IdempotencyHeader, "k-err")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if _, err := repo.Get(context.Background(), u.ID, "k-err"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("5xx should not be stored, got %v", err)
	}
}

// 4xx responses ARE the authoritative answer for "this request,
// repeated" and so are cached — a duplicate retry replays the original
// 4xx rather than re-running validation that will produce the same
// answer.
func TestIdempotency_ClientErrorStoredAndReplayed(t *testing.T) {
	repo := newStubRepo()
	u := mustUser()
	calls := 0
	r := newTestRouter(repo, u, func(c *gin.Context) {
		calls++
		c.JSON(http.StatusUnprocessableEntity, gin.H{"err": "validation"})
	})

	send := func() int {
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{}`))
		req.Header.Set(IdempotencyHeader, "k-422")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := send(); code != http.StatusUnprocessableEntity {
		t.Fatalf("first = %d, want 422", code)
	}
	if code := send(); code != http.StatusUnprocessableEntity {
		t.Fatalf("replay = %d, want 422", code)
	}
	if calls != 1 {
		t.Fatalf("handler calls = %d, want 1 (second is a replay)", calls)
	}
}

// GET requests bypass the middleware entirely — reads are naturally
// idempotent and we don't want to burn storage on every GET.
func TestIdempotency_NonMutatingBypass(t *testing.T) {
	repo := newStubRepo()
	r := newTestRouter(repo, mustUser(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(IdempotencyHeader, "k-get")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", rec.Code)
	}
	if repo.puts != 0 {
		t.Fatalf("repo.puts = %d, want 0 (GET should bypass)", repo.puts)
	}
}

// The downstream handler must still see the original body — the
// middleware reads it for hashing but restores it via NopCloser.
func TestIdempotency_BodyReachesHandler(t *testing.T) {
	repo := newStubRepo()
	var seen string
	r := newTestRouter(repo, mustUser(), func(c *gin.Context) {
		b, _ := io.ReadAll(c.Request.Body)
		seen = string(b)
		c.Status(http.StatusOK)
	})

	body := `{"payload":"after-middleware"}`
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	req.Header.Set(IdempotencyHeader, "k-body")
	r.ServeHTTP(httptest.NewRecorder(), req)
	if seen != body {
		t.Fatalf("handler saw %q, want %q (middleware should restore body)", seen, body)
	}
}

// Key length is capped at maxIdempotencyKeyLen to keep the storage
// column bounded; a misbehaving client gets 400 rather than being
// allowed to dump unbounded data into the table.
func TestIdempotency_OversizedKeyRejected(t *testing.T) {
	repo := newStubRepo()
	r := newTestRouter(repo, mustUser(), func(c *gin.Context) {
		c.Status(http.StatusCreated)
	})

	req := httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader([]byte(`{}`)))
	req.Header.Set(IdempotencyHeader, strings.Repeat("a", maxIdempotencyKeyLen+1))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for oversized key", rec.Code)
	}
}

// nil repo MUST degrade gracefully (transparent pass-through) so a
// misconfigured composition root cannot bring the API down.
func TestIdempotency_NilRepoNoOp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Idempotency(nil))
	r.POST("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{}`))
	req.Header.Set(IdempotencyHeader, "k")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 from nil-repo pass-through", rec.Code)
	}
}
