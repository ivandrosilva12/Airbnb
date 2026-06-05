// Package propertyapp contains listing-related use cases.
package propertyapp

import (
	"context"
	"fmt"
	"io"
	"path"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/audit"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// IdentityVerifier reports whether a user has a verified identity. The property
// service consults it when KYC gating is enabled, before a host publishes.
type IdentityVerifier interface {
	IsVerified(ctx context.Context, userID uuid.UUID) (bool, error)
}

// Service orchestrates property use cases.
type Service struct {
	repo       property.Repository
	storage    port.Storage
	identity   IdentityVerifier
	requireKYC bool
	// auditor, when wired via WithAuditor, appends a compliance row on
	// every successful admin Suspend / Unsuspend (S124). Reuses the
	// package-local Auditor sink declared alongside the cohost service
	// (cohost_service.go), so the application/property package stays
	// free of auditapp coupling — the composition root in main.go
	// adapts auditapp.Service into propertyapp.Auditor via a thin
	// shim. Optional: nil keeps existing tests / wiring working with no
	// audit side-effect.
	auditor Auditor
}

// NewService wires the property application service. When requireKYC is true, a
// host must have a verified identity (per the IdentityVerifier) before
// publishing a listing.
func NewService(repo property.Repository, storage port.Storage, identity IdentityVerifier, requireKYC bool) *Service {
	return &Service{repo: repo, storage: storage, identity: identity, requireKYC: requireKYC}
}

// WithAuditor plugs an Auditor so a successful Suspend / Unsuspend
// appends a "property.suspended" / "property.unsuspended" audit row
// alongside the repo mutation (S124). Fluent setter; pre-existing
// call-sites keep working and the Suspend / Unsuspend paths
// short-circuit when no auditor is wired.
func (s *Service) WithAuditor(a Auditor) *Service {
	s.auditor = a
	return s
}

// CreateInput carries the data required to create a listing.
type CreateInput struct {
	HostID               uuid.UUID
	Title                string
	Description          string
	Type                 string
	AddressLine1         string
	City                 string
	Country              string
	PostalCode           string
	Latitude             float64
	Longitude            float64
	PriceCents           int64
	CleaningFeeCents     int64
	Currency             string
	MaxGuests            int
	Bedrooms             int
	Beds                 int
	Bathrooms            int
	Amenities            []string
	CancellationPolicy   string
	WeeklyDiscountPct    float64
	MonthlyDiscountPct   float64
	TaxRatePct           float64
	WeekendPriceCents    int64
	InstantBook          bool
	MinNights            int
	MaxNights            int
	GuestsIncluded       int
	ExtraGuestFeeCents   int64
	SecurityDepositCents int64
	CheckInMethod        string
	ArrivalInstructions  string
	WifiSSID             string
	WifiPassword         string
}

// applyStayRules sets the listing's stay-length limits and per-guest pricing,
// filling sensible defaults (min 1 night, all guests included) when unset.
func applyStayRules(p *property.Property, currency string, minNights, maxNights, guestsIncluded int, extraGuestFeeCents, securityDepositCents int64) error {
	if minNights < 1 {
		minNights = 1
	}
	if guestsIncluded < 1 {
		guestsIncluded = p.MaxGuests
	}
	extraGuestFee, err := shared.NewMoney(extraGuestFeeCents, currency)
	if err != nil {
		return err
	}
	deposit, err := shared.NewMoney(securityDepositCents, currency)
	if err != nil {
		return err
	}
	return p.SetStayRules(minNights, maxNights, guestsIncluded, extraGuestFee, deposit)
}

// Create builds and persists a draft listing.
func (s *Service) Create(ctx context.Context, in CreateInput) (*property.Property, error) {
	price, err := shared.NewMoney(in.PriceCents, in.Currency)
	if err != nil {
		return nil, err
	}
	cleaningFee, err := shared.NewMoney(in.CleaningFeeCents, in.Currency)
	if err != nil {
		return nil, err
	}
	addr := property.Address{
		Line1:      in.AddressLine1,
		City:       in.City,
		Country:    in.Country,
		PostalCode: in.PostalCode,
		Latitude:   in.Latitude,
		Longitude:  in.Longitude,
	}
	p, err := property.NewProperty(
		in.HostID, in.Title, in.Description, property.PropertyType(in.Type),
		addr, price, cleaningFee, in.MaxGuests, in.Bedrooms, in.Beds, in.Bathrooms, in.Amenities,
	)
	if err != nil {
		return nil, err
	}
	if in.CancellationPolicy != "" {
		p.SetCancellationPolicy(property.CancellationPolicy(in.CancellationPolicy))
	}
	p.SetPricingPolicy(property.PricingPolicy{
		WeeklyDiscountPct:  in.WeeklyDiscountPct,
		MonthlyDiscountPct: in.MonthlyDiscountPct,
		TaxRatePct:         in.TaxRatePct,
		WeekendPriceCents:  in.WeekendPriceCents,
	})
	p.SetInstantBook(in.InstantBook)
	if err := applyStayRules(p, in.Currency, in.MinNights, in.MaxNights, in.GuestsIncluded, in.ExtraGuestFeeCents, in.SecurityDepositCents); err != nil {
		return nil, err
	}
	if err := p.SetArrivalInfo(property.ArrivalInfo{
		CheckInMethod: property.CheckInMethod(in.CheckInMethod),
		Instructions:  in.ArrivalInstructions,
		WifiSSID:      in.WifiSSID,
		WifiPassword:  in.WifiPassword,
	}); err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// GetByID fetches a single listing.
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (*property.Property, error) {
	return s.repo.FindByID(ctx, id)
}

// Search runs a filtered listing search.
func (s *Service) Search(ctx context.Context, c property.SearchCriteria) (shared.PageResult[*property.Property], error) {
	return s.repo.Search(ctx, c)
}

// ListByHost returns the listings owned by a host.
func (s *Service) ListByHost(ctx context.Context, hostID uuid.UUID, page shared.Page) (shared.PageResult[*property.Property], error) {
	return s.repo.ListByHost(ctx, hostID, page)
}

// UpdateInput carries editable listing fields.
type UpdateInput struct {
	Title                string
	Description          string
	PriceCents           int64
	CleaningFeeCents     int64
	Currency             string
	MaxGuests            int
	CancellationPolicy   string
	WeeklyDiscountPct    float64
	MonthlyDiscountPct   float64
	TaxRatePct           float64
	WeekendPriceCents    int64
	InstantBook          bool
	MinNights            int
	MaxNights            int
	GuestsIncluded       int
	ExtraGuestFeeCents   int64
	SecurityDepositCents int64
	CheckInMethod        string
	ArrivalInstructions  string
	WifiSSID             string
	WifiPassword         string
}

// Duplicate clones a host's listing into a fresh draft (S60). The clone copies
// description, type, address, pricing, policy, stay rules and amenities, but:
//   - title gets " (copy)" appended so the host can disambiguate before saving;
//   - status is reset to draft (the host must publish explicitly);
//   - photos and arrival info are NOT copied — photos live behind property-scoped
//     object keys we can't safely re-reference, and arrival credentials are
//     sensitive enough that the host should restate them per listing;
//   - rating/review aggregates and Superhost-fanout flags reset.
//
// Only the owning host may duplicate; ErrForbidden otherwise.
func (s *Service) Duplicate(ctx context.Context, actorID, propertyID uuid.UUID) (*property.Property, error) {
	src, err := s.ownedProperty(ctx, actorID, propertyID)
	if err != nil {
		return nil, err
	}
	copyTitle := src.Title + " (copy)"
	if len(copyTitle) > 200 { // keep within any title bound; "(copy)" is 6 chars + space
		copyTitle = copyTitle[:200]
	}
	dup, err := property.NewProperty(
		src.HostID, copyTitle, src.Description, src.Type,
		src.Address, src.PricePerNight, src.CleaningFee,
		src.MaxGuests, src.Bedrooms, src.Beds, src.Bathrooms, src.Amenities,
	)
	if err != nil {
		return nil, err
	}
	dup.SetCancellationPolicy(src.CancellationPolicy)
	dup.SetPricingPolicy(src.PricingPolicy)
	dup.SetInstantBook(src.InstantBook)
	if err := applyStayRules(dup, src.PricePerNight.Currency(),
		src.MinNights, src.MaxNights, src.GuestsIncluded,
		src.ExtraGuestFee.AmountCents(), src.SecurityDeposit.AmountCents()); err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, dup); err != nil {
		return nil, err
	}
	return dup, nil
}

// Update mutates a listing after verifying ownership.
func (s *Service) Update(ctx context.Context, actorID, propertyID uuid.UUID, in UpdateInput) (*property.Property, error) {
	p, err := s.ownedProperty(ctx, actorID, propertyID)
	if err != nil {
		return nil, err
	}
	price, err := shared.NewMoney(in.PriceCents, in.Currency)
	if err != nil {
		return nil, err
	}
	cleaningFee, err := shared.NewMoney(in.CleaningFeeCents, in.Currency)
	if err != nil {
		return nil, err
	}
	if err := p.UpdateDetails(in.Title, in.Description, price, cleaningFee, in.MaxGuests); err != nil {
		return nil, err
	}
	if in.CancellationPolicy != "" {
		p.SetCancellationPolicy(property.CancellationPolicy(in.CancellationPolicy))
	}
	p.SetPricingPolicy(property.PricingPolicy{
		WeeklyDiscountPct:  in.WeeklyDiscountPct,
		MonthlyDiscountPct: in.MonthlyDiscountPct,
		TaxRatePct:         in.TaxRatePct,
		WeekendPriceCents:  in.WeekendPriceCents,
	})
	p.SetInstantBook(in.InstantBook)
	if err := applyStayRules(p, in.Currency, in.MinNights, in.MaxNights, in.GuestsIncluded, in.ExtraGuestFeeCents, in.SecurityDepositCents); err != nil {
		return nil, err
	}
	if err := p.SetArrivalInfo(property.ArrivalInfo{
		CheckInMethod: property.CheckInMethod(in.CheckInMethod),
		Instructions:  in.ArrivalInstructions,
		WifiSSID:      in.WifiSSID,
		WifiPassword:  in.WifiPassword,
	}); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// Publish publishes a listing after verifying ownership.
func (s *Service) Publish(ctx context.Context, actorID, propertyID uuid.UUID) (*property.Property, error) {
	p, err := s.ownedProperty(ctx, actorID, propertyID)
	if err != nil {
		return nil, err
	}
	if s.requireKYC {
		verified, err := s.identity.IsVerified(ctx, actorID)
		if err != nil {
			return nil, err
		}
		if !verified {
			return nil, shared.NewValidationError("verify your identity before publishing a listing")
		}
	}
	if err := p.Publish(); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// UploadPhoto stores an uploaded image and attaches it to the listing.
func (s *Service) UploadPhoto(
	ctx context.Context,
	actorID, propertyID uuid.UUID,
	r io.Reader, size int64, contentType, filename string,
) (*property.Property, error) {
	p, err := s.ownedProperty(ctx, actorID, propertyID)
	if err != nil {
		return nil, err
	}
	objectKey := fmt.Sprintf("properties/%s/%s%s", propertyID, uuid.NewString(), path.Ext(filename))
	url, err := s.storage.Upload(ctx, objectKey, r, size, contentType)
	if err != nil {
		return nil, err
	}
	p.AddPhoto(objectKey, url)
	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// RemovePhoto detaches a photo from an owned listing. The stored object is left
// in place (the storage port is write-only); a janitor could reap orphans later.
func (s *Service) RemovePhoto(ctx context.Context, actorID, propertyID, photoID uuid.UUID) (*property.Property, error) {
	p, err := s.ownedProperty(ctx, actorID, propertyID)
	if err != nil {
		return nil, err
	}
	if err := p.RemovePhoto(photoID); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// ReorderPhotos sets the photo order (and thus the cover) for an owned listing.
func (s *Service) ReorderPhotos(ctx context.Context, actorID, propertyID uuid.UUID, orderedIDs []uuid.UUID) (*property.Property, error) {
	p, err := s.ownedProperty(ctx, actorID, propertyID)
	if err != nil {
		return nil, err
	}
	if err := p.ReorderPhotos(orderedIDs); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// Delete removes a listing after verifying ownership.
func (s *Service) Delete(ctx context.Context, actorID, propertyID uuid.UUID) error {
	if _, err := s.ownedProperty(ctx, actorID, propertyID); err != nil {
		return err
	}
	return s.repo.Delete(ctx, propertyID)
}

// Suspend hides a listing from search. Intended for platform admins (the
// caller is responsible for enforcing the admin role), so no ownership check.
// adminID is the acting admin's user id; it rides into the audit row when
// an Auditor is wired (S124). A nil auditor keeps the path side-effect free.
func (s *Service) Suspend(ctx context.Context, adminID, propertyID uuid.UUID) (*property.Property, error) {
	p, err := s.repo.FindByID(ctx, propertyID)
	if err != nil {
		return nil, err
	}
	p.Suspend()
	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	// S124 — append the compliance row after the repo mutation lands.
	// Mirrors the cohost_service S120 pattern: the audit hook sits at
	// the application layer (not the HTTP boundary), so the trail is
	// written next to the state change even if a future caller invokes
	// Suspend from somewhere other than the admin HTTP handler.
	if s.auditor != nil {
		if err := s.auditor.Record(ctx, AuditRecord{
			ActorID:    adminID,
			Action:     audit.ActionPropertySuspended,
			TargetType: audit.TargetProperty,
			TargetID:   propertyID,
			Metadata:   map[string]any{"title": p.Title},
		}); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// Unsuspend restores a suspended listing to published (admin action).
// adminID is the acting admin's user id; it rides into the audit row when
// an Auditor is wired (S124).
func (s *Service) Unsuspend(ctx context.Context, adminID, propertyID uuid.UUID) (*property.Property, error) {
	p, err := s.repo.FindByID(ctx, propertyID)
	if err != nil {
		return nil, err
	}
	if err := p.Unsuspend(); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	if s.auditor != nil {
		if err := s.auditor.Record(ctx, AuditRecord{
			ActorID:    adminID,
			Action:     audit.ActionPropertyUnsuspended,
			TargetType: audit.TargetProperty,
			TargetID:   propertyID,
			Metadata:   map[string]any{"title": p.Title},
		}); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (s *Service) ownedProperty(ctx context.Context, actorID, propertyID uuid.UUID) (*property.Property, error) {
	p, err := s.repo.FindByID(ctx, propertyID)
	if err != nil {
		return nil, err
	}
	if !p.IsOwnedBy(actorID) {
		return nil, shared.ErrForbidden
	}
	return p, nil
}
