// Package identityapp contains KYC identity-verification use cases: a user
// submits an identity document and an administrator approves or rejects it.
// Approval publishes an IdentityVerified domain event so the notification and
// email contexts can react.
package identityapp

import (
	"context"
	"errors"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/identity"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Service orchestrates identity-verification use cases.
type Service struct {
	repo identity.Repository
	uow  port.UnitOfWork
}

// NewService wires the identity application service. The UnitOfWork makes the
// approval write and its IdentityVerified event commit atomically.
func NewService(repo identity.Repository, uow port.UnitOfWork) *Service {
	return &Service{repo: repo, uow: uow}
}

// SubmitInput carries a user's document submission.
type SubmitInput struct {
	DocumentType string
	DocumentRef  string
	LegalName    string
}

// Submit files a new verification request for the user. A user may not submit
// while a request is already pending, nor once they are already verified.
func (s *Service) Submit(ctx context.Context, userID uuid.UUID, in SubmitInput) (*identity.Verification, error) {
	latest, err := s.repo.FindLatestByUser(ctx, userID)
	switch {
	case err == nil:
		if latest.Status == identity.StatusApproved {
			return nil, shared.NewValidationError("your identity is already verified")
		}
		if latest.IsPending() {
			return nil, shared.NewValidationError("a verification is already under review")
		}
	case errors.Is(err, shared.ErrNotFound):
		// first submission — fall through
	default:
		return nil, err
	}

	v, err := identity.NewVerification(userID, identity.DocumentType(in.DocumentType), in.DocumentRef, in.LegalName)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, v); err != nil {
		return nil, err
	}
	return v, nil
}

// GetMine returns the user's latest verification, or shared.ErrNotFound.
func (s *Service) GetMine(ctx context.Context, userID uuid.UUID) (*identity.Verification, error) {
	return s.repo.FindLatestByUser(ctx, userID)
}

// IsVerified reports whether the user's latest request was approved.
func (s *Service) IsVerified(ctx context.Context, userID uuid.UUID) (bool, error) {
	v, err := s.repo.FindLatestByUser(ctx, userID)
	if err != nil {
		if errors.Is(err, shared.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return v.Status == identity.StatusApproved, nil
}

// ListPending returns the administrator review queue, newest first.
func (s *Service) ListPending(ctx context.Context, page shared.Page) (shared.PageResult[*identity.Verification], error) {
	return s.repo.ListByStatus(ctx, identity.StatusPending, page)
}

// Approve marks a pending request verified (administrator action) and publishes
// an IdentityVerified event.
func (s *Service) Approve(ctx context.Context, adminID, verificationID uuid.UUID) (*identity.Verification, error) {
	v, err := s.repo.FindByID(ctx, verificationID)
	if err != nil {
		return nil, err
	}
	if err := v.Approve(adminID); err != nil {
		return nil, err
	}
	if err := s.uow.Run(ctx, func(tx port.Tx) error {
		if err := tx.Identity.Update(ctx, v); err != nil {
			return err
		}
		rec, err := event.NewRecord(event.IdentityVerified{VerificationID: v.ID, UserID: v.UserID})
		if err != nil {
			return err
		}
		return tx.Outbox.Append(ctx, rec)
	}); err != nil {
		return nil, err
	}
	return v, nil
}

// Reject refuses a pending request with a reason (administrator action). The
// user may resubmit afterwards.
func (s *Service) Reject(ctx context.Context, adminID, verificationID uuid.UUID, reason string) (*identity.Verification, error) {
	v, err := s.repo.FindByID(ctx, verificationID)
	if err != nil {
		return nil, err
	}
	if err := v.Reject(adminID, reason); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, v); err != nil {
		return nil, err
	}
	return v, nil
}
