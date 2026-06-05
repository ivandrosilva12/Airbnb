package userapp_test

import (
	"context"
	"testing"
	"time"

	bookingapp "github.com/airhost/backend/internal/application/booking"
	"github.com/airhost/backend/internal/application/event"
	propertyapp "github.com/airhost/backend/internal/application/property"
	userapp "github.com/airhost/backend/internal/application/user"
	"github.com/airhost/backend/internal/domain/audit"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

func TestSyncFromIdentity_RelinksOnNewSubjectSameEmail(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewUserRepository()
	svc := userapp.NewService(repo)

	// First login under one subject provisions the account.
	first, err := svc.SyncFromIdentity(ctx, userapp.Identity{
		Subject: "sub-old", Email: "u@test.dev", FullName: "U", Roles: []string{"host"},
	})
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}

	// The IdP re-provisions the same email under a NEW subject. This must re-link
	// the existing account (no unique-email conflict), preserving its id/role.
	again, err := svc.SyncFromIdentity(ctx, userapp.Identity{
		Subject: "sub-new", Email: "u@test.dev", FullName: "U", Roles: nil,
	})
	if err != nil {
		t.Fatalf("relink sync: %v", err)
	}
	if again.ID != first.ID {
		t.Fatalf("expected the same account (id %s), got %s", first.ID, again.ID)
	}
	if again.KeycloakSub != "sub-new" {
		t.Fatalf("subject = %q, want re-linked to sub-new", again.KeycloakSub)
	}
	if again.Role != user.RoleHost {
		t.Fatalf("role = %q, want host preserved", again.Role)
	}
	// A subsequent login under the new subject now resolves directly.
	if u, err := svc.SyncFromIdentity(ctx, userapp.Identity{Subject: "sub-new", Email: "u@test.dev", FullName: "U"}); err != nil || u.ID != first.ID {
		t.Fatalf("lookup by new subject: id=%v err=%v", u.ID, err)
	}
}

func TestSyncFromIdentity_DerivesRoleFromRealmRoles(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewUserRepository()
	svc := userapp.NewService(repo)

	// First login with the admin realm role provisions an admin.
	u, err := svc.SyncFromIdentity(ctx, userapp.Identity{
		Subject: "sub-admin", Email: "admin@test.dev", FullName: "Admin", Roles: []string{"admin", "guest"},
	})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if u.Role != user.RoleAdmin {
		t.Fatalf("role = %q, want admin", u.Role)
	}

	// A host-role token provisions a host.
	h, err := svc.SyncFromIdentity(ctx, userapp.Identity{
		Subject: "sub-host", Email: "host@test.dev", FullName: "Host", Roles: []string{"host"},
	})
	if err != nil || h.Role != user.RoleHost {
		t.Fatalf("host sync: role=%v err=%v", h.Role, err)
	}

	// No recognised role defaults to guest.
	g, err := svc.SyncFromIdentity(ctx, userapp.Identity{
		Subject: "sub-guest", Email: "guest@test.dev", FullName: "Guest", Roles: []string{"offline_access"},
	})
	if err != nil || g.Role != user.RoleGuest {
		t.Fatalf("guest sync: role=%v err=%v", g.Role, err)
	}
}

func TestSyncFromIdentity_ElevatesButNeverDemotes(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewUserRepository()
	svc := userapp.NewService(repo)

	// Start as a guest.
	if _, err := svc.SyncFromIdentity(ctx, userapp.Identity{
		Subject: "sub-1", Email: "u@test.dev", FullName: "U", Roles: nil,
	}); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	// A later login asserting the admin role elevates the persisted user.
	u, err := svc.SyncFromIdentity(ctx, userapp.Identity{
		Subject: "sub-1", Email: "u@test.dev", FullName: "U", Roles: []string{"admin"},
	})
	if err != nil || u.Role != user.RoleAdmin {
		t.Fatalf("elevate: role=%v err=%v", u.Role, err)
	}

	// A subsequent login WITHOUT the role must not demote the admin.
	u2, err := svc.SyncFromIdentity(ctx, userapp.Identity{
		Subject: "sub-1", Email: "u@test.dev", FullName: "U", Roles: nil,
	})
	if err != nil || u2.Role != user.RoleAdmin {
		t.Fatalf("must not demote: role=%v err=%v", u2.Role, err)
	}
}

func TestSyncFromIdentity_PreservesSelfServiceHost(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewUserRepository()
	svc := userapp.NewService(repo)

	u, err := svc.SyncFromIdentity(ctx, userapp.Identity{
		Subject: "sub-1", Email: "u@test.dev", FullName: "U", Roles: nil,
	})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	// User self-promotes to host (no realm role involved).
	if _, err := svc.BecomeHost(ctx, u.ID); err != nil {
		t.Fatalf("become host: %v", err)
	}
	// A plain guest-token login must not demote them back to guest.
	again, err := svc.SyncFromIdentity(ctx, userapp.Identity{
		Subject: "sub-1", Email: "u@test.dev", FullName: "U", Roles: nil,
	})
	if err != nil || again.Role != user.RoleHost {
		t.Fatalf("self-service host preserved: role=%v err=%v", again.Role, err)
	}
}

// recordingCascadeAuditor captures the summary audit row written at
// the tail of a successful cascade so the test can assert exactly one
// ActionUserSuspendedCascadeApplied entry with the expected metadata
// (listings_suspended, bookings_cancelled, errors).
type recordingCascadeAuditor struct {
	records []userapp.CascadeAuditInput
}

func (r *recordingCascadeAuditor) Record(_ context.Context, in userapp.CascadeAuditInput) error {
	r.records = append(r.records, in)
	return nil
}

// cascadeFixture wires real propertyapp + bookingapp services against
// in-memory repos and seeds a host with two published listings, each
// holding one CONFIRMED booking for a different guest. Keeping the
// services real (rather than stubs) means the assertions cover the
// actual host-cancel branch of bookingapp.Cancel — i.e. the 100% refund
// fraction it emits via BookingCancelled.
type cascadeFixture struct {
	userSvc      *userapp.Service
	userRepo     *memory.UserRepository
	propRepo     *memory.PropertyRepository
	bookingRepo  *memory.BookingRepository
	auditor      *recordingCascadeAuditor
	admin        *user.User
	host         *user.User
	guestA       *user.User
	guestB       *user.User
	propA        *property.Property
	propB        *property.Property
	bookingA     *booking.Booking
	bookingB     *booking.Booking
}

func newCascadeFixture(t *testing.T) *cascadeFixture {
	t.Helper()
	ctx := context.Background()

	userRepo := memory.NewUserRepository()
	propRepo := memory.NewPropertyRepository()
	bookingRepo := memory.NewBookingRepository().WithProperties(propRepo)
	couponRepo := memory.NewCouponRepository()
	priceRuleRepo := memory.NewPriceRuleRepository()
	blockRepo := memory.NewBlockRepository()
	outbox := event.NewMemoryOutbox()
	dispatcher := event.NewDispatcher()
	relay := event.NewDurablePublisher(outbox, dispatcher)
	uow := memory.NewUnitOfWork(bookingRepo, nil, nil, nil, nil, outbox, relay)

	propSvc := propertyapp.NewService(propRepo, nil, noopVerifier{}, false)
	bookingSvc := bookingapp.NewService(bookingRepo, propRepo, blockRepo, couponRepo, priceRuleRepo, 0, noopVerifier{}, false, uow)

	auditor := &recordingCascadeAuditor{}
	userSvc := userapp.NewService(userRepo).
		WithCascade(
			testPropertyCascade{svc: propSvc},
			testBookingCascade{svc: bookingSvc},
			bookingRepo,
			testHostListings{repo: propRepo},
		).
		WithCascadeAuditor(auditor)

	admin, _ := user.NewUser("admin-sub", "admin@x.dev", "Admin", user.RoleAdmin)
	host, _ := user.NewUser("host-sub", "host@x.dev", "Host", user.RoleHost)
	guestA, _ := user.NewUser("ga-sub", "ga@x.dev", "GA", user.RoleGuest)
	guestB, _ := user.NewUser("gb-sub", "gb@x.dev", "GB", user.RoleGuest)
	for _, u := range []*user.User{admin, host, guestA, guestB} {
		if err := userRepo.Create(ctx, u); err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}

	// Two published listings owned by the host.
	price, _ := shared.NewMoney(10000, "EUR")
	cleaning, _ := shared.NewMoney(0, "EUR")
	addr := property.Address{City: "Lisbon", Country: "PT", Latitude: 38.7, Longitude: -9.1}
	for _, title := range []string{"Flat A", "Flat B"} {
		p, err := property.NewProperty(host.ID, title, "", property.TypeApartment, addr, price, cleaning, 4, 1, 1, 1, nil)
		if err != nil {
			t.Fatalf("new property %s: %v", title, err)
		}
		p.AddPhoto("k", "http://x/k.jpg")
		if err := p.Publish(); err != nil {
			t.Fatalf("publish %s: %v", title, err)
		}
		// Instant-book so Create auto-confirms — bookings need to be in
		// the CONFIRMED state for the cascade to pick them up.
		p.SetInstantBook(true)
		if err := propRepo.Create(ctx, p); err != nil {
			t.Fatalf("save %s: %v", title, err)
		}
	}
	// Retrieve the seeded props in a deterministic order (by title).
	pageRes, err := propRepo.ListByHost(ctx, host.ID, shared.Page{Limit: 10})
	if err != nil || len(pageRes.Items) != 2 {
		t.Fatalf("list seeded props: items=%d err=%v", len(pageRes.Items), err)
	}
	var propA, propB *property.Property
	for _, p := range pageRes.Items {
		switch p.Title {
		case "Flat A":
			propA = p
		case "Flat B":
			propB = p
		}
	}

	// One confirmed booking on each listing.
	check := func(in bookingapp.CreateInput) *booking.Booking {
		b, err := bookingSvc.Create(ctx, in)
		if err != nil {
			t.Fatalf("create booking: %v", err)
		}
		if b.Status != booking.StatusConfirmed {
			t.Fatalf("booking status = %q, want confirmed", b.Status)
		}
		return b
	}
	bookingA := check(bookingapp.CreateInput{
		GuestID: guestA.ID, PropertyID: propA.ID,
		CheckIn: time.Now().UTC().AddDate(0, 0, 5), CheckOut: time.Now().UTC().AddDate(0, 0, 8),
		Guests: 1,
	})
	bookingB := check(bookingapp.CreateInput{
		GuestID: guestB.ID, PropertyID: propB.ID,
		CheckIn: time.Now().UTC().AddDate(0, 0, 9), CheckOut: time.Now().UTC().AddDate(0, 0, 11),
		Guests: 1,
	})

	return &cascadeFixture{
		userSvc: userSvc, userRepo: userRepo, propRepo: propRepo, bookingRepo: bookingRepo,
		auditor: auditor,
		admin:   admin, host: host, guestA: guestA, guestB: guestB,
		propA: propA, propB: propB,
		bookingA: bookingA, bookingB: bookingB,
	}
}

// noopVerifier satisfies IdentityVerifier ports without KYC.
type noopVerifier struct{}

func (noopVerifier) IsVerified(context.Context, uuid.UUID) (bool, error) { return true, nil }

// testPropertyCascade adapts *propertyapp.Service to the userapp port
// inside the test (mirrors the main.go adapter).
type testPropertyCascade struct{ svc *propertyapp.Service }

func (a testPropertyCascade) Suspend(ctx context.Context, adminID, propertyID uuid.UUID) error {
	_, err := a.svc.Suspend(ctx, adminID, propertyID)
	return err
}

// testBookingCascade adapts *bookingapp.Service to the userapp port.
type testBookingCascade struct{ svc *bookingapp.Service }

func (a testBookingCascade) Cancel(ctx context.Context, actorID, bookingID uuid.UUID) error {
	_, err := a.svc.Cancel(ctx, actorID, bookingID)
	return err
}

// testHostListings adapts the property memory repo to the userapp port
// — same shape as the main.go shim, kept inline for the test.
type testHostListings struct{ repo *memory.PropertyRepository }

func (a testHostListings) IDsByHost(ctx context.Context, hostID uuid.UUID) ([]uuid.UUID, error) {
	page, err := a.repo.ListByHost(ctx, hostID, shared.Page{Limit: 100})
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(page.Items))
	for _, p := range page.Items {
		ids = append(ids, p.ID)
	}
	return ids, nil
}

// TestSuspend_CascadesToListingsAndBookings exercises the S169 happy
// path: a host with two listings and two confirmed bookings is
// suspended; the cascade auto-suspends both listings, auto-cancels both
// bookings via the host-cancel branch (100% refund), and writes one
// ActionUserSuspendedCascadeApplied audit row summarising the result.
func TestSuspend_CascadesToListingsAndBookings(t *testing.T) {
	f := newCascadeFixture(t)
	ctx := context.Background()

	u, err := f.userSvc.Suspend(ctx, f.admin.ID, f.host.ID)
	if err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if u.IsActive {
		t.Fatalf("host not suspended")
	}

	// Both listings are now suspended.
	for _, want := range []*property.Property{f.propA, f.propB} {
		got, err := f.propRepo.FindByID(ctx, want.ID)
		if err != nil {
			t.Fatalf("reload listing %s: %v", want.Title, err)
		}
		if got.Status != property.StatusSuspended {
			t.Fatalf("listing %s status = %q, want suspended", want.Title, got.Status)
		}
	}

	// Both bookings are now cancelled.
	for _, want := range []*booking.Booking{f.bookingA, f.bookingB} {
		got, err := f.bookingRepo.FindByID(ctx, want.ID)
		if err != nil {
			t.Fatalf("reload booking %s: %v", want.ID, err)
		}
		if got.Status != booking.StatusCancelled {
			t.Fatalf("booking %s status = %q, want cancelled", want.ID, got.Status)
		}
	}

	// Exactly one cascade summary audit row landed.
	if len(f.auditor.records) != 1 {
		t.Fatalf("cascade audit rows = %d, want 1", len(f.auditor.records))
	}
	rec := f.auditor.records[0]
	if rec.Action != audit.ActionUserSuspendedCascadeApplied {
		t.Fatalf("action = %q, want %q", rec.Action, audit.ActionUserSuspendedCascadeApplied)
	}
	if rec.ActorID != f.admin.ID {
		t.Fatalf("actor = %v, want admin id %v", rec.ActorID, f.admin.ID)
	}
	if rec.TargetID != f.host.ID || rec.TargetType != audit.TargetUser {
		t.Fatalf("target = %v/%q, want host id %v / user", rec.TargetID, rec.TargetType, f.host.ID)
	}
	if got, want := rec.Metadata["listings_suspended"], 2; got != want {
		t.Fatalf("listings_suspended = %v, want %d", got, want)
	}
	if got, want := rec.Metadata["bookings_cancelled"], 2; got != want {
		t.Fatalf("bookings_cancelled = %v, want %d", got, want)
	}
	if errs, ok := rec.Metadata["errors"].([]string); !ok || len(errs) != 0 {
		t.Fatalf("errors = %v, want empty slice", rec.Metadata["errors"])
	}
}

// TestSuspend_NilCascade_NoCascade locks in the legacy behaviour: a
// Service constructed without WithCascade still suspends the user
// account but does NOT touch listings or bookings (so a deployment
// that doesn't wire the new dependencies — including all the existing
// e2e harnesses — keeps working exactly as before).
func TestSuspend_NilCascade_NoCascade(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewUserRepository()
	svc := userapp.NewService(repo) // no WithCascade

	admin, _ := user.NewUser("admin-sub", "a@x.dev", "A", user.RoleAdmin)
	host, _ := user.NewUser("host-sub", "h@x.dev", "H", user.RoleHost)
	for _, u := range []*user.User{admin, host} {
		if err := repo.Create(ctx, u); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	u, err := svc.Suspend(ctx, admin.ID, host.ID)
	if err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if u.IsActive {
		t.Fatalf("host should be suspended")
	}
	// A second call (already-inactive) is a no-op and must NOT panic on
	// nil cascade deps — the early return guards both branches.
	if _, err := svc.Suspend(ctx, admin.ID, host.ID); err != nil {
		t.Fatalf("retry suspend: %v", err)
	}
}
