// Package messagetemplateapp orchestrates the host's saved-reply playbook.
package messagetemplateapp

import (
	"context"

	"github.com/airhost/backend/internal/domain/messagetemplate"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Service orchestrates template use cases. Every mutation is gated on
// ownership — the playbook is the host's own.
type Service struct {
	repo messagetemplate.Repository
}

// NewService wires the application service.
func NewService(repo messagetemplate.Repository) *Service {
	return &Service{repo: repo}
}

// Create persists a new template for the host. Aggregate-level invariants
// (label/body length, trimmed) are enforced by messagetemplate.New.
func (s *Service) Create(ctx context.Context, hostID uuid.UUID, label, body string) (*messagetemplate.Template, error) {
	t, err := messagetemplate.New(hostID, label, body)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// Update replaces a template's label/body. Only the owning host may edit.
func (s *Service) Update(ctx context.Context, actorID, templateID uuid.UUID, label, body string) (*messagetemplate.Template, error) {
	t, err := s.owned(ctx, actorID, templateID)
	if err != nil {
		return nil, err
	}
	if err := t.Update(label, body); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// Delete removes a template owned by the actor.
func (s *Service) Delete(ctx context.Context, actorID, templateID uuid.UUID) error {
	if _, err := s.owned(ctx, actorID, templateID); err != nil {
		return err
	}
	return s.repo.Delete(ctx, templateID)
}

// ListMine returns the actor's templates (ordered by label).
func (s *Service) ListMine(ctx context.Context, actorID uuid.UUID) ([]*messagetemplate.Template, error) {
	return s.repo.ListByHost(ctx, actorID)
}

// owned is the ownership gate used by Update and Delete.
func (s *Service) owned(ctx context.Context, actorID, templateID uuid.UUID) (*messagetemplate.Template, error) {
	t, err := s.repo.FindByID(ctx, templateID)
	if err != nil {
		return nil, err
	}
	if !t.IsOwnedBy(actorID) {
		return nil, shared.ErrForbidden
	}
	return t, nil
}
