// Package favorite is the bounded context for guests' saved listings
// (wishlist). A Favorite is an association between a user and a property.
package favorite

import (
	"time"

	"github.com/google/uuid"
)

// Favorite associates a user with a property they have saved.
type Favorite struct {
	UserID     uuid.UUID
	PropertyID uuid.UUID
	CreatedAt  time.Time
}

// New builds a Favorite for the given user and property.
func New(userID, propertyID uuid.UUID) *Favorite {
	return &Favorite{
		UserID:     userID,
		PropertyID: propertyID,
		CreatedAt:  time.Now().UTC(),
	}
}
