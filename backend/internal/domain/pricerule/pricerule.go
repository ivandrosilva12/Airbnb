// Package pricerule is the bounded context for date-range nightly-price
// overrides on a listing (seasonal pricing). The listing's base nightly price
// is used when no rule covers a night; rules are then layered with the
// listing's weekend price (set on the property aggregate) as a fallback.
package pricerule

import (
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

const maxLabelLen = 60

// Rule is a date-range override for a listing's nightly price. The range is
// half-open: [StartDate, EndDate), so back-to-back rules don't overlap on the
// boundary day.
type Rule struct {
	ID         uuid.UUID
	PropertyID uuid.UUID
	StartDate  time.Time
	EndDate    time.Time
	PriceCents int64
	Currency   string
	Label      string // optional, e.g. "Christmas"
	CreatedAt  time.Time
}

// New creates a price rule, validating its dates and price.
func New(propertyID uuid.UUID, start, end time.Time, priceCents int64, currency, label string) (*Rule, error) {
	if propertyID == uuid.Nil {
		return nil, shared.NewValidationError("a price rule requires a property")
	}
	start, end = truncateDay(start), truncateDay(end)
	if !end.After(start) {
		return nil, shared.NewValidationError("end date must be after start date")
	}
	if priceCents <= 0 {
		return nil, shared.NewValidationError("override price must be positive")
	}
	if strings.TrimSpace(currency) == "" {
		return nil, shared.NewValidationError("currency is required")
	}
	label = strings.TrimSpace(label)
	if len([]rune(label)) > maxLabelLen {
		return nil, shared.NewValidationError("label is too long")
	}
	return &Rule{
		ID:         uuid.New(),
		PropertyID: propertyID,
		StartDate:  start,
		EndDate:    end,
		PriceCents: priceCents,
		Currency:   currency,
		Label:      label,
		CreatedAt:  time.Now().UTC(),
	}, nil
}

// AppliesOn reports whether the rule covers the given day.
func (r *Rule) AppliesOn(day time.Time) bool {
	d := truncateDay(day)
	return !d.Before(r.StartDate) && d.Before(r.EndDate)
}

func truncateDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
