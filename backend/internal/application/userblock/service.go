// Package userblockapp contains the use cases for blocking and unblocking a
// user. Messaging consults this context to refuse contact between blocked
// parties.
package userblockapp

import (
	"context"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/userblock"
	"github.com/google/uuid"
)

// Service orchestrates user-block use cases.
type Service struct {
	blocks userblock.Repository
}

// NewService wires the user-block application service.
func NewService(blocks userblock.Repository) *Service {
	return &Service{blocks: blocks}
}

// Block records that blockerID has blocked blockedID. A user cannot block
// themselves.
func (s *Service) Block(ctx context.Context, blockerID, blockedID uuid.UUID) error {
	if blockedID == uuid.Nil {
		return shared.NewValidationError("a user to block is required")
	}
	if blockerID == blockedID {
		return shared.NewValidationError("you cannot block yourself")
	}
	return s.blocks.Add(ctx, blockerID, blockedID)
}

// Unblock lifts a block.
func (s *Service) Unblock(ctx context.Context, blockerID, blockedID uuid.UUID) error {
	return s.blocks.Remove(ctx, blockerID, blockedID)
}

// ListBlocked returns the users the actor has blocked.
func (s *Service) ListBlocked(ctx context.Context, blockerID uuid.UUID) ([]uuid.UUID, error) {
	return s.blocks.ListBlocked(ctx, blockerID)
}

// IsBlocked reports whether either user has blocked the other.
func (s *Service) IsBlocked(ctx context.Context, a, b uuid.UUID) (bool, error) {
	return s.blocks.IsBlocked(ctx, a, b)
}
