// Package userblock is the bounded context for one user blocking another. A
// block prevents either party from starting a conversation with, or messaging,
// the other. Blocks are directional records but enforcement is symmetric: if
// A blocks B, neither can message the other.
package userblock

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the persistence port for user blocks.
type Repository interface {
	// Add records that blockerID has blocked blockedID (idempotent).
	Add(ctx context.Context, blockerID, blockedID uuid.UUID) error
	// Remove lifts a block previously placed by blockerID on blockedID.
	Remove(ctx context.Context, blockerID, blockedID uuid.UUID) error
	// ListBlocked returns the users blockerID has blocked.
	ListBlocked(ctx context.Context, blockerID uuid.UUID) ([]uuid.UUID, error)
	// IsBlocked reports whether either user has blocked the other.
	IsBlocked(ctx context.Context, a, b uuid.UUID) (bool, error)
}
