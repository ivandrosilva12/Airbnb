package offer

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the persistence port for the Offer aggregate.
type Repository interface {
	Create(ctx context.Context, o *Offer) error
	Update(ctx context.Context, o *Offer) error
	FindByID(ctx context.Context, id uuid.UUID) (*Offer, error)
	// ListForGuest returns offers addressed to a guest, newest first.
	ListForGuest(ctx context.Context, guestID uuid.UUID) ([]*Offer, error)
	// ListForHost returns offers a host has sent, newest first.
	ListForHost(ctx context.Context, hostID uuid.UUID) ([]*Offer, error)
}
