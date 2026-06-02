// Package userapp contains user-related use cases.
package userapp

import (
	"context"
	"errors"
	"strings"

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

// Identity is the data extracted from a verified Keycloak token. Roles are the
// realm roles asserted by the token; they grant host/admin locally.
type Identity struct {
	Subject  string
	Email    string
	FullName string
	Roles    []string
}

// SyncFromIdentity returns the local user for a Keycloak identity, creating one
// on first login (just-in-time provisioning). The platform role is derived from
// the token's realm roles: on creation the user gets that role, and on later
// logins the role is *elevated* to match (never demoted, so a self-service host
// promotion survives even if the IdP does not assert the host role).
func (s *Service) SyncFromIdentity(ctx context.Context, id Identity) (*user.User, error) {
	tokenRole := user.RoleFromRealmRoles(id.Roles)

	existing, err := s.repo.FindByKeycloakSub(ctx, id.Subject)
	if err == nil {
		if existing.ElevateRole(tokenRole) {
			if err := s.repo.Update(ctx, existing); err != nil {
				return nil, err
			}
		}
		return existing, nil
	}
	if !errors.Is(err, shared.ErrNotFound) {
		return nil, err
	}

	// New subject for this identity. If a user with the token's (verified) email
	// already exists — e.g. the IdP re-provisioned the account with a new subject
	// — re-link that account to the new subject instead of failing on the unique
	// email constraint.
	if email := strings.TrimSpace(strings.ToLower(id.Email)); email != "" {
		byEmail, e := s.repo.FindByEmail(ctx, email)
		if e == nil {
			changed := byEmail.RelinkKeycloakSub(id.Subject)
			elevated := byEmail.ElevateRole(tokenRole)
			if changed || elevated {
				if err := s.repo.Update(ctx, byEmail); err != nil {
					return nil, err
				}
			}
			return byEmail, nil
		}
		if !errors.Is(e, shared.ErrNotFound) {
			return nil, e
		}
	}

	u, err := user.NewUser(id.Subject, id.Email, fallback(id.FullName, id.Email), tokenRole)
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

// UpdatePushPreferences sets which push categories the user's devices receive.
func (s *Service) UpdatePushPreferences(ctx context.Context, id uuid.UUID, prefs user.PushPreferences) (*user.User, error) {
	u, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	u.SetPushPreferences(prefs)
	if err := s.repo.Update(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// Suspend disables a user account (S61). Admin-only — the route gate enforces
// that; this service just performs the state change. Returns the updated user.
// A no-op (already inactive) returns the current state without error so a
// retry doesn't fail; the audit hook in the handler is keyed on a successful
// admin call rather than a state transition.
func (s *Service) Suspend(ctx context.Context, id uuid.UUID) (*user.User, error) {
	u, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !u.Suspend() {
		return u, nil
	}
	if err := s.repo.Update(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// Unsuspend reinstates a previously suspended account (S61). Refuses
// anonymised accounts (sentinel KeycloakSub) — returns ErrValidation so the
// admin sees why instead of a silent success.
func (s *Service) Unsuspend(ctx context.Context, id uuid.UUID) (*user.User, error) {
	u, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if u.IsActive {
		return u, nil
	}
	if !u.Unsuspend() {
		return nil, shared.NewValidationError("account cannot be reactivated (anonymised by data-erasure)")
	}
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
