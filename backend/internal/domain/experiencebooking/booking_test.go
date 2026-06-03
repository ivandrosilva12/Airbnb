package experiencebooking

import (
	"testing"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

func futureSession(t *testing.T, hoursFromNow, duration int) Session {
	t.Helper()
	start := time.Now().UTC().Add(time.Duration(hoursFromNow) * time.Hour)
	s, err := NewSession(start, duration, 30, 24*60)
	if err != nil {
		t.Fatalf("unexpected error building session: %v", err)
	}
	return s
}

func TestNewSession_RejectsPastAndOutOfRange(t *testing.T) {
	past := time.Now().UTC().Add(-time.Hour)
	if _, err := NewSession(past, 90, 30, 24*60); err == nil {
		t.Fatal("expected error for past start")
	}
	future := time.Now().UTC().Add(2 * time.Hour)
	if _, err := NewSession(future, 10, 30, 24*60); err == nil {
		t.Fatal("expected error for under-minimum duration")
	}
	if _, err := NewSession(future, 24*60+1, 30, 24*60); err == nil {
		t.Fatal("expected error for over-maximum duration")
	}
}

func TestSession_EndAt(t *testing.T) {
	s := futureSession(t, 24, 90)
	if got := s.EndAt(); !got.Equal(s.StartAt.Add(90 * time.Minute)) {
		t.Errorf("EndAt mismatch: %v", got)
	}
}

func mkBooking(t *testing.T, guests, cap int) *Booking {
	t.Helper()
	price, _ := shared.NewMoney(2500, "EUR")
	b, err := NewBooking(uuid.New(), uuid.New(), uuid.New(), futureSession(t, 48, 90), guests, cap, price, 0.10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return b
}

func TestNewBooking_DerivesPricing(t *testing.T) {
	b := mkBooking(t, 2, 8) // 25.00 × 2 = 50.00 ; fee 10% = 5.00 ; total 55.00
	if got := b.Pricing.Subtotal.AmountCents(); got != 5000 {
		t.Errorf("subtotal got %d, want 5000", got)
	}
	if got := b.Pricing.ServiceFee.AmountCents(); got != 500 {
		t.Errorf("fee got %d, want 500", got)
	}
	if got := b.Pricing.Total.AmountCents(); got != 5500 {
		t.Errorf("total got %d, want 5500", got)
	}
	if b.Status != StatusPending {
		t.Errorf("status got %s, want pending", b.Status)
	}
}

func TestNewBooking_RejectsOverCap(t *testing.T) {
	price, _ := shared.NewMoney(1000, "EUR")
	if _, err := NewBooking(uuid.New(), uuid.New(), uuid.New(), futureSession(t, 24, 90), 9, 8, price, 0.10); err == nil {
		t.Fatal("expected error for guests > cap")
	}
}

func TestNewBooking_RejectsHostBookingOwnExperience(t *testing.T) {
	price, _ := shared.NewMoney(1000, "EUR")
	host := uuid.New()
	if _, err := NewBooking(uuid.New(), host, host, futureSession(t, 24, 90), 1, 8, price, 0.10); err == nil {
		t.Fatal("expected error for host booking own experience")
	}
}

func TestBooking_LifecycleHappy(t *testing.T) {
	b := mkBooking(t, 1, 8)
	if err := b.Confirm(); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if b.Status != StatusConfirmed {
		t.Errorf("status got %s, want confirmed", b.Status)
	}
	// Complete must wait until end-of-session.
	if err := b.Complete(time.Now().UTC()); err == nil {
		t.Fatal("expected error completing before session end")
	}
	if err := b.Complete(b.Session.EndAt().Add(time.Minute)); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if b.Status != StatusCompleted {
		t.Errorf("status got %s, want completed", b.Status)
	}
}

func TestBooking_CancelBeforeStart(t *testing.T) {
	b := mkBooking(t, 1, 8)
	if err := b.Cancel(time.Now().UTC()); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if b.Status != StatusCancelled {
		t.Errorf("status got %s, want cancelled", b.Status)
	}
	// Idempotent.
	if err := b.Cancel(time.Now().UTC()); err != nil {
		t.Errorf("re-cancel should be no-op: %v", err)
	}
}

func TestBooking_CancelAfterStartRejected(t *testing.T) {
	b := mkBooking(t, 1, 8)
	if err := b.Cancel(b.Session.StartAt.Add(time.Minute)); err == nil {
		t.Fatal("expected error cancelling after session start")
	}
}

func TestBooking_CompleteOnlyConfirmed(t *testing.T) {
	b := mkBooking(t, 1, 8) // pending
	if err := b.Complete(b.Session.EndAt().Add(time.Minute)); err == nil {
		t.Fatal("expected error completing pending booking")
	}
}

func TestComputePricing_ZeroFeeRate(t *testing.T) {
	price, _ := shared.NewMoney(10000, "USD")
	p, err := ComputePricing(price, 3, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ServiceFee.AmountCents() != 0 {
		t.Errorf("fee got %d, want 0", p.ServiceFee.AmountCents())
	}
	if p.Total.AmountCents() != 30000 {
		t.Errorf("total got %d, want 30000", p.Total.AmountCents())
	}
}

func TestComputePricing_RejectsBadInputs(t *testing.T) {
	price, _ := shared.NewMoney(1000, "EUR")
	if _, err := ComputePricing(price, 0, 0.1); err == nil {
		t.Fatal("expected error for guests=0")
	}
	if _, err := ComputePricing(price, 2, -0.1); err == nil {
		t.Fatal("expected error for negative fee rate")
	}
	if _, err := ComputePricing(price, 2, 1.1); err == nil {
		t.Fatal("expected error for fee rate > 1")
	}
}

// TestLifecycleEvents_EventNamesAreStable pins the stable wire names of the
// S85 lifecycle events. Subscribers + Prometheus metric labels depend on
// these strings — flipping one is a breaking change that would silently
// break alerts and downstream filters.
func TestLifecycleEvents_EventNamesAreStable(t *testing.T) {
	if got := (ExperienceBookingCreated{}).EventName(); got != "experiencebooking.created" {
		t.Errorf("created EventName = %q, want experiencebooking.created", got)
	}
	if got := (ExperienceBookingConfirmed{}).EventName(); got != "experiencebooking.confirmed" {
		t.Errorf("confirmed EventName = %q, want experiencebooking.confirmed", got)
	}
	if got := (ExperienceBookingCancelled{}).EventName(); got != "experiencebooking.cancelled" {
		t.Errorf("cancelled EventName = %q, want experiencebooking.cancelled", got)
	}
}

// TestLifecycleEvents_CarryAggregateIdentity proves each event embeds the
// booking identity (id + experience id) so a subscriber doesn't have to
// re-read the aggregate to know which booking transitioned. The Created
// event additionally carries the money snapshot and the host id.
func TestLifecycleEvents_CarryAggregateIdentity(t *testing.T) {
	bid, exp, host, guest := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC()

	created := ExperienceBookingCreated{
		BookingID:    bid,
		ExperienceID: exp,
		HostID:       host,
		GuestID:      guest,
		TotalCents:   5500,
		Currency:     "EUR",
		OccurredAt:   now,
	}
	if created.BookingID != bid || created.ExperienceID != exp || created.HostID != host || created.GuestID != guest {
		t.Errorf("created identity mismatch: %+v", created)
	}
	if created.TotalCents != 5500 || created.Currency != "EUR" {
		t.Errorf("created money mismatch: %d %s", created.TotalCents, created.Currency)
	}

	confirmed := ExperienceBookingConfirmed{BookingID: bid, ExperienceID: exp, HostID: host, GuestID: guest, OccurredAt: now}
	if confirmed.BookingID != bid || confirmed.HostID != host {
		t.Errorf("confirmed identity mismatch: %+v", confirmed)
	}

	cancelled := ExperienceBookingCancelled{BookingID: bid, ExperienceID: exp, CancelledBy: guest, OccurredAt: now}
	if cancelled.BookingID != bid || cancelled.CancelledBy != guest {
		t.Errorf("cancelled identity mismatch: %+v", cancelled)
	}
}
