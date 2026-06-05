package propertyapp_test

import (
	"context"
	"errors"
	"testing"

	"github.com/airhost/backend/internal/application/event"
	propertyapp "github.com/airhost/backend/internal/application/property"
	"github.com/airhost/backend/internal/domain/audit"
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

// fakeAuditor records propertyapp.AuditRecord calls so a test can assert
// the cohost service wrote exactly one audit row, with the expected
// action, actor, subject (target) id and metadata shape (S120).
type fakeAuditor struct {
	records []propertyapp.AuditRecord
}

func (f *fakeAuditor) Record(_ context.Context, rec propertyapp.AuditRecord) error {
	f.records = append(f.records, rec)
	return nil
}

// TestInvite_RecordsAuditEvent verifies that a successful Invite appends
// a "cohost.invited" audit record with the inviting host as actor, the
// invitee as subject (target id), and the property id + permission list
// in metadata so the row is self-contained for compliance review (S120).
func TestInvite_RecordsAuditEvent(t *testing.T) {
	f := newCohostFixture(t)
	ctx := context.Background()
	auditor := &fakeAuditor{}
	f.svc.WithAuditor(auditor)

	perms := []property.CohostPermission{property.PermReplyMessages, property.PermManageCalendar}
	if _, err := f.svc.Invite(ctx, propertyapp.InviteInput{
		HostID:      f.host.ID,
		PropertyID:  f.property.ID,
		Email:       "cohost@test.dev",
		Permissions: perms,
	}); err != nil {
		t.Fatalf("invite: %v", err)
	}

	if len(auditor.records) != 1 {
		t.Fatalf("audit record count = %d, want 1", len(auditor.records))
	}
	rec := auditor.records[0]
	if rec.Action != audit.ActionCohostInvited {
		t.Fatalf("action = %q, want %q", rec.Action, audit.ActionCohostInvited)
	}
	if rec.ActorID != f.host.ID {
		t.Fatalf("actor id = %v, want host id %v", rec.ActorID, f.host.ID)
	}
	if rec.TargetID != f.cohostU.ID {
		t.Fatalf("target (subject) id = %v, want invitee id %v", rec.TargetID, f.cohostU.ID)
	}
	if rec.TargetType != audit.TargetUser {
		t.Fatalf("target type = %q, want %q", rec.TargetType, audit.TargetUser)
	}
	// Metadata must carry the property id (so a "who got access to X" query
	// joins on it without re-fetching the cohost grant) AND the permission
	// list (so a revoked grant still shows what was granted).
	if got, want := rec.Metadata["property_id"], f.property.ID.String(); got != want {
		t.Fatalf("metadata property_id = %v, want %v", got, want)
	}
	gotPerms, ok := rec.Metadata["permissions"].([]string)
	if !ok {
		t.Fatalf("metadata permissions type = %T, want []string", rec.Metadata["permissions"])
	}
	if len(gotPerms) != len(perms) {
		t.Fatalf("metadata permissions len = %d, want %d", len(gotPerms), len(perms))
	}
	want := map[string]bool{}
	for _, p := range perms {
		want[string(p)] = true
	}
	for _, p := range gotPerms {
		if !want[p] {
			t.Fatalf("unexpected permission in metadata: %q", p)
		}
	}
}

// TestInvite_PublishesCohostInvitedThroughUoW confirms that when a UoW
// is wired (S156) a successful Invite lands the CohostInvited event in
// the outbox AND dispatches it via the durable publisher, instead of
// going through the legacy in-process s.pub.Publish path. This is the
// core durability guarantee of the slice — a crash between the cohost
// row commit and the event emit can no longer drop the notification
// because both writes commit atomically. The assertion captures one
// dispatched event and matches the invitee + property + permissions
// payload to prove the closure populated the record correctly.
func TestInvite_PublishesCohostInvitedThroughUoW(t *testing.T) {
	f := newCohostFixture(t)
	ctx := context.Background()

	captured := &[]event.Event{}
	d := event.NewDispatcher()
	d.Subscribe(func(_ context.Context, e event.Event) { *captured = append(*captured, e) })
	outbox := event.NewMemoryOutbox()
	relay := event.NewDurablePublisher(outbox, d)
	uow := memory.NewUnitOfWork(nil, nil, nil, nil, nil, outbox, relay)
	f.svc.WithUnitOfWork(uow)

	perms := []property.CohostPermission{property.PermReplyMessages, property.PermManageCalendar}
	c, err := f.svc.Invite(ctx, propertyapp.InviteInput{
		HostID:      f.host.ID,
		PropertyID:  f.property.ID,
		Email:       "cohost@test.dev",
		Permissions: perms,
	})
	if err != nil {
		t.Fatalf("invite: %v", err)
	}

	if len(*captured) != 1 {
		t.Fatalf("captured event count = %d, want 1", len(*captured))
	}
	ev, ok := (*captured)[0].(event.CohostInvited)
	if !ok {
		t.Fatalf("captured event type = %T, want event.CohostInvited", (*captured)[0])
	}
	if ev.CohostID != c.ID {
		t.Fatalf("cohost id = %v, want %v", ev.CohostID, c.ID)
	}
	if ev.PropertyID != f.property.ID {
		t.Fatalf("property id = %v, want %v", ev.PropertyID, f.property.ID)
	}
	if ev.UserID != f.cohostU.ID {
		t.Fatalf("invitee user id = %v, want %v", ev.UserID, f.cohostU.ID)
	}
	if ev.HostID != f.host.ID {
		t.Fatalf("host id = %v, want %v", ev.HostID, f.host.ID)
	}
	if len(ev.Permissions) != len(perms) {
		t.Fatalf("permissions len = %d, want %d", len(ev.Permissions), len(perms))
	}
}

// TestInvite_UoWCommitErrorRollsBackCohostInvited proves the durability
// guarantee added by S156: when the UoW reports a commit-time failure
// after the closure ran, the CohostInvited event is NOT dispatched —
// matching what production would do if the DB commit failed. The
// pre-S156 path could not offer this contract because the event was
// published in-process AFTER the row write, so a commit-time crash
// would have already shipped the notification.
func TestInvite_UoWCommitErrorRollsBackCohostInvited(t *testing.T) {
	f := newCohostFixture(t)
	ctx := context.Background()

	captured := &[]event.Event{}
	d := event.NewDispatcher()
	d.Subscribe(func(_ context.Context, e event.Event) { *captured = append(*captured, e) })
	outbox := event.NewMemoryOutbox()
	relay := event.NewDurablePublisher(outbox, d)
	boom := errors.New("simulated commit failure")
	uow := memory.NewUnitOfWork(nil, nil, nil, nil, nil, outbox, relay).WithCommitError(boom)
	f.svc.WithUnitOfWork(uow)

	_, err := f.svc.Invite(ctx, propertyapp.InviteInput{
		HostID:      f.host.ID,
		PropertyID:  f.property.ID,
		Email:       "cohost@test.dev",
		Permissions: []property.CohostPermission{property.PermManageCalendar},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("invite err = %v, want simulated commit failure", err)
	}
	if len(*captured) != 0 {
		t.Fatalf("captured event count = %d, want 0 (no event dispatched on commit rollback)", len(*captured))
	}
}
