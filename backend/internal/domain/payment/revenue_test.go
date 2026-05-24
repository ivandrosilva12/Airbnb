package payment

import "testing"

func TestRevenueAccumulator_NetsRefundsAndPicksDominantCurrency(t *testing.T) {
	a := NewRevenueAccumulator()
	// EUR: one captured (10000) + one refunded charge (8000 charged, 3000 back → keeps 5000).
	a.Add("EUR", StatusCaptured, 10000, 0)
	a.Add("EUR", StatusRefunded, 8000, 3000)
	a.Add("EUR", StatusAuthorized, 2000, 0)
	// AOA: a smaller captured amount that must not be summed into EUR.
	a.Add("AOA", StatusCaptured, 1000, 0)
	// A fully-released hold contributes nothing.
	a.Add("EUR", StatusRefunded, 4000, 4000)

	rev := a.Dominant()
	if rev.Currency != "EUR" {
		t.Fatalf("currency = %q, want EUR (dominant)", rev.Currency)
	}
	if rev.CapturedCents != 15000 { // 10000 + 5000 kept + 0 released
		t.Fatalf("captured = %d, want 15000", rev.CapturedCents)
	}
	if rev.PendingCents != 2000 {
		t.Fatalf("pending = %d, want 2000", rev.PendingCents)
	}
}

func TestRevenueAccumulator_Empty(t *testing.T) {
	if rev := NewRevenueAccumulator().Dominant(); rev != (Revenue{}) {
		t.Fatalf("empty accumulator = %+v, want zero", rev)
	}
}
