package property

import (
	"sort"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// CohostPermission is one of the granular powers a primary host can delegate
// to a co-host on a single listing. The primary host can mix-and-match these
// freely; a co-host with no permissions is effectively read-only on the host
// surface (kept around so a host can pre-create a co-host and grant later).
//
// What a co-host *cannot* do, ever:
//   - delete the listing or change ownership
//   - move money (request a payout, resolve a dispute, refund a guest)
//   - manage other co-hosts (only the primary host invites and revokes)
//   - publish or unsuspend the listing (the primary host bears that risk)
//
// Those restrictions are enforced at the application layer by keeping the
// affected use cases on the existing IsOwnedBy gate and only routing the
// expanded gate (owner OR co-host with a matching permission) through
// callers that opt in.
type CohostPermission string

const (
	// PermManageCalendar lets the co-host create/remove host blocks and import
	// external iCal feeds.
	PermManageCalendar CohostPermission = "manage_calendar"
	// PermManagePricing lets the co-host add/remove per-date / seasonal price
	// rules (it does NOT let them change the listing's nightly base price —
	// that lives on the property aggregate, gated by ownership).
	PermManagePricing CohostPermission = "manage_pricing"
	// PermReplyMessages lets the co-host send messages on a conversation
	// attached to a listing they help manage (kept for forward-compat; the
	// initial slice does not yet rewrite messaging authorization).
	PermReplyMessages CohostPermission = "reply_messages"
)

// ValidCohostPermission reports whether the value is a recognised permission.
// Used to validate input from the HTTP layer; unknown values are rejected to
// avoid silently storing a permission that nothing will ever consult.
func ValidCohostPermission(p CohostPermission) bool {
	switch p {
	case PermManageCalendar, PermManagePricing, PermReplyMessages:
		return true
	default:
		return false
	}
}

// allCohostPermissions is the deterministic order used to canonicalise a
// permission set on storage; tests and clients can rely on it.
var allCohostPermissions = []CohostPermission{
	PermManageCalendar,
	PermManagePricing,
	PermReplyMessages,
}

// Cohost is a per-listing grant of one or more CohostPermissions to a user
// other than the listing's primary host. The grant is mutable (perms can be
// added or removed) and revocable (the row is deleted).
type Cohost struct {
	ID          uuid.UUID
	PropertyID  uuid.UUID
	UserID      uuid.UUID
	Permissions []CohostPermission
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// NewCohost builds a Cohost grant after normalising the permission set and
// rejecting an empty or unknown-only set (which would be meaningless). The
// caller must enforce that userID is not the primary host of the listing —
// the aggregate itself does not know the property's host id.
func NewCohost(propertyID, userID uuid.UUID, perms []CohostPermission) (*Cohost, error) {
	if propertyID == uuid.Nil {
		return nil, shared.NewValidationError("propertyID is required")
	}
	if userID == uuid.Nil {
		return nil, shared.NewValidationError("userID is required")
	}
	clean, err := normalisePermissions(perms)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &Cohost{
		ID:          uuid.New(),
		PropertyID:  propertyID,
		UserID:      userID,
		Permissions: clean,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// SetPermissions replaces the grant's permission set after the same
// normalisation as NewCohost. At least one valid permission is required —
// a host who wants to revoke completely should delete the row instead.
func (c *Cohost) SetPermissions(perms []CohostPermission) error {
	clean, err := normalisePermissions(perms)
	if err != nil {
		return err
	}
	c.Permissions = clean
	c.UpdatedAt = time.Now().UTC()
	return nil
}

// Has reports whether the grant includes the given permission.
func (c *Cohost) Has(p CohostPermission) bool {
	for _, granted := range c.Permissions {
		if granted == p {
			return true
		}
	}
	return false
}

// normalisePermissions validates each entry, drops duplicates, and returns
// the canonical (deterministic) ordering. An input with no valid entries
// returns a validation error so callers cannot store empty grants.
func normalisePermissions(perms []CohostPermission) ([]CohostPermission, error) {
	seen := make(map[CohostPermission]struct{}, len(perms))
	for _, p := range perms {
		if !ValidCohostPermission(p) {
			return nil, shared.NewValidationError("unknown cohost permission: " + string(p))
		}
		seen[p] = struct{}{}
	}
	if len(seen) == 0 {
		return nil, shared.NewValidationError("at least one permission is required")
	}
	out := make([]CohostPermission, 0, len(seen))
	for _, candidate := range allCohostPermissions {
		if _, ok := seen[candidate]; ok {
			out = append(out, candidate)
		}
	}
	// Defensive: if a future enum entry isn't in allCohostPermissions yet,
	// still surface it (sorted) so storage doesn't lose data.
	if len(out) < len(seen) {
		extras := make([]CohostPermission, 0)
		for p := range seen {
			found := false
			for _, kept := range out {
				if kept == p {
					found = true
					break
				}
			}
			if !found {
				extras = append(extras, p)
			}
		}
		sort.Slice(extras, func(i, j int) bool { return extras[i] < extras[j] })
		out = append(out, extras...)
	}
	return out, nil
}
