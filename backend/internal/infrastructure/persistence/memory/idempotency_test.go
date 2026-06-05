package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/airhost/backend/internal/domain/idempotency"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

func TestIdempotencyRepository_PutGet(t *testing.T) {
	ctx := context.Background()
	repo := NewIdempotencyRepository()
	uid := uuid.New()

	if _, err := repo.Get(ctx, uid, "missing"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get on miss: want ErrNotFound, got %v", err)
	}

	rec, err := idempotency.New("k-1", uid, "POST", "/api/v1/bookings", []byte{0xaa}, 201, []byte(`{"id":1}`), "application/json")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := repo.Put(ctx, rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := repo.Get(ctx, uid, "k-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.StatusCode != 201 || string(got.ResponseBody) != `{"id":1}` || got.ResponseContentType != "application/json" {
		t.Fatalf("captured response not returned: %+v", got)
	}

	// Same key under a DIFFERENT user must miss — the composite PK
	// scopes the namespace per-user.
	other := uuid.New()
	if _, err := repo.Get(ctx, other, "k-1"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("cross-user leak: want ErrNotFound, got %v", err)
	}

	// First-writer-wins: a second Put with the same composite key is a
	// no-op, the original record stands.
	rec2, _ := idempotency.New("k-1", uid, "POST", "/api/v1/bookings", []byte{0xbb}, 500, []byte(`x`), "text/plain")
	if err := repo.Put(ctx, rec2); err != nil {
		t.Fatalf("second Put: %v", err)
	}
	got2, _ := repo.Get(ctx, uid, "k-1")
	if got2.StatusCode != 201 {
		t.Fatalf("first-writer-wins violated: status = %d", got2.StatusCode)
	}
}

func TestIdempotencyRepository_Cleanup(t *testing.T) {
	ctx := context.Background()
	repo := NewIdempotencyRepository()
	uid := uuid.New()

	old, _ := idempotency.New("old", uid, "POST", "/p", []byte{0x01}, 200, nil, "")
	old.CreatedAt = time.Now().UTC().Add(-48 * time.Hour)
	fresh, _ := idempotency.New("fresh", uid, "POST", "/p", []byte{0x02}, 200, nil, "")
	if err := repo.Put(ctx, old); err != nil {
		t.Fatalf("Put old: %v", err)
	}
	if err := repo.Put(ctx, fresh); err != nil {
		t.Fatalf("Put fresh: %v", err)
	}

	deleted, err := repo.Cleanup(ctx, time.Now().UTC().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	if _, err := repo.Get(ctx, uid, "old"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("old should have been pruned")
	}
	if _, err := repo.Get(ctx, uid, "fresh"); err != nil {
		t.Fatalf("fresh should remain: %v", err)
	}
}
