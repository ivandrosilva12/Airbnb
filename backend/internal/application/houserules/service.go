// Package houserulesapp orchestrates the house-rules use cases:
// reading the current rule set, editing it (host), and recording the
// per-booking acceptance proof.
//
// The package also satisfies the booking BC's HouseRulesVerifier port
// (see bookingapp.HouseRulesVerifier) so the booking service can ask
// "is this version still current?" without importing the houserules
// domain directly. That keeps the dependency arrow one-way: bookings
// depend on a small port, the port is implemented here.
package houserulesapp

import (
	"context"
	"errors"

	"github.com/airhost/backend/internal/domain/houserules"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Service holds the use cases for house rules.
type Service struct {
	rules      houserules.Repository
	properties property.Repository
}

// NewService wires the service with its repositories. The property
// repo is used to enforce host-only edits at the application layer
// (rather than threading host-id checks through the handler).
func NewService(rules houserules.Repository, properties property.Repository) *Service {
	return &Service{rules: rules, properties: properties}
}

// Get returns the current Rules for a property. A property that has
// never been given rules returns (nil, nil) — the caller renders an
// empty list rather than treating absence as an error.
func (s *Service) Get(ctx context.Context, propertyID uuid.UUID) (*houserules.Rules, error) {
	r, err := s.rules.GetCurrent(ctx, propertyID)
	if errors.Is(err, shared.ErrNotFound) {
		return nil, nil
	}
	return r, err
}

// SetInput carries the host-authored rule set to persist.
type SetInput struct {
	HostID     uuid.UUID
	PropertyID uuid.UUID
	Items      []string
}

// Set authors or bumps the rule set. The first Save creates version 1;
// subsequent Sets create version+1 and KEEP the prior rows for the
// audit trail. The host check happens here (not in the handler) so the
// invariant is enforced uniformly across all callers.
func (s *Service) Set(ctx context.Context, in SetInput) (*houserules.Rules, error) {
	prop, err := s.properties.FindByID(ctx, in.PropertyID)
	if err != nil {
		return nil, err
	}
	if prop.HostID != in.HostID {
		return nil, shared.ErrForbidden
	}
	current, err := s.rules.GetCurrent(ctx, in.PropertyID)
	if err != nil && !errors.Is(err, shared.ErrNotFound) {
		return nil, err
	}
	var next *houserules.Rules
	if current == nil {
		next, err = houserules.New(in.PropertyID, in.Items)
	} else {
		next, err = current.Bump(in.Items)
	}
	if err != nil {
		return nil, err
	}
	if err := s.rules.Save(ctx, next); err != nil {
		return nil, err
	}
	return next, nil
}

// RequiresAcceptanceFor implements bookingapp.HouseRulesVerifier. It
// returns the version a guest must acknowledge before booking, or 0
// when the listing has no active rule set (the booking flow then
// skips the gate entirely).
func (s *Service) RequiresAcceptanceFor(ctx context.Context, propertyID uuid.UUID) (int, error) {
	r, err := s.rules.GetCurrent(ctx, propertyID)
	if errors.Is(err, shared.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !r.Active() {
		return 0, nil
	}
	return r.Version, nil
}

// RecordAcceptance persists the per-booking proof. Called from the
// booking service AFTER the booking is committed — idempotent on
// bookingID so a retry doesn't double-write.
func (s *Service) RecordAcceptance(ctx context.Context, bookingID, guestID, propertyID uuid.UUID, version int) error {
	acc, err := houserules.NewAcceptance(bookingID, guestID, propertyID, version)
	if err != nil {
		return err
	}
	return s.rules.RecordAcceptance(ctx, acc)
}

// AcceptanceView is the read-shaped result returned by the
// /bookings/:id/house-rules-acceptance handler: it bundles the
// acceptance row with the rule items the guest actually saw, so the
// caller renders the full proof in one round-trip.
type AcceptanceView struct {
	Acceptance *houserules.Acceptance
	Rules      *houserules.Rules
}

// GetAcceptance returns the acceptance + the matching versioned rules
// for a booking. Returns ErrNotFound when no acceptance exists for
// the booking (host had no rules, or recording failed and was not
// retried — the host can then choose to accept the gap or pursue it).
func (s *Service) GetAcceptance(ctx context.Context, bookingID uuid.UUID) (*AcceptanceView, error) {
	acc, err := s.rules.AcceptanceFor(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	rules, err := s.rules.GetVersion(ctx, acc.PropertyID, acc.AcceptedVersion)
	if err != nil {
		return nil, err
	}
	return &AcceptanceView{Acceptance: acc, Rules: rules}, nil
}
