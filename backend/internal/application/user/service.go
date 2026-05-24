// Package userapp contains user-related use cases.
package userapp

import (
	"context"
	"errors"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
)

// Service orchestrates user use cases.
type Service struct {
	repo user.Repository
}

// NewService wires the user application service.
func NewService(repo user.Repository) *Service {
	return &Service{repo: repo}
}

// Identity is the data extracted from a verified Keycloak token.
type Identity struct {
	Subject  string
	Email    string
	FullName string
}

// SyncFromIdentity returns the local user for a Keycloak identity, creating one
// on first login (just-in-time provisioning).
func (s *Service) SyncFromIdentity(ctx context.Context, id Identity) (*user.User, error) {
	existing, err := s.repo.FindByKeycloakSub(ctx, id.Subject)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, shared.ErrNotFound) {
		return nil, err
	}

	u, err := user.NewUser(id.Subject, id.Email, fallback(id.FullName, id.Email), user.RoleGuest)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// GetByID fetches a user by its internal id.
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (*user.User, error) {
	return s.repo.FindByID(ctx, id)
}

// UpdateProfileInput carries editable profile fields.
type UpdateProfileInput struct {
	FullName  string
	AvatarURL string
}

// UpdateProfile applies profile changes for the given user.
func (s *Service) UpdateProfile(ctx context.Context, id uuid.UUID, in UpdateProfileInput) (*user.User, error) {
	u, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := u.UpdateProfile(in.FullName, in.AvatarURL); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// UpdateEmailPreferences sets which transactional emails the user receives.
func (s *Service) UpdateEmailPreferences(ctx context.Context, id uuid.UUID, prefs user.EmailPreferences) (*user.User, error) {
	u, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	u.SetEmailPreferences(prefs)
	if err := s.repo.Update(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// BecomeHost promotes the user to host.
func (s *Service) BecomeHost(ctx context.Context, id uuid.UUID) (*user.User, error) {
	u, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	u.PromoteToHost()
	if err := s.repo.Update(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

func fallback(primary, secondary string) string {
	if primary != "" {
		return primary
	}
	return secondary
}
