// Package taxapp wires the tax domain. Its two use cases are the
// admin-facing rule CRUD (List/Create/Delete) and the public
// QuoteForProperty that resolves a property's jurisdiction and runs
// the calculator against the active rule set.
package taxapp

import (
	"context"
	"time"

	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/tax"
	"github.com/google/uuid"
)

// Service holds the use cases for the tax BC.
type Service struct {
	rules      tax.Repository
	properties property.Repository
}

// NewService wires the service. The property repo is required so the
// public quote endpoint can resolve the listing's (country, city) to
// pick the applicable rules without forcing callers to pass them
// explicitly (and risk mismatch with the listing's metadata).
func NewService(rules tax.Repository, properties property.Repository) *Service {
	return &Service{rules: rules, properties: properties}
}

// List returns every rule. Admin-only at the handler boundary.
func (s *Service) List(ctx context.Context) ([]*tax.Rule, error) {
	return s.rules.List(ctx)
}

// CreateInput carries the host-authored rule data. Mirrors the
// domain constructor's args so the handler can pass it straight
// through after parsing the request body.
type CreateInput struct {
	Name            string
	Kind            tax.Kind
	Country, City   string
	Currency        string
	RatePctBips     int
	FlatAmountCents int64
	MaxNights       int
	EffectiveFrom   time.Time
	EffectiveUntil  time.Time
}

// Create validates and persists a rule. The domain constructor does
// the heavy lifting (the kind-specific knob checks); Save lifts any
// repo-level errors (e.g. unique-constraint conflicts in postgres).
func (s *Service) Create(ctx context.Context, in CreateInput) (*tax.Rule, error) {
	r, err := tax.NewRule(in.Name, in.Kind, in.Country, in.City, in.Currency, in.RatePctBips, in.FlatAmountCents, in.MaxNights, in.EffectiveFrom, in.EffectiveUntil)
	if err != nil {
		return nil, err
	}
	if err := s.rules.Save(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

// Delete removes a rule by ID. Idempotent at the repo level —
// deleting a missing rule is not an error.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.rules.Delete(ctx, id)
}

// QuoteInput is what the public quote endpoint receives, after the
// handler validated the query string.
type QuoteInput struct {
	PropertyID    uuid.UUID
	CheckIn       time.Time
	Nights        int
	Guests        int
	SubtotalCents int64
}

// QuoteForProperty resolves the property's jurisdiction, picks the
// rules that apply at CheckIn, runs the calculator, and returns the
// itemised Quote. The endpoint is public so a UI can render the
// breakdown before the guest is signed in — the calculator is pure,
// no state mutation happens here.
func (s *Service) QuoteForProperty(ctx context.Context, in QuoteInput) (tax.Quote, error) {
	prop, err := s.properties.FindByID(ctx, in.PropertyID)
	if err != nil {
		return tax.Quote{}, err
	}
	if in.Nights <= 0 {
		return tax.Quote{}, shared.NewValidationError("tax: nights must be > 0")
	}
	if in.Guests <= 0 {
		return tax.Quote{}, shared.NewValidationError("tax: guests must be > 0")
	}
	rules, err := s.rules.RulesFor(ctx, prop.Address.Country, prop.Address.City, in.CheckIn)
	if err != nil {
		return tax.Quote{}, err
	}
	return tax.Calculate(tax.StayInput{
		Country:       prop.Address.Country,
		City:          prop.Address.City,
		CheckIn:       in.CheckIn,
		Nights:        in.Nights,
		Guests:        in.Guests,
		SubtotalCents: in.SubtotalCents,
		Currency:      prop.PricePerNight.Currency(),
	}, rules)
}
