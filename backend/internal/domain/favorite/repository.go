package favorite

import (
	"context"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Repository is the persistence port for the favorite context.
type Repository interface {
	// Add saves a favorite. Re-adding an existing favorite updates its collection
	// (so "save to <list>" works even when already saved) and is otherwise a
	// no-op, keeping the operation idempotent.
	Add(ctx context.Context, f *Favorite) error
	// Remove deletes a favorite; removing a missing one is a no-op.
	Remove(ctx context.Context, userID, propertyID uuid.UUID) error
	Exists(ctx context.Context, userID, propertyID uuid.UUID) (bool, error)
	// ListPropertyIDs returns every property ID a user has favorited (across all
	// collections), newest first.
	ListPropertyIDs(ctx context.Context, userID uuid.UUID, page shared.Page) (shared.PageResult[uuid.UUID], error)
	// ListPropertyIDsInCollection returns the property IDs filed under a specific
	// collection; a nil collectionID selects the default "Saved" bucket.
	ListPropertyIDsInCollection(ctx context.Context, userID uuid.UUID, collectionID *uuid.UUID, page shared.Page) (shared.PageResult[uuid.UUID], error)
	// SetCollection moves an existing favorite into a collection (nil = default
	// bucket). Returns ErrNotFound when the favorite does not exist.
	SetCollection(ctx context.Context, userID, propertyID uuid.UUID, collectionID *uuid.UUID) error

	// CreateCollection persists a new named collection.
	CreateCollection(ctx context.Context, c *Collection) error
	// DeleteCollection removes a collection the user owns; its favorites fall back
	// to the default bucket. Returns ErrNotFound when absent or not owned.
	DeleteCollection(ctx context.Context, userID, collectionID uuid.UUID) error
	// ListCollections returns the user's collections with their saved-listing
	// counts, newest first.
	ListCollections(ctx context.Context, userID uuid.UUID) ([]CollectionWithCount, error)
	// FindCollection loads a collection the user owns (for ownership checks).
	FindCollection(ctx context.Context, userID, collectionID uuid.UUID) (*Collection, error)
	// SetCollectionShareToken sets (or clears, when token is empty) a collection's
	// public share token. The collection must belong to the user; returns
	// ErrNotFound otherwise.
	SetCollectionShareToken(ctx context.Context, userID, collectionID uuid.UUID, token string) error
	// FindCollectionByShareToken loads a collection by its public share token,
	// regardless of owner. Returns ErrNotFound when the token is unknown.
	FindCollectionByShareToken(ctx context.Context, token string) (*Collection, error)
}
