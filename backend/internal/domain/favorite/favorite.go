// Package favorite is the bounded context for guests' saved listings
// (wishlist). A Favorite is an association between a user and a property, which
// may optionally be filed under a named Collection.
package favorite

import (
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Favorite associates a user with a property they have saved. CollectionID,
// when set, files the favorite under one of the user's named wishlist
// collections; nil means the default "Saved" bucket.
type Favorite struct {
	UserID       uuid.UUID
	PropertyID   uuid.UUID
	CollectionID *uuid.UUID
	CreatedAt    time.Time
}

// New builds a Favorite for the given user and property, optionally filed under
// a collection (nil for the default bucket).
func New(userID, propertyID uuid.UUID, collectionID *uuid.UUID) *Favorite {
	return &Favorite{
		UserID:       userID,
		PropertyID:   propertyID,
		CollectionID: collectionID,
		CreatedAt:    time.Now().UTC(),
	}
}

// maxCollectionNameLen caps a wishlist collection's display name.
const maxCollectionNameLen = 60

// Collection is a user-owned named list grouping saved listings. ShareToken,
// when non-empty, makes the collection viewable by anyone holding the token
// (a public read-only share link); empty means private.
type Collection struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Name       string
	ShareToken string
	CreatedAt  time.Time
}

// NewCollection creates a named wishlist collection, validating the name.
func NewCollection(userID uuid.UUID, name string) (*Collection, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, shared.NewValidationError("collection name is required")
	}
	if len([]rune(name)) > maxCollectionNameLen {
		return nil, shared.NewValidationError("collection name is too long")
	}
	return &Collection{
		ID:        uuid.New(),
		UserID:    userID,
		Name:      name,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// Share assigns (or keeps) an unguessable public share token and returns it, so
// the collection becomes viewable by anyone holding the link.
func (c *Collection) Share() string {
	if c.ShareToken == "" {
		c.ShareToken = strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	return c.ShareToken
}

// Unshare revokes public access by clearing the share token.
func (c *Collection) Unshare() { c.ShareToken = "" }

// IsShared reports whether the collection currently has a public link.
func (c *Collection) IsShared() bool { return c.ShareToken != "" }

// CollectionWithCount pairs a collection with the number of listings saved in it
// (a read-model for the wishlist overview).
type CollectionWithCount struct {
	Collection *Collection
	Count      int
}
