package propertyapp_test

import (
	"context"
	"errors"
	"testing"

	propertyapp "github.com/airhost/backend/internal/application/property"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
)

// cohostFixture wires the in-memory repos + service used across the cases.
// Each test gets its own fixture so concurrent test runs don't share state.
type cohostFixture struct {
	svc      *propertyapp.CohostService
	props    *memory.PropertyRepository
	cohosts  *memory.CohostRepository
	users    *memory.UserRepository
	host     *user.User
	cohostU  *user.User
	stranger *user.User
	property *property.Property
}

func newCohostFixture(t *testing.T) *cohostFixture {
	t.Helper()
	ctx := context.Background()
	users := memory.NewUserRepository()
	props := memory.NewPropertyRepository()
	cohosts := memory.NewCohostRepository()
	svc := propertyapp.NewCohostService(cohosts, props, users)

	mustUser := func(email string, role user.Role) *user.User {
		u, err := user.NewUser("kc-"+email, email, "Test "+email, role)
		if err != nil {
			t.Fatalf("seed user: %v", err)
		}
		if err := users.Create(ctx, u); err != nil {
			t.Fatalf("save user: %v", err)
		}
		return u
	}
	host := mustUser("host@test.dev", user.RoleHost)
	cohostU := mustUser("cohost@test.dev", user.RoleGuest)
	stranger := mustUser("stranger@test.dev", user.RoleGuest)

	addr := property.Address{Line1: "1 Main", City: "Lisbon", Country: "PT", PostalCode: "1000-001"}
	price, _ := shared.NewMoney(10000, "EUR")
	cleaning, _ := shared.NewMoney(0, "EUR")
	p, err := property.NewProperty(host.ID, "Test Flat", "desc", property.TypeApartment, addr, price, cleaning, 2, 1, 1, 1, nil)
	if err != nil {
		t.Fatalf("seed property: %v", err)
	}
	if err := props.Create(ctx, p); err != nil {
		t.Fatalf("save property: %v", err)
	}

	return &cohostFixture{
		svc: svc, props: props, cohosts: cohosts, users: users,
		host: host, cohostU: cohostU, stranger: stranger, property: p,
	}
}

// TestInviteHappyPath confirms the host can invite a cohost by email and the
// grant is persisted with the requested permissions.
func TestInviteHappyPath(t *testing.T) {
	f := newCohostFixture(t)
	ctx := context.Background()
	c, err := f.svc.Invite(ctx, propertyapp.InviteInput{
		HostID:      f.host.ID,
		PropertyID:  f.property.ID,
		Email:       "cohost@test.dev",
		Permissions: []property.CohostPermission{property.PermReplyMessages},
	})
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	if c.UserID != f.cohostU.ID {
		t.Fatalf("grant user = %v, want %v", c.UserID, f.cohostU.ID)
	}
	if !c.Has(property.PermReplyMessages) {
		t.Fatalf("grant lacks reply_messages permission")
	}
}

// TestInviteRejectsNonOwner — only the listing's primary host may invite.
func TestInviteRejectsNonOwner(t *testing.T) {
	f := newCohostFixture(t)
	ctx := context.Background()
	_, err := f.svc.Invite(ctx, propertyapp.InviteInput{
		HostID:      f.stranger.ID, // wrong actor
		PropertyID:  f.property.ID,
		Email:       "cohost@test.dev",
		Permissions: []property.CohostPermission{property.PermManageCalendar},
	})
	if !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("invite by stranger: err = %v, want ErrForbidden", err)
	}
}

// TestInviteRejectsSelfHost — the host can't invite themselves as a co-host.
func TestInviteRejectsSelfHost(t *testing.T) {
	f := newCohostFixture(t)
	ctx := context.Background()
	_, err := f.svc.Invite(ctx, propertyapp.InviteInput{
		HostID:      f.host.ID,
		PropertyID:  f.property.ID,
		Email:       "host@test.dev", // themselves
		Permissions: []property.CohostPermission{property.PermManageCalendar},
	})
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
}

// TestInviteRejectsDuplicateGrant — a second invite for the same user is a
// conflict; callers should PATCH (SetPermissions) instead.
func TestInviteRejectsDuplicateGrant(t *testing.T) {
	f := newCohostFixture(t)
	ctx := context.Background()
	if _, err := f.svc.Invite(ctx, propertyapp.InviteInput{
		HostID: f.host.ID, PropertyID: f.property.ID, Email: "cohost@test.dev",
		Permissions: []property.CohostPermission{property.PermManageCalendar},
	}); err != nil {
		t.Fatalf("first invite: %v", err)
	}
	_, err := f.svc.Invite(ctx, propertyapp.InviteInput{
		HostID: f.host.ID, PropertyID: f.property.ID, Email: "cohost@test.dev",
		Permissions: []property.CohostPermission{property.PermReplyMessages},
	})
	if !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("dup invite: err = %v, want ErrConflict", err)
	}
}

// TestSetPermissionsAndRevoke — happy path through the full lifecycle.
func TestSetPermissionsAndRevoke(t *testing.T) {
	f := newCohostFixture(t)
	ctx := context.Background()
	c, err := f.svc.Invite(ctx, propertyapp.InviteInput{
		HostID: f.host.ID, PropertyID: f.property.ID, Email: "cohost@test.dev",
		Permissions: []property.CohostPermission{property.PermManageCalendar},
	})
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	updated, err := f.svc.SetPermissions(ctx, f.host.ID, f.property.ID, c.ID, []property.CohostPermission{
		property.PermManageCalendar, property.PermReplyMessages,
	})
	if err != nil {
		t.Fatalf("setperms: %v", err)
	}
	if !updated.Has(property.PermReplyMessages) {
		t.Fatalf("perm not added")
	}
	if err := f.svc.Revoke(ctx, f.host.ID, f.property.ID, c.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	// After revoke, ListForProperty is empty.
	all, _ := f.svc.ListForProperty(ctx, f.host.ID, f.property.ID)
	if len(all) != 0 {
		t.Fatalf("after revoke len = %d, want 0", len(all))
	}
}

// TestListPropertyIDsWithPermission_FiltersByPermission — only listings where
// the user has the required permission appear in the result. Locks the S27
// behaviour (single-query ListByUser + in-memory permission filter).
func TestListPropertyIDsWithPermission_FiltersByPermission(t *testing.T) {
	f := newCohostFixture(t)
	ctx := context.Background()

	// Seed a second property and grant the same user different permissions
	// on each, so we can confirm the filter actually filters.
	addr := property.Address{Line1: "2 Main", City: "Porto", Country: "PT", PostalCode: "4000-001"}
	price, _ := shared.NewMoney(8000, "EUR")
	zero, _ := shared.NewMoney(0, "EUR")
	p2, err := property.NewProperty(f.host.ID, "Second Flat", "desc", property.TypeApartment, addr, price, zero, 2, 1, 1, 1, nil)
	if err != nil {
		t.Fatalf("seed p2: %v", err)
	}
	if err := f.props.Create(ctx, p2); err != nil {
		t.Fatalf("save p2: %v", err)
	}

	// On property 1: reply_messages. On property 2: manage_calendar only.
	if _, err := f.svc.Invite(ctx, propertyapp.InviteInput{
		HostID: f.host.ID, PropertyID: f.property.ID, Email: "cohost@test.dev",
		Permissions: []property.CohostPermission{property.PermReplyMessages},
	}); err != nil {
		t.Fatalf("invite p1: %v", err)
	}
	if _, err := f.svc.Invite(ctx, propertyapp.InviteInput{
		HostID: f.host.ID, PropertyID: p2.ID, Email: "cohost@test.dev",
		Permissions: []property.CohostPermission{property.PermManageCalendar},
	}); err != nil {
		t.Fatalf("invite p2: %v", err)
	}

	ids, err := f.svc.ListPropertyIDsWithPermission(ctx, f.cohostU.ID, property.PermReplyMessages)
	if err != nil {
		t.Fatalf("list with reply_messages: %v", err)
	}
	if len(ids) != 1 || ids[0] != f.property.ID {
		t.Fatalf("reply_messages ids = %v, want [%v]", ids, f.property.ID)
	}

	ids, err = f.svc.ListPropertyIDsWithPermission(ctx, f.cohostU.ID, property.PermManageCalendar)
	if err != nil {
		t.Fatalf("list with manage_calendar: %v", err)
	}
	if len(ids) != 1 || ids[0] != p2.ID {
		t.Fatalf("manage_calendar ids = %v, want [%v]", ids, p2.ID)
	}
}

// TestAuthorizeAdmitsOwnerOrCohost — the relaxed gate used by sibling
// services. Owner passes regardless of permission set; cohost passes only
// when the required permission is held; stranger gets ErrForbidden.
func TestAuthorizeAdmitsOwnerOrCohost(t *testing.T) {
	f := newCohostFixture(t)
	ctx := context.Background()
	if _, err := f.svc.Invite(ctx, propertyapp.InviteInput{
		HostID: f.host.ID, PropertyID: f.property.ID, Email: "cohost@test.dev",
		Permissions: []property.CohostPermission{property.PermManageCalendar},
	}); err != nil {
		t.Fatalf("invite: %v", err)
	}

	// Owner — passes regardless of required permission.
	if _, err := f.svc.Authorize(ctx, f.host.ID, f.property.ID, property.PermReplyMessages); err != nil {
		t.Fatalf("owner authorize: %v", err)
	}
	// Cohost with correct permission — passes.
	if _, err := f.svc.Authorize(ctx, f.cohostU.ID, f.property.ID, property.PermManageCalendar); err != nil {
		t.Fatalf("cohost authorize: %v", err)
	}
	// Cohost without the required permission — forbidden.
	if _, err := f.svc.Authorize(ctx, f.cohostU.ID, f.property.ID, property.PermReplyMessages); !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("cohost wrong perm: err = %v, want ErrForbidden", err)
	}
	// Stranger — forbidden.
	if _, err := f.svc.Authorize(ctx, f.stranger.ID, f.property.ID, property.PermManageCalendar); !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("stranger authorize: err = %v, want ErrForbidden", err)
	}
}
