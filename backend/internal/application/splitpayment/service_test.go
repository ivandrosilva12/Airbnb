package splitpaymentapp_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/airhost/backend/internal/application/event"
	splitpaymentapp "github.com/airhost/backend/internal/application/splitpayment"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/splitpayment"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

// capturingPublisher records every event the service publishes so the test
// can assert on completion-event fan-out without spinning up the in-process
// dispatcher.
type capturingPublisher struct {
	mu     sync.Mutex
	events []event.Event
}

func (p *capturingPublisher) Publish(_ context.Context, ev event.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, ev)
}

func (p *capturingPublisher) names() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, 0, len(p.events))
	for _, ev := range p.events {
		out = append(out, ev.EventName())
	}
	return out
}

// splitFixture is the common harness: organizer + invitee user accounts,
// in-memory repos, a capturing publisher, the service.
type splitFixture struct {
	svc       *splitpaymentapp.Service
	splits    *memory.SplitPaymentRepository
	users     *memory.UserRepository
	publisher *capturingPublisher
	organizer *user.User
	invitee   *user.User
	stranger  *user.User
	bookingID uuid.UUID
}

func newSplitFixture(t *testing.T) *splitFixture {
	t.Helper()
	ctx := context.Background()
	users := memory.NewUserRepository()
	splits := memory.NewSplitPaymentRepository()
	pub := &capturingPublisher{}
	svc := splitpaymentapp.NewService(splits, users, pub)

	mustUser := func(email string) *user.User {
		u, err := user.NewUser("kc-"+email, email, "Test "+email, user.RoleGuest)
		if err != nil {
			t.Fatalf("seed user: %v", err)
		}
		if err := users.Create(ctx, u); err != nil {
			t.Fatalf("save user: %v", err)
		}
		return u
	}
	organizer := mustUser("alice@test.dev")
	invitee := mustUser("bob@test.dev")
	stranger := mustUser("eve@test.dev")

	return &splitFixture{
		svc: svc, splits: splits, users: users, publisher: pub,
		organizer: organizer, invitee: invitee, stranger: stranger,
		bookingID: uuid.New(),
	}
}

func (f *splitFixture) seedSplit(t *testing.T) *splitpayment.SplitPayment {
	t.Helper()
	sp, err := f.svc.Create(context.Background(), splitpaymentapp.CreateInput{
		BookingID: f.bookingID, OrganizerID: f.organizer.ID, OrganizerEmail: f.organizer.Email,
		Currency: "EUR", TotalCents: 10000,
		Shares: []splitpayment.ShareInput{
			{Email: f.organizer.Email, AmountCents: 5000},
			{Email: f.invitee.Email, AmountCents: 5000},
		},
	})
	if err != nil {
		t.Fatalf("create split: %v", err)
	}
	return sp
}

// TestCreatePersistsSplit — happy path.
func TestCreatePersistsSplit(t *testing.T) {
	f := newSplitFixture(t)
	sp := f.seedSplit(t)
	if sp.Status != splitpayment.StatusPending {
		t.Fatalf("status = %v, want pending", sp.Status)
	}
	if len(sp.Shares) != 2 {
		t.Fatalf("shares = %d, want 2", len(sp.Shares))
	}
}

// TestAuthorizeMyShareNotCompletedYet — first payer authorises; the split
// stays pending (one share still unpaid); no completion event published.
func TestAuthorizeMyShareNotCompletedYet(t *testing.T) {
	f := newSplitFixture(t)
	sp := f.seedSplit(t)
	myShare := sp.Shares[0] // organizer's share
	after, err := f.svc.AuthorizeShare(context.Background(), f.organizer.ID, sp.ID, myShare.ID)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if after.Status != splitpayment.StatusPending {
		t.Fatalf("status = %v, want pending after 1 of 2", after.Status)
	}
	if len(f.publisher.events) != 0 {
		t.Fatalf("publisher events = %v, want none yet", f.publisher.names())
	}
}

// TestLastShareCompletesAndPublishes — once every share is paid the split
// transitions to completed and SplitPaymentCompleted is fanned out so the
// booking context can confirm the reservation.
func TestLastShareCompletesAndPublishes(t *testing.T) {
	f := newSplitFixture(t)
	sp := f.seedSplit(t)
	ctx := context.Background()
	// Organizer pays their share.
	if _, err := f.svc.AuthorizeShare(ctx, f.organizer.ID, sp.ID, sp.Shares[0].ID); err != nil {
		t.Fatalf("authorize organizer: %v", err)
	}
	// Invitee pays the second (last) share.
	after, err := f.svc.AuthorizeShare(ctx, f.invitee.ID, sp.ID, sp.Shares[1].ID)
	if err != nil {
		t.Fatalf("authorize invitee: %v", err)
	}
	if after.Status != splitpayment.StatusCompleted {
		t.Fatalf("status = %v, want completed", after.Status)
	}
	names := f.publisher.names()
	if len(names) != 1 || names[0] != "splitpayment.completed" {
		t.Fatalf("publisher events = %v, want exactly [splitpayment.completed]", names)
	}
}

// TestAuthorizeShareRejectsStranger — a third party whose email doesn't
// appear in the shares cannot mark anyone's share as paid.
func TestAuthorizeShareRejectsStranger(t *testing.T) {
	f := newSplitFixture(t)
	sp := f.seedSplit(t)
	_, err := f.svc.AuthorizeShare(context.Background(), f.stranger.ID, sp.ID, sp.Shares[1].ID)
	if err == nil {
		t.Fatalf("authorize by stranger: expected error, got nil")
	}
}

// TestCancelOnlyOrganizer — only the organizer can cancel the split.
func TestCancelOnlyOrganizer(t *testing.T) {
	f := newSplitFixture(t)
	sp := f.seedSplit(t)
	ctx := context.Background()
	if _, err := f.svc.Cancel(ctx, f.invitee.ID, sp.ID); !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("cancel by invitee: err = %v, want ErrForbidden", err)
	}
	after, err := f.svc.Cancel(ctx, f.organizer.ID, sp.ID)
	if err != nil {
		t.Fatalf("cancel by organizer: %v", err)
	}
	if after.Status != splitpayment.StatusCancelled {
		t.Fatalf("status = %v, want cancelled", after.Status)
	}
}

// TestGetByIDGatedByEmailOrOrganizer — only the organizer or a payer
// (matched by email on their user record) can read the split.
func TestGetByIDGatedByEmailOrOrganizer(t *testing.T) {
	f := newSplitFixture(t)
	sp := f.seedSplit(t)
	ctx := context.Background()
	if _, err := f.svc.GetByID(ctx, f.organizer.ID, sp.ID); err != nil {
		t.Fatalf("organizer get: %v", err)
	}
	if _, err := f.svc.GetByID(ctx, f.invitee.ID, sp.ID); err != nil {
		t.Fatalf("invitee get: %v", err)
	}
	if _, err := f.svc.GetByID(ctx, f.stranger.ID, sp.ID); !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("stranger get: err = %v, want ErrForbidden", err)
	}
}

// TestListMineSeesOrganizerAndPayerSplits — invitee's ListMine surfaces a
// split where they're only a payer (matched by lower-case email).
func TestListMineSeesOrganizerAndPayerSplits(t *testing.T) {
	f := newSplitFixture(t)
	f.seedSplit(t)
	items, err := f.svc.ListMine(context.Background(), f.invitee.ID)
	if err != nil {
		t.Fatalf("list invitee: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("invitee ListMine len = %d, want 1", len(items))
	}
	// Stranger sees nothing.
	items, err = f.svc.ListMine(context.Background(), f.stranger.ID)
	if err != nil {
		t.Fatalf("list stranger: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("stranger ListMine len = %d, want 0", len(items))
	}
}
