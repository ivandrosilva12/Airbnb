package tax

import (
	"sort"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// StayInput is the projection the calculator needs about a candidate
// stay. The fields are deliberately small so any caller — booking
// service, public quote endpoint, batch reconciliation — can produce
// one without dragging in the booking aggregate.
type StayInput struct {
	Country       string
	City          string
	CheckIn       time.Time
	Nights        int
	Guests        int
	SubtotalCents int64 // taxable base (typically subtotal + cleaning, NOT service fee)
	Currency      string
}

// Line is one tax row in a Quote. Kept human-friendly with a Name
// (rule name copy) so a UI can render the breakdown without joining
// back to the rule table.
type Line struct {
	RuleID      uuid.UUID
	Name        string
	Kind        Kind
	AmountCents int64
}

// Quote is the calculator's output: an itemised breakdown plus its
// sum. Lines are sorted alphabetically by Name for deterministic
// output (a regression in line ordering breaks UI snapshots).
type Quote struct {
	Lines      []Line
	TotalCents int64
	Currency   string
}

// Calculate sums every rule that matches the stay's jurisdiction and
// effective window. Each rule contributes exactly one Line; the order
// in the input slice is irrelevant (the output is name-sorted).
//
// Currency enforcement: a rule whose Currency differs from the stay's
// Currency is SKIPPED, not silently converted — a EUR rule applied to
// an AOA booking would be misleading, and the right answer (currency
// conversion + which exchange rate) is policy that belongs in the
// application layer, not here.
func Calculate(stay StayInput, rules []*Rule) (Quote, error) {
	if stay.Nights < 0 || stay.Guests < 0 || stay.SubtotalCents < 0 {
		return Quote{}, shared.NewValidationError("tax: stay inputs must be non-negative")
	}
	out := Quote{Currency: stay.Currency}
	for _, r := range rules {
		if r == nil {
			continue
		}
		if !r.Matches(stay.Country, stay.City, stay.CheckIn) {
			continue
		}
		if r.Currency != stay.Currency {
			// Currency mismatch is policy — see the comment above.
			continue
		}
		amount := r.applyTo(stay)
		if amount <= 0 {
			continue
		}
		out.Lines = append(out.Lines, Line{
			RuleID: r.ID, Name: r.Name, Kind: r.Kind, AmountCents: amount,
		})
		out.TotalCents += amount
	}
	sort.Slice(out.Lines, func(i, j int) bool { return out.Lines[i].Name < out.Lines[j].Name })
	return out, nil
}

// applyTo turns a single Rule into a per-stay amount in cents. The
// arithmetic stays in integer cents throughout — no float — so a
// repeated calculation always yields the bit-identical result, which
// matters for reconciliation against payment-provider records.
func (r *Rule) applyTo(stay StayInput) int64 {
	switch r.Kind {
	case KindPercent:
		// (subtotal * bips) / 10_000, integer-truncated. The bips
		// representation lets us encode fractional percents (e.g. 825 =
		// 8.25%) without floats.
		return stay.SubtotalCents * int64(r.RatePctBips) / 10000
	case KindPerNightPerGuest:
		nights := stay.Nights
		if r.MaxNights > 0 && nights > r.MaxNights {
			nights = r.MaxNights
		}
		return r.FlatAmountCents * int64(nights) * int64(stay.Guests)
	case KindPerStay:
		return r.FlatAmountCents
	}
	return 0
}
