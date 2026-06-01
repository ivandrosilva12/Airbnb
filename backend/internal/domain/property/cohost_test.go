package property

import (
	"errors"
	"testing"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

func TestNewCohostNormalisesPermissions(t *testing.T) {
	propertyID, userID := uuid.New(), uuid.New()
	// Out-of-order, duplicated input is normalised to canonical order.
	c, err := NewCohost(propertyID, userID, []CohostPermission{
		PermReplyMessages, PermManageCalendar, PermManageCalendar, PermManagePricing,
	})
	if err != nil {
		t.Fatalf("NewCohost: %v", err)
	}
	want := []CohostPermission{PermManageCalendar, PermManagePricing, PermReplyMessages}
	if len(c.Permissions) != len(want) {
		t.Fatalf("got %d perms, want %d (%v)", len(c.Permissions), len(want), c.Permissions)
	}
	for i, p := range want {
		if c.Permissions[i] != p {
			t.Fatalf("perm[%d] = %q, want %q", i, c.Permissions[i], p)
		}
	}
	if !c.Has(PermManageCalendar) || !c.Has(PermManagePricing) || !c.Has(PermReplyMessages) {
		t.Fatalf("Has() missed an explicit grant: %v", c.Permissions)
	}
}

func TestNewCohostRejectsUnknownPermission(t *testing.T) {
	_, err := NewCohost(uuid.New(), uuid.New(), []CohostPermission{"manage_payouts"})
	if !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("want validation error, got %v", err)
	}
}

func TestNewCohostRejectsEmptyPermissions(t *testing.T) {
	if _, err := NewCohost(uuid.New(), uuid.New(), nil); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("want validation error for nil perms, got %v", err)
	}
	if _, err := NewCohost(uuid.New(), uuid.New(), []CohostPermission{}); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("want validation error for empty perms, got %v", err)
	}
}

func TestNewCohostRejectsMissingIDs(t *testing.T) {
	if _, err := NewCohost(uuid.Nil, uuid.New(), []CohostPermission{PermManageCalendar}); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("want validation error for empty propertyID, got %v", err)
	}
	if _, err := NewCohost(uuid.New(), uuid.Nil, []CohostPermission{PermManageCalendar}); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("want validation error for empty userID, got %v", err)
	}
}

func TestCohostSetPermissionsTouches(t *testing.T) {
	c, err := NewCohost(uuid.New(), uuid.New(), []CohostPermission{PermManageCalendar})
	if err != nil {
		t.Fatalf("NewCohost: %v", err)
	}
	first := c.UpdatedAt
	// Ensure time advances at least one tick on a typical system clock.
	for !c.UpdatedAt.After(first) && first == c.UpdatedAt {
		if err := c.SetPermissions([]CohostPermission{PermManagePricing}); err != nil {
			t.Fatalf("SetPermissions: %v", err)
		}
	}
	if !c.Has(PermManagePricing) || c.Has(PermManageCalendar) {
		t.Fatalf("SetPermissions did not replace the perm set: %v", c.Permissions)
	}
	if !c.UpdatedAt.After(first) {
		t.Fatalf("UpdatedAt should advance on mutation")
	}
}

func TestCohostSetPermissionsRejectsEmpty(t *testing.T) {
	c, err := NewCohost(uuid.New(), uuid.New(), []CohostPermission{PermManageCalendar})
	if err != nil {
		t.Fatalf("NewCohost: %v", err)
	}
	if err := c.SetPermissions(nil); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("want validation error, got %v", err)
	}
}

func TestValidCohostPermission(t *testing.T) {
	for _, p := range []CohostPermission{PermManageCalendar, PermManagePricing, PermReplyMessages} {
		if !ValidCohostPermission(p) {
			t.Fatalf("%q should be valid", p)
		}
	}
	for _, p := range []CohostPermission{"", "manage_payouts", "delete_listing", "owner"} {
		if ValidCohostPermission(p) {
			t.Fatalf("%q should be invalid", p)
		}
	}
}
