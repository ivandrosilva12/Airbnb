// Package tax owns the per-jurisdiction tax rules a stay is subject
// to and the calculator that resolves a stay against the active rule
// set into an itemised Quote.
//
// This is the stub the rest of the codebase will gradually adopt
// (S48). The property aggregate's PricingPolicy.TaxRatePct stays put
// for now — it is a single flat per-listing percent that suits the
// initial deployments; the tax BC is the home for the more nuanced
// rules (Lisbon tourist tax, country-level VAT, per-stay convention
// fees) that a follow-up slice will plug into the booking pricing
// flow. The public quote endpoint already lets a UI preview those
// extras without changing the booking total.
package tax

import (
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Kind enumerates the calculation modes a Rule supports. Closed set —
// callers MUST pick one; adding a new kind means adding a case to
// every consumer (Kind.Valid, Rule.applyTo, the postgres encoder).
type Kind string

const (
	// KindPercent: a percentage of the taxable subtotal. Use for VAT
	// (Portugal national 23%), occupancy taxes that are a percent of the
	// room charge, etc. RatePctBips holds basis-points (2300 = 23%).
	KindPercent Kind = "percent"

	// KindPerNightPerGuest: a flat amount charged per night per guest.
	// Use for European-style tourist taxes (Lisbon 2 EUR/night/guest,
	// capped at 7 nights). MaxNights caps the chargeable nights; 0
	// means no cap.
	KindPerNightPerGuest Kind = "per_night_per_guest"

	// KindPerStay: a flat amount per booking regardless of nights or
	// guests. Use for convention/registration fees a city levies once
	// per stay.
	KindPerStay Kind = "per_stay"
)

// Valid reports whether k is a recognised kind. Zero-value Kind ("")
// is invalid — the constructor rejects it.
func (k Kind) Valid() bool {
	switch k {
	case KindPercent, KindPerNightPerGuest, KindPerStay:
		return true
	}
	return false
}

// Rule is one taxation directive scoped to a country (optionally a
// city). A booking accumulates every rule whose jurisdiction matches
// at the stay's CheckIn date.
//
// Matching rules:
//   - Country MUST match (case-insensitive). Country == "" is treated as
//     "applies everywhere" (rare; covers a hypothetical worldwide platform
//     surcharge).
//   - City matches by exact equality (case-insensitive). An empty City
//     means the rule applies to the whole country.
//   - EffectiveFrom and EffectiveUntil define a half-open window
//     [from, until). Either may be the zero value, meaning open-ended.
//
// Currency is part of the rule, not the booking — a per-night tax
// stated in EUR doesn't change because the booking happens to be
// priced in AOA; in practice the booking flow will reject any rule
// whose currency doesn't match the listing's currency, but the domain
// itself stays currency-strict so the discrepancy is visible.
type Rule struct {
	ID              uuid.UUID
	Name            string
	Kind            Kind
	Country         string
	City            string
	Currency        string
	RatePctBips     int // for KindPercent. 2300 = 23%.
	FlatAmountCents int64 // for KindPerNightPerGuest and KindPerStay.
	MaxNights       int   // for KindPerNightPerGuest. 0 = no cap.
	EffectiveFrom   time.Time
	EffectiveUntil  time.Time
}

// NewRule validates inputs and returns a Rule. Each Kind has its own
// required fields — the constructor enforces "the right knob for the
// right kind" so a Percent rule with a flat amount, or a PerStay rule
// with rateBips, fails loud at construction rather than producing a
// silent zero-tax line later.
func NewRule(name string, kind Kind, country, city, currency string, rateBips int, flatCents int64, maxNights int, from, until time.Time) (*Rule, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, shared.NewValidationError("tax: name is required")
	}
	if !kind.Valid() {
		return nil, shared.NewValidationError("tax: unknown kind " + string(kind))
	}
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if len(currency) != 3 {
		return nil, shared.NewValidationError("tax: currency must be a 3-letter ISO code")
	}
	switch kind {
	case KindPercent:
		if rateBips <= 0 {
			return nil, shared.NewValidationError("tax: percent rule needs rateBips > 0")
		}
		if flatCents != 0 || maxNights != 0 {
			return nil, shared.NewValidationError("tax: percent rule must not set flat/maxNights")
		}
	case KindPerNightPerGuest:
		if flatCents <= 0 {
			return nil, shared.NewValidationError("tax: per-night-per-guest rule needs flatCents > 0")
		}
		if rateBips != 0 {
			return nil, shared.NewValidationError("tax: per-night-per-guest rule must not set rateBips")
		}
		if maxNights < 0 {
			return nil, shared.NewValidationError("tax: maxNights cannot be negative")
		}
	case KindPerStay:
		if flatCents <= 0 {
			return nil, shared.NewValidationError("tax: per-stay rule needs flatCents > 0")
		}
		if rateBips != 0 || maxNights != 0 {
			return nil, shared.NewValidationError("tax: per-stay rule must not set rateBips/maxNights")
		}
	}
	if !from.IsZero() && !until.IsZero() && !until.After(from) {
		return nil, shared.NewValidationError("tax: effectiveUntil must be after effectiveFrom")
	}
	return &Rule{
		ID:              uuid.New(),
		Name:            name,
		Kind:            kind,
		Country:         strings.ToUpper(strings.TrimSpace(country)),
		City:            strings.TrimSpace(city),
		Currency:        currency,
		RatePctBips:     rateBips,
		FlatAmountCents: flatCents,
		MaxNights:       maxNights,
		EffectiveFrom:   from,
		EffectiveUntil:  until,
	}, nil
}

// Matches reports whether the rule applies to a stay in (country,
// city) at the given check-in date. Used by the calculator to filter
// the candidate set; also reusable by the repository for an indexed
// pre-filter at SQL level.
func (r *Rule) Matches(country, city string, at time.Time) bool {
	if r == nil {
		return false
	}
	if r.Country != "" && !strings.EqualFold(r.Country, country) {
		return false
	}
	if r.City != "" && !strings.EqualFold(r.City, city) {
		return false
	}
	if !r.EffectiveFrom.IsZero() && at.Before(r.EffectiveFrom) {
		return false
	}
	if !r.EffectiveUntil.IsZero() && !at.Before(r.EffectiveUntil) {
		return false
	}
	return true
}
