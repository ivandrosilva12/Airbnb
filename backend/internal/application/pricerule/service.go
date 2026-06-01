// Package priceruleapp contains seasonal/per-date pricing use cases: a host
// adds, lists, and removes price overrides for date ranges on a listing they
// own.
package priceruleapp

import (
	"context"
	"time"

	"github.com/airhost/backend/internal/domain/pricerule"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// CohostAuthorizer is the relaxed permission gate used to admit a co-host
// with PermManagePricing on a listing they don't own. The pricerule service
// defers to it when set; otherwise it falls back to strict ownership.
type CohostAuthorizer interface {
	Authorize(ctx context.Context, actorID, propertyID uuid.UUID, required property.CohostPermission) (*property.Property, error)
}

// Service orchestrates price-rule use cases.
type Service struct {
	rules      pricerule.Repository
	properties property.Repository
	cohosts    CohostAuthorizer
}

// NewService wires the price-rule application service.
func NewService(rules pricerule.Repository, properties property.Repository) *Service {
	return &Service{rules: rules, properties: properties}
}

// WithCohosts plugs in the relaxed gate so co-hosts with manage_pricing may
// add/list/remove rules. Returns the receiver for chaining.
func (s *Service) WithCohosts(c CohostAuthorizer) *Service {
	s.cohosts = c
	return s
}

// access resolves the calling user to the listing they're acting on, honouring
// the co-host gate when configured. The strict path (ownership only) is used
// when no authorizer was wired — preserves the pre-S17 behaviour.
func (s *Service) access(ctx context.Context, actorID, propertyID uuid.UUID) (*property.Property, error) {
	if s.cohosts != nil {
		return s.cohosts.Authorize(ctx, actorID, propertyID, property.PermManagePricing)
	}
	prop, err := s.properties.FindByID(ctx, propertyID)
	if err != nil {
		return nil, err
	}
	if !prop.IsOwnedBy(actorID) {
		return nil, shared.ErrForbidden
	}
	return prop, nil
}

// CreateInput carries the data required to add a price rule to a listing.
type CreateInput struct {
	HostID     uuid.UUID
	PropertyID uuid.UUID
	StartDate  time.Time
	EndDate    time.Time
	PriceCents int64
	Label      string
}

// Create adds a price rule on a listing the caller may manage (primary host
// or co-host with manage_pricing). The new rule must not overlap any existing
// rule on the same property (caller can delete first then re-add if they want
// to replace a range).
func (s *Service) Create(ctx context.Context, in CreateInput) (*pricerule.Rule, error) {
	prop, err := s.access(ctx, in.HostID, in.PropertyID)
	if err != nil {
		return nil, err
	}
	rule, err := pricerule.New(in.PropertyID, in.StartDate, in.EndDate, in.PriceCents, prop.PricePerNight.Currency(), in.Label)
	if err != nil {
		return nil, err
	}
	existing, err := s.rules.ListOverlapping(ctx, in.PropertyID, rule.StartDate, rule.EndDate)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return nil, shared.NewValidationError("this date range overlaps an existing price rule")
	}
	if err := s.rules.Create(ctx, rule); err != nil {
		return nil, err
	}
	return rule, nil
}

// ListForHost returns every price rule attached to a listing the caller may
// manage (primary host or co-host with manage_pricing).
func (s *Service) ListForHost(ctx context.Context, hostID, propertyID uuid.UUID) ([]*pricerule.Rule, error) {
	if _, err := s.access(ctx, hostID, propertyID); err != nil {
		return nil, err
	}
	return s.rules.ListByProperty(ctx, propertyID)
}

// Delete removes a price rule on a listing the caller may manage (primary
// host or co-host with manage_pricing).
func (s *Service) Delete(ctx context.Context, hostID, propertyID, ruleID uuid.UUID) error {
	if _, err := s.access(ctx, hostID, propertyID); err != nil {
		return err
	}
	return s.rules.Delete(ctx, propertyID, ruleID)
}
