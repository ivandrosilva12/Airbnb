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

// Service orchestrates price-rule use cases.
type Service struct {
	rules      pricerule.Repository
	properties property.Repository
}

// NewService wires the price-rule application service.
func NewService(rules pricerule.Repository, properties property.Repository) *Service {
	return &Service{rules: rules, properties: properties}
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

// Create adds a price rule on a listing the host owns. The new rule must not
// overlap any existing rule on the same property (host can delete first then
// re-add if they want to replace a range).
func (s *Service) Create(ctx context.Context, in CreateInput) (*pricerule.Rule, error) {
	prop, err := s.properties.FindByID(ctx, in.PropertyID)
	if err != nil {
		return nil, err
	}
	if !prop.IsOwnedBy(in.HostID) {
		return nil, shared.ErrForbidden
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

// ListForHost returns every price rule attached to a listing the host owns.
func (s *Service) ListForHost(ctx context.Context, hostID, propertyID uuid.UUID) ([]*pricerule.Rule, error) {
	prop, err := s.properties.FindByID(ctx, propertyID)
	if err != nil {
		return nil, err
	}
	if !prop.IsOwnedBy(hostID) {
		return nil, shared.ErrForbidden
	}
	return s.rules.ListByProperty(ctx, propertyID)
}

// Delete removes a price rule on a listing the host owns.
func (s *Service) Delete(ctx context.Context, hostID, propertyID, ruleID uuid.UUID) error {
	prop, err := s.properties.FindByID(ctx, propertyID)
	if err != nil {
		return err
	}
	if !prop.IsOwnedBy(hostID) {
		return shared.ErrForbidden
	}
	return s.rules.Delete(ctx, propertyID, ruleID)
}
