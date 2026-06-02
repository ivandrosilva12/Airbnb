package propertyapp

import (
	"context"
	"errors"
	"strings"

	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
)

// CohostService orchestrates the per-listing co-host grants: invite by email,
// list, update permissions, revoke. Every mutation is gated on the caller
// being the primary host of the listing — co-hosts cannot manage other
// co-hosts.
//
// Lookups: the gate used by sibling services (block, pricerule, etc) lives on
// the read-only Authorize method here so each bounded context can opt into
// the expanded "owner OR cohost with required perm" check without duplicating
// the FindByPropertyAndUser dance.
type CohostService struct {
	cohosts    property.CohostRepository
	properties property.Repository
	users      user.Repository
}

// NewCohostService wires the cohost application service.
func NewCohostService(cohosts property.CohostRepository, properties property.Repository, users user.Repository) *CohostService {
	return &CohostService{cohosts: cohosts, properties: properties, users: users}
}

// InviteInput carries the data required to add a co-host to a listing. Either
// UserID or Email must be set; if both are set, UserID wins.
type InviteInput struct {
	HostID      uuid.UUID
	PropertyID  uuid.UUID
	UserID      uuid.UUID
	Email       string
	Permissions []property.CohostPermission
}

// Invite grants a user one or more permissions on a listing the caller owns.
// It rejects:
//   - non-owner callers (only the primary host invites)
//   - inviting yourself (already the host)
//   - inviting the same user twice (PATCH instead via SetPermissions)
//   - empty / unknown permission sets (caught by the domain aggregate)
func (s *CohostService) Invite(ctx context.Context, in InviteInput) (*property.Cohost, error) {
	prop, err := s.ownedProperty(ctx, in.HostID, in.PropertyID)
	if err != nil {
		return nil, err
	}
	userID, err := s.resolveUser(ctx, in.UserID, in.Email)
	if err != nil {
		return nil, err
	}
	if userID == prop.HostID {
		return nil, shared.NewValidationError("the primary host cannot be invited as a co-host")
	}
	// A grant for the same (property, user) pair already exists — caller
	// should PATCH it instead of re-inviting.
	existing, err := s.cohosts.FindByPropertyAndUser(ctx, prop.ID, userID)
	if err != nil && !errors.Is(err, shared.ErrNotFound) {
		return nil, err
	}
	if existing != nil {
		return nil, shared.ErrConflict
	}
	c, err := property.NewCohost(prop.ID, userID, in.Permissions)
	if err != nil {
		return nil, err
	}
	if err := s.cohosts.Create(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// ListForProperty returns every co-host on a listing the caller owns.
func (s *CohostService) ListForProperty(ctx context.Context, hostID, propertyID uuid.UUID) ([]*property.Cohost, error) {
	if _, err := s.ownedProperty(ctx, hostID, propertyID); err != nil {
		return nil, err
	}
	return s.cohosts.ListByProperty(ctx, propertyID)
}

// SetPermissions replaces the permission set on an existing grant.
func (s *CohostService) SetPermissions(ctx context.Context, hostID, propertyID, cohostID uuid.UUID, perms []property.CohostPermission) (*property.Cohost, error) {
	if _, err := s.ownedProperty(ctx, hostID, propertyID); err != nil {
		return nil, err
	}
	c, err := s.cohosts.FindByID(ctx, cohostID)
	if err != nil {
		return nil, err
	}
	if c.PropertyID != propertyID {
		// The cohost id belongs to a different listing — treat as not found
		// so a host can't probe another listing's grants by id.
		return nil, shared.ErrNotFound
	}
	if err := c.SetPermissions(perms); err != nil {
		return nil, err
	}
	if err := s.cohosts.Update(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// Revoke deletes a grant.
func (s *CohostService) Revoke(ctx context.Context, hostID, propertyID, cohostID uuid.UUID) error {
	if _, err := s.ownedProperty(ctx, hostID, propertyID); err != nil {
		return err
	}
	c, err := s.cohosts.FindByID(ctx, cohostID)
	if err != nil {
		return err
	}
	if c.PropertyID != propertyID {
		return shared.ErrNotFound
	}
	return s.cohosts.Delete(ctx, cohostID)
}

// ListMine returns the listings on which the caller is a co-host (i.e. not the
// primary host). Used by the host area to surface "listings I help manage".
func (s *CohostService) ListMine(ctx context.Context, userID uuid.UUID) ([]*property.Property, error) {
	ids, err := s.cohosts.ListPropertiesForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []*property.Property{}, nil
	}
	return s.properties.FindByIDs(ctx, ids)
}

// ListPropertyIDsWithPermission returns the listing IDs on which the caller
// has a co-host grant containing the given permission. Used by sibling
// services (e.g. messaging) to scope a "team mailbox" view to listings the
// co-host is actually allowed to act on.
//
// Performance contract: one query. The previous implementation fetched
// property IDs then did FindByPropertyAndUser per id (N+1 over the user's
// portfolio of grants). ListByUser returns every grant in a single query and
// we filter by Permission in memory — the per-user grant count is small by
// design (a user typically co-hosts a handful of listings, not hundreds).
func (s *CohostService) ListPropertyIDsWithPermission(ctx context.Context, userID uuid.UUID, required property.CohostPermission) ([]uuid.UUID, error) {
	grants, err := s.cohosts.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(grants))
	for _, g := range grants {
		if g.Has(required) {
			out = append(out, g.PropertyID)
		}
	}
	return out, nil
}

// Authorize is the relaxed permission gate used by sibling services (calendar
// blocks, price rules) that admit co-host access. It returns the listing if
// the actor is either the primary host OR a co-host with the required
// permission, and shared.ErrForbidden otherwise. Callers that genuinely need
// owner-only access (delete listing, payouts, photo upload, etc) keep using
// the strict IsOwnedBy gate instead.
func (s *CohostService) Authorize(ctx context.Context, actorID, propertyID uuid.UUID, required property.CohostPermission) (*property.Property, error) {
	prop, err := s.properties.FindByID(ctx, propertyID)
	if err != nil {
		return nil, err
	}
	if prop.IsOwnedBy(actorID) {
		return prop, nil
	}
	grant, err := s.cohosts.FindByPropertyAndUser(ctx, propertyID, actorID)
	if err != nil {
		if errors.Is(err, shared.ErrNotFound) {
			return nil, shared.ErrForbidden
		}
		return nil, err
	}
	if !grant.Has(required) {
		return nil, shared.ErrForbidden
	}
	return prop, nil
}

// ownedProperty mirrors propertyapp.Service.ownedProperty — owner-only access.
// We keep a local copy so CohostService does not need to depend on the main
// property service.
func (s *CohostService) ownedProperty(ctx context.Context, actorID, propertyID uuid.UUID) (*property.Property, error) {
	p, err := s.properties.FindByID(ctx, propertyID)
	if err != nil {
		return nil, err
	}
	if !p.IsOwnedBy(actorID) {
		return nil, shared.ErrForbidden
	}
	return p, nil
}

// resolveUser turns the (userID, email) pair into a concrete user id. Email
// lookup is case-insensitive (the user repo already normalises stored
// emails). Returns ErrNotFound if neither input resolves.
func (s *CohostService) resolveUser(ctx context.Context, userID uuid.UUID, email string) (uuid.UUID, error) {
	if userID != uuid.Nil {
		u, err := s.users.FindByID(ctx, userID)
		if err != nil {
			return uuid.Nil, err
		}
		return u.ID, nil
	}
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return uuid.Nil, shared.NewValidationError("user id or email is required")
	}
	u, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, shared.ErrNotFound) {
			return uuid.Nil, shared.NewValidationError("no user with that email")
		}
		return uuid.Nil, err
	}
	return u.ID, nil
}
