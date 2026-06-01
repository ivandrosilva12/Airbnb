package payment

import (
	"testing"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// newAuthorizedDeposit builds an authorized DepositHold of the given amount.
func newAuthorizedDeposit(t *testing.T, amountCents int64) *DepositHold {
	t.Helper()
	money, err := shared.NewMoney(amountCents, "EUR")
	if err != nil {
		t.Fatalf("money: %v", err)
	}
	d, err := NewDepositHold(uuid.New(), uuid.New(), money)
	if err != nil {
		t.Fatalf("new deposit: %v", err)
	}
	if err := d.Authorize("dep_ref"); err != nil {
		t.Fatalf("authorize: %v", err)
	}
	return d
}

func TestDepositHold_CapturePartialBelowAmount(t *testing.T) {
	d := newAuthorizedDeposit(t, 10000)
	disputeID := uuid.New()
	adj, took, err := d.CapturePartial(3000, "broken vase", "dispute", disputeID)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if took != 3000 {
		t.Fatalf("took = %d, want 3000", took)
	}
	if d.CapturedCents != 3000 || d.Status != DepositPartiallyCaptured {
		t.Fatalf("after capture: captured=%d status=%s, want 3000/partially_captured", d.CapturedCents, d.Status)
	}
	if adj.Kind != AdjustmentDepositCapture || adj.RefID != disputeID || adj.AmountCents != 3000 {
		t.Fatalf("adjustment unexpected: %+v", adj)
	}
}

func TestDepositHold_CaptureClampsToRemaining(t *testing.T) {
	d := newAuthorizedDeposit(t, 5000)
	// Caller asks for more than the deposit can cover — only the remaining
	// amount is taken and the caller learns the gap (5000 - 3000 = the
	// remaining cap on the second call).
	if _, took, err := d.CapturePartial(3000, "x", "dispute", uuid.New()); err != nil || took != 3000 {
		t.Fatalf("first capture: took=%d err=%v", took, err)
	}
	_, took, err := d.CapturePartial(10000, "x", "dispute", uuid.New())
	if err != nil {
		t.Fatalf("second capture: %v", err)
	}
	if took != 2000 {
		t.Fatalf("clamped take = %d, want 2000", took)
	}
	if d.CapturedCents != 5000 || d.Status != DepositCaptured {
		t.Fatalf("after second: captured=%d status=%s, want 5000/captured", d.CapturedCents, d.Status)
	}
}

func TestDepositHold_CaptureRejectsAfterClosed(t *testing.T) {
	d := newAuthorizedDeposit(t, 1000)
	if err := d.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, _, err := d.CapturePartial(100, "x", "dispute", uuid.New()); err == nil {
		t.Fatal("expected capture on released deposit to be rejected")
	}
}

func TestDepositHold_ReleaseIdempotentAndForbidsAfterFullCapture(t *testing.T) {
	d := newAuthorizedDeposit(t, 1000)
	if _, _, err := d.CapturePartial(1000, "total loss", "dispute", uuid.New()); err != nil {
		t.Fatalf("capture all: %v", err)
	}
	if d.Status != DepositCaptured {
		t.Fatalf("status = %s, want captured", d.Status)
	}
	// Once fully captured the deposit cannot be released — there is nothing
	// left to return to the guest.
	if err := d.Release(); err == nil {
		t.Fatal("release after full capture must fail")
	}

	// A second deposit released twice is the no-op idempotent case.
	d2 := newAuthorizedDeposit(t, 1000)
	if err := d2.Release(); err != nil {
		t.Fatalf("first release: %v", err)
	}
	if err := d2.Release(); err != nil {
		t.Fatalf("second release must be a no-op, got: %v", err)
	}
}

func TestDepositHold_CaptureRejectsNonPositive(t *testing.T) {
	d := newAuthorizedDeposit(t, 1000)
	for _, amt := range []int64{0, -1, -1000} {
		if _, took, err := d.CapturePartial(amt, "x", "dispute", uuid.New()); err == nil {
			t.Fatalf("expected capture %d to be rejected (took=%d)", amt, took)
		}
	}
}

func TestDepositHold_HasAdjustmentForIsIdempotencyKey(t *testing.T) {
	d := newAuthorizedDeposit(t, 1000)
	disputeID := uuid.New()
	if _, _, err := d.CapturePartial(100, "x", "dispute", disputeID); err != nil {
		t.Fatalf("seed capture: %v", err)
	}
	if !d.HasAdjustmentFor("dispute", disputeID) {
		t.Fatal("expected the recorded capture to be reported")
	}
	if d.HasAdjustmentFor("dispute", uuid.New()) {
		t.Fatal("a different ref must not match")
	}
}

func TestDepositHold_NewRejectsInvalidInputs(t *testing.T) {
	money, _ := shared.NewMoney(0, "EUR")
	if _, err := NewDepositHold(uuid.New(), uuid.New(), money); err == nil {
		t.Fatal("zero amount must be rejected")
	}
	good, _ := shared.NewMoney(1000, "EUR")
	if _, err := NewDepositHold(uuid.Nil, uuid.New(), good); err == nil {
		t.Fatal("nil bookingID must be rejected")
	}
	if _, err := NewDepositHold(uuid.New(), uuid.Nil, good); err == nil {
		t.Fatal("nil guestID must be rejected")
	}
}
