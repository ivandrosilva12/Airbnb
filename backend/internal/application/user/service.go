// Package userapp contains user-related use cases.
package userapp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/airhost/backend/internal/domain/audit"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
)

// PropertyCascader is the narrow port the user service uses to
// auto-suspend a host's listings when the host is suspended (S169).
// adminID rides through so the listing's audit row attributes the
// state change to the same human who suspended the host.
// Implemented by *propertyapp.Service.Suspend at the composition root.
type PropertyCascader interface {
	Suspend(ctx context.Context, adminID, propertyID uuid.UUID) error
}

// BookingCascader is the narrow port the user service uses to
// auto-cancel confirmed bookings on a suspended host's listings (S169).
// The host-cancel path of bookingapp.Cancel returns 100% refund per
// the existing policy; the cascade passes hostID as actor so the same
// path runs (the suspended host is, in effect, the one cancelling).
// Implemented by *bookingapp.Service.Cancel at the composition root.
type BookingCascader interface {
	Cancel(ctx context.Context, actorID, bookingID uuid.UUID) error
}

// CascadeAuditor is the narrow port the user service uses to append a
// single ActionUserSuspendedCascadeApplied row summarising the cascade
// (S169). Implemented by *auditapp.Service.Record at the composition
// root via the same shim pattern as cohostAuditor / bookingAuditor.
type CascadeAuditor interface {
	Record(ctx context.Context, in CascadeAuditInput) error
}

// CascadeAuditInput mirrors auditapp.RecordInput at the userapp
// boundary so this package does not import auditapp directly.
type CascadeAuditInput struct {
	ActorID    uuid.UUID
	Action     audit.Action
	TargetType audit.TargetType
	TargetID   uuid.UUID
	Metadata   map[string]any
}

// HostListingLister is the narrow port the user service uses to walk
// every listing owned by a host during the cascade (S169). Mirrors
// property.Repository.ListByHost; declared here so userapp does not
// import the property repository directly. main.go adapts the
// concrete repo via a small shim.
type HostListingLister interface {
	IDsByHost(ctx context.Context, hostID uuid.UUID) ([]uuid.UUID, error)
}

// Service orchestrates user use cases.
type Service struct {
	repo user.Repository
	// Cascade dependencies (S169). All four must be wired together via
	// WithCascade for the cascade arm to fire; any nil short-circuits to
	// the legacy "just flip IsActive" behaviour so older wiring and
	// existing tests keep working unchanged.
	props        PropertyCascader
	bookings     BookingCascader
	bookingRepo  booking.Repository
	listings     HostListingLister
	cascadeAudit CascadeAuditor
}

// NewService wires the user application service.
func NewService(repo user.Repository) *Service {
	return &Service{repo: repo}
}

// WithCascade enables the host-suspension cascade (S169). Wiring all
// four deps switches Suspend into cascade mode: auto-suspend every
// listing owned by the kicked host, auto-cancel every confirmed
// booking on those listings via the host-cancel path (100% refund),
// then append a single ActionUserSuspendedCascadeApplied summary row.
// Both downstream arms are best-effort — a failure on one listing or
// booking does NOT abort the whole cascade; the failed ids land in
// the audit row's metadata.errors slice. Optional auditor is wired
// separately via WithCascadeAuditor so tests can exercise the
// cascade without an audit sink.
func (s *Service) WithCascade(props PropertyCascader, bookings BookingCascader, bookingRepo booking.Repository, listings HostListingLister) *Service {
	s.props = props
	s.bookings = bookings
	s.bookingRepo = bookingRepo
	s.listings = listings
	return s
}

// WithCascadeAuditor wires the audit sink that receives the single
// summary row when the cascade fires (S169). Optional — nil keeps the
// cascade running but silent on the audit trail (the per-listing
// property.suspended rows still land via propertyapp's own auditor).
func (s *Service) WithCascadeAuditor(a CascadeAuditor) *Service {
	s.cascadeAudit = a
	return s
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

// List returns users matching filter (S65). Admin-only — the route gate
// enforces that; this service just delegates to the repo.
func (s *Service) List(ctx context.Context, f user.ListFilter, page shared.Page) (shared.PageResult[*user.User], error) {
	return s.repo.List(ctx, f, page)
}

// Suspend disables a user account (S61). Admin-only — the route gate enforces
// that; this service just performs the state change. Returns the updated user.
// A no-op (already inactive) returns the current state without error so a
// retry doesn't fail; the audit hook in the handler is keyed on a successful
// admin call rather than a state transition.
//
// S169 — when cascade deps are wired (WithCascade), a successful state
// transition fans out: every listing owned by the kicked host is
// auto-suspended (so it stops accepting new bookings) and every
// CONFIRMED booking on those listings is auto-cancelled via the host-
// cancel path (100% refund per the existing policy). A single
// ActionUserSuspendedCascadeApplied audit row summarises the result.
// Both downstream arms are best-effort — a failure on one listing or
// booking does NOT abort the whole cascade. A no-op suspend (already
// inactive) skips the cascade entirely so a retry doesn't re-fire
// cancellations that already ran.
//
// adminID is the user who initiated the suspension. It's threaded into
// the property-level audit rows (so the per-listing trail attributes
// the state change to the same human) and into the cascade summary row.
func (s *Service) Suspend(ctx context.Context, adminID, id uuid.UUID) (*user.User, error) {
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
	// Legacy path: no cascade wired. Older tests and deployments that
	// don't run the cascade arm keep the original two-line semantics.
	if s.props == nil || s.bookings == nil || s.bookingRepo == nil || s.listings == nil {
		return u, nil
	}
	s.runCascade(ctx, adminID, id)
	return u, nil
}

// runCascade applies the S169 cascade after a host has been
// successfully suspended. Both arms (listings, bookings) are best-
// effort: a failure on one item is captured in the summary audit
// row's metadata.errors slice and the cascade carries on, so a
// single bad listing can't block the rest of the host's footprint
// from being neutralised. The summary row is written at the end
// regardless of partial failure — its purpose is to record what
// actually happened, not to roll back.
func (s *Service) runCascade(ctx context.Context, adminID, hostID uuid.UUID) {
	var (
		listingsSuspended  int
		bookingsCancelled  int
		cascadeErrors      []string
	)

	// 1. Auto-suspend every listing owned by the host so no new
	//    booking can land on a property whose owner has been kicked.
	listingIDs, err := s.listings.IDsByHost(ctx, hostID)
	if err != nil {
		cascadeErrors = append(cascadeErrors, fmt.Sprintf("list-listings: %v", err))
	}
	for _, propID := range listingIDs {
		if err := s.props.Suspend(ctx, adminID, propID); err != nil {
			cascadeErrors = append(cascadeErrors, fmt.Sprintf("suspend-listing %s: %v", propID, err))
			continue
		}
		listingsSuspended++
	}

	// 2. Auto-cancel confirmed bookings on the host's listings via
	//    the host-cancel path. Passing hostID as actor triggers the
	//    100% refund branch the existing bookingapp.Cancel implements,
	//    so the guest is made whole. CancelledBy in the event payload
	//    is hostID — semantically "the host cancelled (involuntarily,
	//    because the platform kicked them)".
	bks, err := s.bookingRepo.ListByHostStatus(ctx, hostID, booking.StatusConfirmed)
	if err != nil {
		cascadeErrors = append(cascadeErrors, fmt.Sprintf("list-bookings: %v", err))
	}
	for _, b := range bks {
		if err := s.bookings.Cancel(ctx, hostID, b.ID); err != nil {
			cascadeErrors = append(cascadeErrors, fmt.Sprintf("cancel-booking %s: %v", b.ID, err))
			continue
		}
		bookingsCancelled++
	}

	// 3. Single summary audit row. Optional sink — older wiring leaves
	//    s.cascadeAudit nil and the row is silently skipped (the per-
	//    listing property.suspended rows from propertyapp's own
	//    auditor still land). Errors are intentionally serialised as
	//    a flat string slice rather than structured objects so the
	//    metadata JSON shape stays grep-friendly for ops queries.
	if s.cascadeAudit == nil {
		return
	}
	meta := map[string]any{
		"listings_suspended": listingsSuspended,
		"bookings_cancelled": bookingsCancelled,
		"errors":             cascadeErrors,
	}
	_ = s.cascadeAudit.Record(ctx, CascadeAuditInput{
		ActorID:    adminID,
		Action:     audit.ActionUserSuspendedCascadeApplied,
		TargetType: audit.TargetUser,
		TargetID:   hostID,
		Metadata:   meta,
	})
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
