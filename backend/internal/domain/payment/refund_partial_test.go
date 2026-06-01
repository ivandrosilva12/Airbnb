package payment

import (
	"testing"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// newCaptured returns a Payment in the Captured state with the given amount,
// ready to receive partial refunds.
func newCaptured(t *testing.T, amount int64, currency string) *Payment {
	t.Helper()
	money, err := shared.NewMoney(amount, currency)
	if err != nil {
		t.Fatalf("money: %v", err)
	}
	p := New(uuid.New(), uuid.New(), money)
	if err := p.Authorize("ref-1"); err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if err := p.Capture(); err != nil {
		t.Fatalf("capture: %v", err)
	}
	return p
}

func TestRefundPartial_CumulativeSumNeverExceedsCaptured(t *testing.T) {
	p := newCaptured(t, 10000, "EUR")
	disputeA := uuid.New()
	disputeB := uuid.New()

	// First partial refund.
	if _, err := p.RefundPartial(3000, "first claim", "dispute", disputeA); err != nil {
		t.Fatalf("first refund: %v", err)
	}
	if p.RefundedCents != 3000 {
		t.Fatalf("refunded after first = %d, want 3000", p.RefundedCents)
	}
	if p.Status != StatusCaptured {
		t.Fatalf("status after first = %s, want still captured", p.Status)
	}

	// Second partial refund — still within the cap.
	if _, err := p.RefundPartial(4000, "second claim", "dispute", disputeB); err != nil {
		t.Fatalf("second refund: %v", err)
	}
	if p.RefundedCents != 7000 {
		t.Fatalf("refunded after second = %d, want 7000", p.RefundedCents)
	}
	if p.Status != StatusCaptured {
		t.Fatalf("status after second = %s, want still captured", p.Status)
	}

	// A third refund that would overflow the captured amount is rejected and
	// must not mutate the payment.
	if _, err := p.RefundPartial(4000, "overflow", "dispute", uuid.New()); err == nil {
		t.Fatal("expected overflow refund to be rejected")
	}
	if p.RefundedCents != 7000 {
		t.Fatalf("refunded after rejected = %d, want 7000 (unchanged)", p.RefundedCents)
	}
	if len(p.Adjustments) != 2 {
		t.Fatalf("adjustments after rejected = %d, want 2 (unchanged)", len(p.Adjustments))
	}

	// A refund that exactly tops up to the captured amount is accepted and
	// flips the status to refunded.
	if _, err := p.RefundPartial(3000, "final", "dispute", uuid.New()); err != nil {
		t.Fatalf("final refund: %v", err)
	}
	if p.RefundedCents != 10000 {
		t.Fatalf("refunded final = %d, want 10000", p.RefundedCents)
	}
	if p.Status != StatusRefunded {
		t.Fatalf("status final = %s, want refunded", p.Status)
	}
	if len(p.Adjustments) != 3 {
		t.Fatalf("adjustments final = %d, want 3", len(p.Adjustments))
	}
}

func TestRefundPartial_RejectsNonPositiveAmount(t *testing.T) {
	p := newCaptured(t, 5000, "EUR")
	for _, amt := range []int64{0, -1, -1000} {
		if _, err := p.RefundPartial(amt, "x", "dispute", uuid.New()); err == nil {
			t.Fatalf("expected refund of %d to be rejected", amt)
		}
	}
	if p.RefundedCents != 0 || len(p.Adjustments) != 0 {
		t.Fatalf("payment mutated by rejected refunds: refunded=%d adjustments=%d", p.RefundedCents, len(p.Adjustments))
	}
}

func TestRefundPartial_RejectedOnTerminalStates(t *testing.T) {
	// Refunded payment can no longer accept further partial refunds.
	p := newCaptured(t, 1000, "EUR")
	if _, err := p.RefundPartial(1000, "full", "dispute", uuid.New()); err != nil {
		t.Fatalf("seed full refund: %v", err)
	}
	if p.Status != StatusRefunded {
		t.Fatalf("status = %s, want refunded", p.Status)
	}
	if _, err := p.RefundPartial(1, "x", "dispute", uuid.New()); err == nil {
		t.Fatal("expected refund after full refund to be rejected")
	}

	// Failed payment is not refundable either.
	failed := newCaptured(t, 1000, "EUR")
	failed.Fail("card declined")
	if _, err := failed.RefundPartial(100, "x", "dispute", uuid.New()); err == nil {
		t.Fatal("expected refund on failed payment to be rejected")
	}
}

func TestRecordDamageClaim_AccumulatesAndIsAuditOnly(t *testing.T) {
	p := newCaptured(t, 10000, "EUR")
	originalStatus := p.Status

	if _, err := p.RecordDamageClaim(2500, "broken vase", "dispute", uuid.New()); err != nil {
		t.Fatalf("first damage: %v", err)
	}
	if _, err := p.RecordDamageClaim(1500, "stained sofa", "dispute", uuid.New()); err != nil {
		t.Fatalf("second damage: %v", err)
	}
	if p.DamageClaimCents != 4000 {
		t.Fatalf("damage total = %d, want 4000", p.DamageClaimCents)
	}
	if p.Status != originalStatus {
		t.Fatalf("status changed to %s, damage claims must be audit-only", p.Status)
	}
	if len(p.Adjustments) != 2 {
		t.Fatalf("adjustments = %d, want 2", len(p.Adjustments))
	}
	if p.Adjustments[0].Kind != AdjustmentDamageClaim {
		t.Fatalf("kind = %s, want %s", p.Adjustments[0].Kind, AdjustmentDamageClaim)
	}
	// Damage claims must not bleed into the refund total.
	if p.RefundedCents != 0 {
		t.Fatalf("refunded = %d, want 0 (damage must not affect refunds)", p.RefundedCents)
	}
}

func TestRecordDamageClaim_RejectsNonPositive(t *testing.T) {
	p := newCaptured(t, 1000, "EUR")
	for _, amt := range []int64{0, -1} {
		if _, err := p.RecordDamageClaim(amt, "x", "dispute", uuid.New()); err == nil {
			t.Fatalf("expected damage of %d to be rejected", amt)
		}
	}
}

func TestHasAdjustmentFor_FlagsExistingAndIgnoresOther(t *testing.T) {
	p := newCaptured(t, 5000, "EUR")
	disputeID := uuid.New()
	if _, err := p.RefundPartial(500, "x", "dispute", disputeID); err != nil {
		t.Fatalf("seed refund: %v", err)
	}
	if !p.HasAdjustmentFor(AdjustmentRefund, "dispute", disputeID) {
		t.Fatal("expected HasAdjustmentFor to find the refund")
	}
	if p.HasAdjustmentFor(AdjustmentDamageClaim, "dispute", disputeID) {
		t.Fatal("damage kind should not match a refund row")
	}
	if p.HasAdjustmentFor(AdjustmentRefund, "dispute", uuid.New()) {
		t.Fatal("a different dispute id must not match")
	}
}
