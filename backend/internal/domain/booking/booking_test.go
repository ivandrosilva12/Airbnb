package booking

import (
	"testing"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

func futureRange(t *testing.T, startOffsetDays, nights int) DateRange {
	t.Helper()
	start := time.Now().UTC().AddDate(0, 0, startOffsetDays)
	dr, err := NewDateRange(start, start.AddDate(0, 0, nights))
	if err != nil {
		t.Fatalf("unexpected error building range: %v", err)
	}
	return dr
}

func TestNewDateRange_RejectsPastAndInverted(t *testing.T) {
	past := time.Now().UTC().AddDate(0, 0, -1)
	if _, err := NewDateRange(past, past.AddDate(0, 0, 2)); err == nil {
		t.Fatal("expected error for check-in in the past")
	}
	future := time.Now().UTC().AddDate(0, 0, 5)
	if _, err := NewDateRange(future, future); err == nil {
		t.Fatal("expected error for check-out not after check-in")
	}
}

func TestDateRange_Overlaps(t *testing.T) {
	a := futureRange(t, 1, 3) // days 1..4
	b := futureRange(t, 3, 3) // days 3..6 -> overlaps
	c := futureRange(t, 4, 2) // days 4..6 -> adjacent (half-open) -> no overlap

	if !a.Overlaps(b) {
		t.Error("expected a and b to overlap")
	}
	if a.Overlaps(c) {
		t.Error("expected adjacent ranges not to overlap (half-open)")
	}
}

func TestNewBooking_DerivesTotalPriceFromNights(t *testing.T) {
	price, _ := shared.NewMoney(5000, "EUR")    // 50.00 EUR / night
	cleaning, _ := shared.NewMoney(2000, "EUR") // 20.00 EUR cleaning
	dates := futureRange(t, 2, 4)               // 4 nights

	b, err := NewBooking(uuid.New(), uuid.New(), dates, 2, price, cleaning, 0.10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// subtotal 200.00 + cleaning 20.00 = 220.00; service fee 10% = 22.00; total 242.00.
	if got := b.Pricing.Subtotal.AmountCents(); got != 20000 {
		t.Errorf("subtotal = %d cents, want 20000", got)
	}
	if got := b.Pricing.ServiceFee.AmountCents(); got != 2200 {
		t.Errorf("service fee = %d cents, want 2200", got)
	}
	if got := b.TotalPrice().AmountCents(); got != 24200 {
		t.Errorf("total price = %d cents, want 24200", got)
	}
	if b.Status != StatusPending {
		t.Errorf("status = %q, want pending", b.Status)
	}
}

func TestBooking_StatusTransitions(t *testing.T) {
	price, _ := shared.NewMoney(1000, "USD")
	cleaning, _ := shared.NewMoney(0, "USD")
	b, _ := NewBooking(uuid.New(), uuid.New(), futureRange(t, 1, 1), 1, price, cleaning, 0.12)

	if err := b.Complete(); err == nil {
		t.Error("expected completing a pending booking to fail")
	}
	if err := b.Confirm(); err != nil {
		t.Fatalf("confirm failed: %v", err)
	}
	if err := b.Complete(); err != nil {
		t.Fatalf("complete failed: %v", err)
	}
	if err := b.Cancel(); err == nil {
		t.Error("expected cancelling a completed booking to fail")
	}
}
