// Package idempotency owns the RFC-style Idempotency-Key replay record
// (S160). The middleware records the (status, content-type, body) of
// the first successful mutation under a given (user_id, key) tuple,
// then replays it verbatim on subsequent retries that arrive with the
// same key + body hash.
//
// The aggregate is intentionally thin: it is a transport-layer cache
// keyed on the caller's identity, not a domain entity in its own right.
// Keeping it in its own bounded context (rather than sprinkling the
// concept across the http middleware package) lets the postgres adapter
// stay storage-symmetric with the rest of the platform, and lets tests
// stub a Repository without standing up a router.
package idempotency

import (
	"context"
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Record is one captured request+response pair. The fields mirror the
// request_idempotency table verbatim — there is no separate read model
// because the middleware uses the record as a literal replay buffer.
type Record struct {
	// Key is the client-supplied Idempotency-Key header value.
	Key string
	// UserID scopes the key to the authenticated caller, so two users
	// can independently pick the same key string without colliding (and
	// neither one can guess the other's captured response).
	UserID uuid.UUID
	// Method, Path identify the original mutation. A hit under the same
	// key but a different (method, path) is treated as a 409 conflict
	// rather than a replay — the client recycled the key by accident.
	Method string
	Path   string
	// BodyHash is the SHA-256 of the request body that produced the
	// captured response. A retry whose body hashes differently triggers
	// the same 409 conflict — same key, different request.
	BodyHash []byte
	// StatusCode, ResponseBody, ResponseContentType are the literal
	// bytes we replay to a duplicate request. The middleware writes
	// them straight back to the client; no re-rendering.
	StatusCode          int
	ResponseBody        []byte
	ResponseContentType string
	CreatedAt           time.Time
}

// New builds a Record, validating the inputs that must be present for
// the replay path to be safe. Returns shared.NewValidationError when
// any required field is missing.
func New(key string, userID uuid.UUID, method, path string, bodyHash []byte, statusCode int, responseBody []byte, responseContentType string) (*Record, error) {
	if strings.TrimSpace(key) == "" {
		return nil, shared.NewValidationError("idempotency: key is required")
	}
	if userID == uuid.Nil {
		return nil, shared.NewValidationError("idempotency: userID is required")
	}
	if strings.TrimSpace(method) == "" {
		return nil, shared.NewValidationError("idempotency: method is required")
	}
	if strings.TrimSpace(path) == "" {
		return nil, shared.NewValidationError("idempotency: path is required")
	}
	if len(bodyHash) == 0 {
		return nil, shared.NewValidationError("idempotency: bodyHash is required")
	}
	if statusCode <= 0 {
		return nil, shared.NewValidationError("idempotency: statusCode is required")
	}
	return &Record{
		Key:                 key,
		UserID:              userID,
		Method:              method,
		Path:                path,
		BodyHash:            bodyHash,
		StatusCode:          statusCode,
		ResponseBody:        responseBody,
		ResponseContentType: responseContentType,
		CreatedAt:           time.Now().UTC(),
	}, nil
}

// Repository is the persistence port for Idempotency-Key replay
// records. Implementations live under infrastructure/persistence/.
type Repository interface {
	// Get returns the record stored under (userID, key), or
	// shared.ErrNotFound when there is no hit. The caller compares
	// method/path/body_hash itself to distinguish "replay" from "key
	// reused for a different request".
	Get(ctx context.Context, userID uuid.UUID, key string) (*Record, error)
	// Put persists a fresh record. Idempotent on (userID, key) — a
	// second Put with the same key is a no-op (the first writer wins).
	// The middleware never overwrites: if Get already returned a hit,
	// Put is not called.
	Put(ctx context.Context, r *Record) error
	// Cleanup deletes records older than olderThan. The hourly
	// scheduler job calls this with now-24h so the table cannot grow
	// indefinitely under retry storms.
	Cleanup(ctx context.Context, olderThan time.Time) (int64, error)
}
