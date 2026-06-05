package propertyapp

import (
	"context"
	"errors"
	"strings"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/audit"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
)

// AuditRecord is the narrow input the cohost service hands to its
// Auditor sink. Mirrors auditapp.RecordInput field-for-field but is
// declared HERE so this package does not import the auditapp wrapper
// — the main.go adapter translates one into the other. Keeping the
// shape local follows the OfferMetrics pattern S113 used to wire
// observability without cross-package coupling at the application
// layer.
type AuditRecord struct {
	ActorID    uuid.UUID
	Action     audit.Action
	TargetType audit.TargetType
	TargetID   uuid.UUID
	Metadata   map[string]any
}

// Auditor is the narrow sink the cohost service uses to append a
// compliance row for a role-mutating action (S120). The concrete
// wiring in main.go adapts auditapp.Service into this shape.
type Auditor interface {
	Record(ctx context.Context, rec AuditRecord) error
}

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
	// pub, when wired via WithPublisher, receives CohostInvited so the
	// notification subscriber can ping the invitee (S99 — WF-GAP-016).
	// Optional — older tests leave it nil and emit becomes a no-op.
	pub event.Publisher
	// auditor, when wired via WithAuditor, appends a "cohost.invited"
	// row on every successful Invite so the compliance trail records
	// who granted whom which permissions on which listing (S120).
	// Optional — older tests / wiring leave it nil and the call site
	// short-circuits without writing.
	auditor Auditor
	// uow, when wired (S156), routes Invite's cohort-row write + the
	// CohostInvited outbox event through one UnitOfWork so a crash
	// between them no longer drops the event (the legacy in-process
	// s.pub.Publish path landed the row first and emitted second — any
	// failure between those two steps silently lost the CohostInvited
	// notification). Nil falls back to the legacy publisher path so
	// older tests that don't wire a UoW keep working.
	uow port.UnitOfWork
}

// NewCohostService wires the cohost application service.
func NewCohostService(cohosts property.CohostRepository, properties property.Repository, users user.Repository) *CohostService {
	return &CohostService{cohosts: cohosts, properties: properties, users: users}
}

// WithPublisher plugs an event Publisher so Invite surfaces CohostInvited
// (S99 — WF-GAP-016). Fluent setter; pre-existing call-sites keep working.
func (s *CohostService) WithPublisher(p event.Publisher) *CohostService {
	s.pub = p
	return s
}

// WithAuditor plugs an Auditor so a successful Invite appends a
// "cohost.invited" audit row alongside the domain event publication
// (S120). Fluent setter; pre-existing call-sites keep working and the
// Invite path short-circuits when no auditor is wired.
func (s *CohostService) WithAuditor(a Auditor) *CohostService {
	s.auditor = a
	return s
}

// WithUnitOfWork wires a UoW into the service so Invite commits the
// cohost row write + the CohostInvited outbox event in a single
// transaction (S156 — Cohost UoW slice). Without it, the service falls
// back to the legacy pool-bound write + in-process s.pub.Publish path
// where a crash between the row commit and the event emit silently
// drops the notification. Returns the same service for chained
// construction.
func (s *CohostService) WithUnitOfWork(uow port.UnitOfWork) *CohostService {
	s.uow = uow
	return s
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
	perms := make([]string, 0, len(in.Permissions))
	for _, p := range in.Permissions {
		perms = append(perms, string(p))
	}
	if err := s.persistInvite(ctx, c, prop, userID, perms); err != nil {
		return nil, err
	}
	// S120 — append the compliance row. TargetID is the invitee (the
	// subject of the role mutation); the property id + permission list
	// ride in Metadata so the audit row is self-contained even if the
	// cohost grant is later revoked or deleted. A nil auditor keeps
	// older tests / wiring working with no audit side-effect.
	if s.auditor != nil {
		if err := s.auditor.Record(ctx, AuditRecord{
			ActorID:    in.HostID,
			Action:     audit.ActionCohostInvited,
			TargetType: audit.TargetUser,
			TargetID:   userID,
			Metadata: map[string]any{
				"property_id": prop.ID.String(),
				"permissions": perms,
			},
		}); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// persistInvite writes the new cohost grant + a CohostInvited outbox
// event atomically. When a UoW is wired (S156), both writes land in the
// same transaction — so a crash between them no longer drops the event.
// When the UoW is absent (legacy tests / call sites built before S156),
// it falls back to the pool-bound repo + in-process s.pub.Publish path
// (matching the pre-S156 behaviour). port.Tx does not currently expose
// a Cohosts repo so the row write itself goes through the service's
// pool-bound repo even inside the closure — the outbox append still
// participates in the same transaction, which closes the
// "row landed but event was lost" gap that motivated the slice
// (mirrors review.persistCreate's fallback when tx.Reviews is nil).
func (s *CohostService) persistInvite(ctx context.Context, c *property.Cohost, prop *property.Property, userID uuid.UUID, perms []string) error {
	if s.uow == nil {
		if err := s.cohosts.Create(ctx, c); err != nil {
			return err
		}
		if s.pub != nil {
			s.pub.Publish(ctx, event.CohostInvited{
				CohostID:      c.ID,
				PropertyID:    prop.ID,
				PropertyTitle: prop.Title,
				HostID:        prop.HostID,
				UserID:        userID,
				Permissions:   perms,
			})
		}
		return nil
	}
	return s.uow.Run(ctx, func(tx port.Tx) error {
		if err := s.cohosts.Create(ctx, c); err != nil {
			return err
		}
		rec, err := event.NewRecord(event.CohostInvited{
			CohostID:      c.ID,
			PropertyID:    prop.ID,
			PropertyTitle: prop.Title,
			HostID:        prop.HostID,
			UserID:        userID,
			Permissions:   perms,
		})
		if err != nil {
			return err
		}
		return tx.Outbox.Append(ctx, rec)
	})
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
