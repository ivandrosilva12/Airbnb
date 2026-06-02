package dto

import (
	"time"

	houserules "github.com/airhost/backend/internal/application/houserules"
	domain "github.com/airhost/backend/internal/domain/houserules"
	"github.com/google/uuid"
)

// HouseRulesView is the wire shape returned for GET/PATCH on a
// property's house rules. Version is part of the contract: every
// client that intends to let a guest book must pass it back as
// acceptedHouseRulesVersion on the booking request.
type HouseRulesView struct {
	PropertyID uuid.UUID `json:"propertyId"`
	Version    int       `json:"version"`
	Items      []string  `json:"items"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// EmptyHouseRules is the response for a property whose host has never
// authored rules. The wire shape mirrors a real rules row at version
// 0 with no items — the client can render "no rules" without a
// special-case branch and the version-mismatch gate stays uniform
// (0 ≠ any current version > 0).
func EmptyHouseRules(propertyID uuid.UUID) HouseRulesView {
	return HouseRulesView{PropertyID: propertyID, Version: 0, Items: []string{}}
}

// FromHouseRules maps the domain aggregate to the wire shape.
func FromHouseRules(r *domain.Rules) HouseRulesView {
	if r == nil {
		return HouseRulesView{Items: []string{}}
	}
	items := make([]string, len(r.Items))
	copy(items, r.Items)
	return HouseRulesView{
		PropertyID: r.PropertyID,
		Version:    r.Version,
		Items:      items,
		UpdatedAt:  r.UpdatedAt,
	}
}

// HouseRulesAcceptanceView bundles the acceptance row with the
// versioned rules text the guest saw, so callers render the full
// proof in one round-trip.
type HouseRulesAcceptanceView struct {
	BookingID       uuid.UUID      `json:"bookingId"`
	GuestID         uuid.UUID      `json:"guestId"`
	PropertyID      uuid.UUID      `json:"propertyId"`
	AcceptedVersion int            `json:"acceptedVersion"`
	AcceptedAt      time.Time      `json:"acceptedAt"`
	Rules           HouseRulesView `json:"rules"`
}

// FromAcceptance turns the application-layer AcceptanceView into the
// wire shape, handling the case where the rules row is missing (a
// corrupted history would otherwise nil-panic the response).
func FromAcceptance(v *houserules.AcceptanceView) HouseRulesAcceptanceView {
	out := HouseRulesAcceptanceView{
		BookingID:       v.Acceptance.BookingID,
		GuestID:         v.Acceptance.GuestID,
		PropertyID:      v.Acceptance.PropertyID,
		AcceptedVersion: v.Acceptance.AcceptedVersion,
		AcceptedAt:      v.Acceptance.AcceptedAt,
	}
	out.Rules = FromHouseRules(v.Rules)
	return out
}
