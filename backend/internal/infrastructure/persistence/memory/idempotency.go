package memory

import (
	"context"
	"sync"
	"time"

	"github.com/airhost/backend/internal/domain/idempotency"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// IdempotencyRepository is an in-memory idempotency.Repository: keyed
// on (userID, key) it captures the (status, content-type, body) of the
// first successful mutation and replays it on retries. Used in tests
// and local development; production wires the postgres adapter.
type IdempotencyRepository struct {
	mu      sync.Mutex
	records map[idemKey]*idempotency.Record
}

type idemKey struct {
	user uuid.UUID
	key  string
}

// NewIdempotencyRepository builds an empty in-memory store.
func NewIdempotencyRepository() *IdempotencyRepository {
	return &IdempotencyRepository{records: map[idemKey]*idempotency.Record{}}
}

// Get returns the record under (userID, key), or shared.ErrNotFound.
// A defensive copy is returned so the caller cannot mutate the stored
// record by reference.
func (r *IdempotencyRepository) Get(_ context.Context, userID uuid.UUID, key string) (*idempotency.Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.records[idemKey{userID, key}]
	if !ok {
		return nil, shared.ErrNotFound
	}
	cp := *rec
	// Slices are still aliased after the struct copy; clone the bytes
	// so a caller writing into them cannot corrupt the stored record.
	cp.BodyHash = append([]byte(nil), rec.BodyHash...)
	cp.ResponseBody = append([]byte(nil), rec.ResponseBody...)
	return &cp, nil
}

// Put stores a record. First writer wins on (userID, key); a second
// Put under the same composite key is a no-op, mirroring the postgres
// ON CONFLICT DO NOTHING behaviour.
func (r *IdempotencyRepository) Put(_ context.Context, rec *idempotency.Record) error {
	if rec == nil {
		return shared.NewValidationError("idempotency: record is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	k := idemKey{rec.UserID, rec.Key}
	if _, exists := r.records[k]; exists {
		return nil
	}
	cp := *rec
	cp.BodyHash = append([]byte(nil), rec.BodyHash...)
	cp.ResponseBody = append([]byte(nil), rec.ResponseBody...)
	r.records[k] = &cp
	return nil
}

// Cleanup drops records older than olderThan. The boundary is
// inclusive (older-or-equal) to mirror the postgres adapter.
func (r *IdempotencyRepository) Cleanup(_ context.Context, olderThan time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var deleted int64
	for k, rec := range r.records {
		if !rec.CreatedAt.After(olderThan) {
			delete(r.records, k)
			deleted++
		}
	}
	return deleted, nil
}

var _ idempotency.Repository = (*IdempotencyRepository)(nil)
