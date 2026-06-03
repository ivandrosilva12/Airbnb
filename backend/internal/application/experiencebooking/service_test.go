package experiencebookingapp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/domain/experience"
	"github.com/airhost/backend/internal/domain/experiencebooking"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

// recordingDispatcher captures every published event so tests can assert
// what the service handed to the dispatcher. Mirrors the capturing pattern
// used by splitpayment's service test — kept local so each BC's test
// stays self-contained.
type recordingDispatcher struct {
	mu     sync.Mutex
	events []event.Event
}

func (r *recordingDispatcher) Publish(_ context.Context, e event.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recordingDispatcher) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.events))
	for _, e := range r.events {
		out = append(out, e.EventName())
	}
	return out
}

type stubFinder struct {
	exp *experience.Experience
	err error
}

func (s *stubFinder) FindByID(_ context.Context, _ uuid.UUID) (*experience.Experience, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.exp, nil
}

func mkExperience(t *testing.T, status experience.Status) *experience.Experience {
	t.Helper()
	price, _ := shared.NewMoney(3000, "EUR")
	exp, err := experience.NewExperience(uuid.New(), "Pasta workshop", "Make fresh pasta from scratch",
		experience.CategoryCooking,
		experience.Address{City: "Rome", Country: "IT", Latitude: 41.9, Longitude: 12.5},
		120, 6, price, "en")
	if err != nil {
		t.Fatalf("new experience: %v", err)
	}
	exp.AddPhoto("k", "u")
	switch status {
	case experience.StatusPublished:
		if err := exp.Publish(); err != nil {
			t.Fatalf("publish: %v", err)
		}
	case experience.StatusSuspended:
		exp.Suspend()
	}
	return exp
}

func TestService_Create_HappyPath(t *testing.T) {
	exp := mkExperience(t, experience.StatusPublished)
	svc := NewService(memory.NewExperienceBookingRepository(), &stubFinder{exp: exp}, 0.10)
	b, err := svc.Create(context.Background(), CreateInput{
		ExperienceID: exp.ID, GuestID: uuid.New(),
		StartAt: time.Now().UTC().Add(48 * time.Hour), Guests: 2,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if b.Status != experiencebooking.StatusPending {
		t.Errorf("status got %s want pending", b.Status)
	}
	if got := b.Pricing.Subtotal.AmountCents(); got != 6000 {
		t.Errorf("subtotal got %d want 6000", got)
	}
	if got := b.Pricing.Total.AmountCents(); got != 6600 {
		t.Errorf("total got %d want 6600", got)
	}
}

func TestService_Create_RejectsDraftExperience(t *testing.T) {
	exp := mkExperience(t, experience.StatusDraft)
	svc := NewService(memory.NewExperienceBookingRepository(), &stubFinder{exp: exp}, 0.10)
	_, err := svc.Create(context.Background(), CreateInput{
		ExperienceID: exp.ID, GuestID: uuid.New(),
		StartAt: time.Now().UTC().Add(48 * time.Hour), Guests: 1,
	})
	if err == nil {
		t.Fatal("expected error for draft experience")
	}
}

func TestService_Create_RejectsOverlappingSession(t *testing.T) {
	exp := mkExperience(t, experience.StatusPublished)
	repo := memory.NewExperienceBookingRepository()
	svc := NewService(repo, &stubFinder{exp: exp}, 0.10)
	start := time.Now().UTC().Add(48 * time.Hour)
	if _, err := svc.Create(context.Background(), CreateInput{
		ExperienceID: exp.ID, GuestID: uuid.New(), StartAt: start, Guests: 1,
	}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	// Second booking same start → conflict.
	_, err := svc.Create(context.Background(), CreateInput{
		ExperienceID: exp.ID, GuestID: uuid.New(), StartAt: start, Guests: 1,
	})
	if !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestService_Cancel_GuestAllowed(t *testing.T) {
	exp := mkExperience(t, experience.StatusPublished)
	svc := NewService(memory.NewExperienceBookingRepository(), &stubFinder{exp: exp}, 0.10)
	guest := uuid.New()
	b, _ := svc.Create(context.Background(), CreateInput{
		ExperienceID: exp.ID, GuestID: guest,
		StartAt: time.Now().UTC().Add(48 * time.Hour), Guests: 1,
	})
	got, err := svc.Cancel(context.Background(), guest, b.ID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if got.Status != experiencebooking.StatusCancelled {
		t.Errorf("status got %s want cancelled", got.Status)
	}
}

func TestService_Cancel_HostAllowed(t *testing.T) {
	exp := mkExperience(t, experience.StatusPublished)
	svc := NewService(memory.NewExperienceBookingRepository(), &stubFinder{exp: exp}, 0.10)
	b, _ := svc.Create(context.Background(), CreateInput{
		ExperienceID: exp.ID, GuestID: uuid.New(),
		StartAt: time.Now().UTC().Add(48 * time.Hour), Guests: 1,
	})
	if _, err := svc.Cancel(context.Background(), exp.HostID, b.ID); err != nil {
		t.Fatalf("host cancel: %v", err)
	}
}

func TestService_Cancel_StrangerForbidden(t *testing.T) {
	exp := mkExperience(t, experience.StatusPublished)
	svc := NewService(memory.NewExperienceBookingRepository(), &stubFinder{exp: exp}, 0.10)
	b, _ := svc.Create(context.Background(), CreateInput{
		ExperienceID: exp.ID, GuestID: uuid.New(),
		StartAt: time.Now().UTC().Add(48 * time.Hour), Guests: 1,
	})
	_, err := svc.Cancel(context.Background(), uuid.New(), b.ID)
	if !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestService_Confirm_HostOnly(t *testing.T) {
	exp := mkExperience(t, experience.StatusPublished)
	svc := NewService(memory.NewExperienceBookingRepository(), &stubFinder{exp: exp}, 0.10)
	b, _ := svc.Create(context.Background(), CreateInput{
		ExperienceID: exp.ID, GuestID: uuid.New(),
		StartAt: time.Now().UTC().Add(48 * time.Hour), Guests: 1,
	})
	// Guest cannot confirm.
	if _, err := svc.Confirm(context.Background(), b.GuestID, b.ID); !errors.Is(err, shared.ErrForbidden) {
		t.Errorf("guest confirm: expected ErrForbidden, got %v", err)
	}
	got, err := svc.Confirm(context.Background(), exp.HostID, b.ID)
	if err != nil {
		t.Fatalf("host confirm: %v", err)
	}
	if got.Status != experiencebooking.StatusConfirmed {
		t.Errorf("status got %s want confirmed", got.Status)
	}
}

// TestService_Create_DispatchesEvents proves a successful Create hands the
// in-process dispatcher exactly one ExperienceBookingCreated event carrying
// the new aggregate's identity, host, guest and total. The Money snapshot
// is captured as cents+currency so future durable-outbox routing can
// (de)serialise without exposing shared.Money's unexported fields.
func TestService_Create_DispatchesEvents(t *testing.T) {
	exp := mkExperience(t, experience.StatusPublished)
	rec := &recordingDispatcher{}
	svc := NewService(memory.NewExperienceBookingRepository(), &stubFinder{exp: exp}, 0.10).
		WithDispatcher(rec)
	guest := uuid.New()
	b, err := svc.Create(context.Background(), CreateInput{
		ExperienceID: exp.ID, GuestID: guest,
		StartAt: time.Now().UTC().Add(48 * time.Hour), Guests: 2,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	names := rec.names()
	if len(names) != 1 || names[0] != "experiencebooking.created" {
		t.Fatalf("dispatched names = %v, want [experiencebooking.created]", names)
	}
	created, ok := rec.events[0].(experiencebooking.ExperienceBookingCreated)
	if !ok {
		t.Fatalf("dispatched event = %T, want ExperienceBookingCreated", rec.events[0])
	}
	if created.BookingID != b.ID {
		t.Errorf("BookingID got %s, want %s", created.BookingID, b.ID)
	}
	if created.ExperienceID != exp.ID {
		t.Errorf("ExperienceID got %s, want %s", created.ExperienceID, exp.ID)
	}
	if created.HostID != exp.HostID {
		t.Errorf("HostID got %s, want %s", created.HostID, exp.HostID)
	}
	if created.GuestID != guest {
		t.Errorf("GuestID got %s, want %s", created.GuestID, guest)
	}
	if created.TotalCents != b.Pricing.Total.AmountCents() || created.Currency != b.Pricing.Total.Currency() {
		t.Errorf("money snapshot got %d %s, want %d %s",
			created.TotalCents, created.Currency,
			b.Pricing.Total.AmountCents(), b.Pricing.Total.Currency())
	}
	if created.OccurredAt.IsZero() {
		t.Error("OccurredAt should be set")
	}
}

// TestService_Confirm_DispatchesEventOnceAndIsIdempotent proves the first
// host Confirm fires ExperienceBookingConfirmed, but a second Confirm on
// the now-confirmed booking does NOT re-publish — subscribers would
// otherwise double-charge or double-notify.
func TestService_Confirm_DispatchesEventOnceAndIsIdempotent(t *testing.T) {
	exp := mkExperience(t, experience.StatusPublished)
	rec := &recordingDispatcher{}
	svc := NewService(memory.NewExperienceBookingRepository(), &stubFinder{exp: exp}, 0.10).
		WithDispatcher(rec)
	b, _ := svc.Create(context.Background(), CreateInput{
		ExperienceID: exp.ID, GuestID: uuid.New(),
		StartAt: time.Now().UTC().Add(48 * time.Hour), Guests: 1,
	})
	if _, err := svc.Confirm(context.Background(), exp.HostID, b.ID); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if _, err := svc.Confirm(context.Background(), exp.HostID, b.ID); err != nil {
		t.Fatalf("re-confirm: %v", err)
	}
	names := rec.names()
	want := []string{"experiencebooking.created", "experiencebooking.confirmed"}
	if len(names) != len(want) {
		t.Fatalf("dispatched names = %v, want %v", names, want)
	}
	for i, n := range want {
		if names[i] != n {
			t.Errorf("dispatched[%d] = %q, want %q", i, names[i], n)
		}
	}
	confirmed, ok := rec.events[1].(experiencebooking.ExperienceBookingConfirmed)
	if !ok {
		t.Fatalf("second event = %T, want ExperienceBookingConfirmed", rec.events[1])
	}
	if confirmed.BookingID != b.ID || confirmed.HostID != exp.HostID {
		t.Errorf("confirmed identity mismatch: %+v", confirmed)
	}
}

// TestService_Cancel_DispatchesEventOnceAndIsIdempotent mirrors the confirm
// test for the cancellation transition.
func TestService_Cancel_DispatchesEventOnceAndIsIdempotent(t *testing.T) {
	exp := mkExperience(t, experience.StatusPublished)
	rec := &recordingDispatcher{}
	svc := NewService(memory.NewExperienceBookingRepository(), &stubFinder{exp: exp}, 0.10).
		WithDispatcher(rec)
	guest := uuid.New()
	b, _ := svc.Create(context.Background(), CreateInput{
		ExperienceID: exp.ID, GuestID: guest,
		StartAt: time.Now().UTC().Add(48 * time.Hour), Guests: 1,
	})
	if _, err := svc.Cancel(context.Background(), guest, b.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := svc.Cancel(context.Background(), guest, b.ID); err != nil {
		t.Fatalf("re-cancel: %v", err)
	}
	names := rec.names()
	want := []string{"experiencebooking.created", "experiencebooking.cancelled"}
	if len(names) != len(want) {
		t.Fatalf("dispatched names = %v, want %v", names, want)
	}
	cancelled, ok := rec.events[1].(experiencebooking.ExperienceBookingCancelled)
	if !ok {
		t.Fatalf("second event = %T, want ExperienceBookingCancelled", rec.events[1])
	}
	if cancelled.BookingID != b.ID || cancelled.CancelledBy != guest {
		t.Errorf("cancelled identity mismatch: %+v", cancelled)
	}
}

// TestService_NilDispatcherFallsBackToNop proves the service constructed
// without WithDispatcher (or with nil) doesn't panic on the publish call —
// it falls back to event.Nop().
func TestService_NilDispatcherFallsBackToNop(t *testing.T) {
	exp := mkExperience(t, experience.StatusPublished)
	svc := NewService(memory.NewExperienceBookingRepository(), &stubFinder{exp: exp}, 0.10).
		WithDispatcher(nil)
	if _, err := svc.Create(context.Background(), CreateInput{
		ExperienceID: exp.ID, GuestID: uuid.New(),
		StartAt: time.Now().UTC().Add(48 * time.Hour), Guests: 1,
	}); err != nil {
		t.Fatalf("create with nil dispatcher: %v", err)
	}
}

// mutableClock lets the scheduler tests fast-forward time so they can
// stage past-session bookings without bypassing the NewSession future-
// start invariant (S82).
type mutableClock struct{ t time.Time }

func (c *mutableClock) Now() time.Time           { return c.t }
func (c *mutableClock) Advance(d time.Duration)  { c.t = c.t.Add(d) }

func TestService_AutoCompleteOverdue_FlipsConfirmedBookings(t *testing.T) {
	exp := mkExperience(t, experience.StatusPublished)
	repo := memory.NewExperienceBookingRepository()
	clk := &mutableClock{t: time.Now().UTC()}
	svc := NewService(repo, &stubFinder{exp: exp}, 0.10, WithClock(clk.Now))

	// Session starts 1h after "now"; duration is 120 minutes (from mkExperience).
	start := clk.Now().Add(1 * time.Hour)
	b, err := svc.Create(context.Background(), CreateInput{
		ExperienceID: exp.ID, GuestID: uuid.New(), StartAt: start, Guests: 1,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Confirm(context.Background(), exp.HostID, b.ID); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	// Advance past the session end (start + 120m + a minute of slack).
	clk.Advance(3*time.Hour + time.Minute)

	completed, err := svc.AutoCompleteOverdue(context.Background())
	if err != nil {
		t.Fatalf("auto-complete: %v", err)
	}
	if completed != 1 {
		t.Errorf("completed got %d want 1", completed)
	}

	got, err := svc.Get(context.Background(), b.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != experiencebooking.StatusCompleted {
		t.Errorf("status got %s want completed", got.Status)
	}
}

func TestService_AutoCompleteOverdue_SkipsPending(t *testing.T) {
	exp := mkExperience(t, experience.StatusPublished)
	repo := memory.NewExperienceBookingRepository()
	clk := &mutableClock{t: time.Now().UTC()}
	svc := NewService(repo, &stubFinder{exp: exp}, 0.10, WithClock(clk.Now))

	b, err := svc.Create(context.Background(), CreateInput{
		ExperienceID: exp.ID, GuestID: uuid.New(),
		StartAt: clk.Now().Add(1 * time.Hour), Guests: 1,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Pending — NEVER confirmed.

	clk.Advance(3*time.Hour + time.Minute)

	completed, err := svc.AutoCompleteOverdue(context.Background())
	if err != nil {
		t.Fatalf("auto-complete: %v", err)
	}
	if completed != 0 {
		t.Errorf("completed got %d want 0", completed)
	}

	got, _ := svc.Get(context.Background(), b.ID)
	if got.Status != experiencebooking.StatusPending {
		t.Errorf("status got %s want pending", got.Status)
	}
}

func TestService_AutoCompleteOverdue_SkipsAlreadyCompleted(t *testing.T) {
	exp := mkExperience(t, experience.StatusPublished)
	repo := memory.NewExperienceBookingRepository()
	clk := &mutableClock{t: time.Now().UTC()}
	svc := NewService(repo, &stubFinder{exp: exp}, 0.10, WithClock(clk.Now))

	b, err := svc.Create(context.Background(), CreateInput{
		ExperienceID: exp.ID, GuestID: uuid.New(),
		StartAt: clk.Now().Add(1 * time.Hour), Guests: 1,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Confirm(context.Background(), exp.HostID, b.ID); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	// Advance past the session end and flip once.
	clk.Advance(3*time.Hour + time.Minute)
	if _, err := svc.AutoCompleteOverdue(context.Background()); err != nil {
		t.Fatalf("first auto-complete: %v", err)
	}

	// Second pass: idempotent — no further completions.
	clk.Advance(10 * time.Minute)
	completed, err := svc.AutoCompleteOverdue(context.Background())
	if err != nil {
		t.Fatalf("second auto-complete: %v", err)
	}
	if completed != 0 {
		t.Errorf("second pass completed got %d want 0", completed)
	}

	got, _ := svc.Get(context.Background(), b.ID)
	if got.Status != experiencebooking.StatusCompleted {
		t.Errorf("status got %s want completed", got.Status)
	}
}

func TestService_ListMine_ReturnsGuestsBookings(t *testing.T) {
	exp := mkExperience(t, experience.StatusPublished)
	svc := NewService(memory.NewExperienceBookingRepository(), &stubFinder{exp: exp}, 0.10)
	g1, g2 := uuid.New(), uuid.New()
	_, _ = svc.Create(context.Background(), CreateInput{ExperienceID: exp.ID, GuestID: g1, StartAt: time.Now().UTC().Add(48 * time.Hour), Guests: 1})
	_, _ = svc.Create(context.Background(), CreateInput{ExperienceID: exp.ID, GuestID: g2, StartAt: time.Now().UTC().Add(72 * time.Hour), Guests: 1})
	res, err := svc.ListMine(context.Background(), g1, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("list mine: %v", err)
	}
	if res.Total != 1 {
		t.Errorf("total got %d want 1", res.Total)
	}
}
