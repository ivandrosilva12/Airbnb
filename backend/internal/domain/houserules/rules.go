// Package houserules owns the per-listing rule set guests acknowledge
// before booking, plus the per-booking acceptance record that proves
// it. The two aggregates together give the host a defensible audit
// trail ("you accepted version 3 at 14:02 UTC on 2026-06-02") that
// matters for insurance claims and dispute adjudication where the
// guest later denies they knew about a rule.
//
// Versioning is the key design choice: every rule edit produces a new
// Rules row with version+1, never an in-place update. Historical
// acceptances therefore continue to resolve to the exact text the
// guest saw — a rule list that changed after the booking would
// otherwise let a host rewrite history to support a dispute.
package houserules

import (
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// maxItems caps the rule count per listing. The number is generous (a
// realistic listing has 5-15 rules) and the cap exists to keep the
// per-version row a sane size for indexing and serving.
const maxItems = 50

// maxItemLength caps a single rule's character count. Long enough for
// "No parties or events. Quiet hours 22:00–08:00. Pets allowed with
// prior approval and a 50 EUR cleaning surcharge." short enough that a
// host can't paste a wall of text that makes the UI unusable.
const maxItemLength = 500

// Rules is the host-authored rule set at a specific Version. A new
// Save bumps Version by 1 and writes a new row — Rules is therefore
// effectively immutable once persisted.
type Rules struct {
	PropertyID uuid.UUID
	Version    int
	Items      []string
	UpdatedAt  time.Time
}

// New constructs a brand-new Rules at version 1. Use it when a host
// first authors rules; subsequent edits go through Bump.
func New(propertyID uuid.UUID, items []string) (*Rules, error) {
	cleaned, err := validateItems(items)
	if err != nil {
		return nil, err
	}
	if propertyID == uuid.Nil {
		return nil, shared.NewValidationError("houserules: propertyID is required")
	}
	return &Rules{
		PropertyID: propertyID,
		Version:    1,
		Items:      cleaned,
		UpdatedAt:  time.Now().UTC(),
	}, nil
}

// Bump produces the next version of an existing Rules. The receiver
// stays untouched (the caller persists the new value and keeps the old
// row for history). Returns a new aggregate at version+1.
func (r *Rules) Bump(items []string) (*Rules, error) {
	if r == nil {
		return nil, shared.NewValidationError("houserules: cannot bump nil rules")
	}
	cleaned, err := validateItems(items)
	if err != nil {
		return nil, err
	}
	return &Rules{
		PropertyID: r.PropertyID,
		Version:    r.Version + 1,
		Items:      cleaned,
		UpdatedAt:  time.Now().UTC(),
	}, nil
}

// Active reports whether this Rules version requires guest acknowledgement.
// An empty rule set (no items, version may still be > 0 if the host cleared
// them) is treated as no-op — the booking flow skips the version-match
// gate and acceptance is optional.
//
// Why allow version > 0 with empty items: a host can withdraw all rules
// and we want existing acceptances to remain valid (they still point at a
// non-current version) without preventing future bookings.
func (r *Rules) Active() bool {
	return r != nil && len(r.Items) > 0
}

// validateItems trims whitespace, rejects empties and oversized entries,
// and applies the count cap. Returns the cleaned slice in input order.
func validateItems(items []string) ([]string, error) {
	if len(items) > maxItems {
		return nil, shared.NewValidationError("houserules: too many rules")
	}
	out := make([]string, 0, len(items))
	for _, raw := range items {
		t := strings.TrimSpace(raw)
		if t == "" {
			return nil, shared.NewValidationError("houserules: rule text is required")
		}
		if len(t) > maxItemLength {
			return nil, shared.NewValidationError("houserules: rule text exceeds 500 characters")
		}
		out = append(out, t)
	}
	return out, nil
}
