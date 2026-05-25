package coupon_test

import (
	"testing"
	"time"

	"github.com/airhost/backend/internal/domain/coupon"
)

func TestPercentageDiscount(t *testing.T) {
	c, err := coupon.NewPercentage("save10", 0.10, 0, 0, nil)
	if err != nil {
		t.Fatalf("new percentage: %v", err)
	}
	if c.Code != "SAVE10" {
		t.Errorf("code = %q, want normalized SAVE10", c.Code)
	}
	got, err := c.DiscountFor(20000, "EUR", 3)
	if err != nil {
		t.Fatalf("discount: %v", err)
	}
	if got != 2000 {
		t.Errorf("discount = %d, want 2000 (10%% of 20000)", got)
	}
}

func TestFixedDiscount_CurrencyMustMatchAndIsCapped(t *testing.T) {
	c, err := coupon.NewFixed("flat50", 5000, "EUR", 0, 0, nil)
	if err != nil {
		t.Fatalf("new fixed: %v", err)
	}
	if _, err := c.DiscountFor(10000, "USD", 2); err == nil {
		t.Error("expected a currency mismatch to be rejected")
	}
	// Capped at the subtotal when the fixed amount exceeds it.
	got, err := c.DiscountFor(3000, "EUR", 2)
	if err != nil {
		t.Fatalf("discount: %v", err)
	}
	if got != 3000 {
		t.Errorf("discount = %d, want it capped at the 3000 subtotal", got)
	}
}

func TestMinNightsGate(t *testing.T) {
	c, _ := coupon.NewPercentage("week", 0.2, 7, 0, nil)
	if _, err := c.DiscountFor(10000, "EUR", 5); err == nil {
		t.Error("expected a coupon requiring 7 nights to reject a 5-night stay")
	}
	if _, err := c.DiscountFor(10000, "EUR", 7); err != nil {
		t.Errorf("7-night stay should qualify: %v", err)
	}
}

func TestExpiryAndRedemptionLimit(t *testing.T) {
	future := time.Now().UTC().Add(48 * time.Hour)
	c, _ := coupon.NewPercentage("once", 0.5, 0, 1, &future)
	if _, err := c.DiscountFor(10000, "EUR", 1); err != nil {
		t.Fatalf("first use should be allowed: %v", err)
	}
	if err := c.Redeem(); err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if err := c.Redeem(); err == nil {
		t.Error("expected the second redemption to exceed the limit")
	}
	if _, err := c.DiscountFor(10000, "EUR", 1); err == nil {
		t.Error("expected an exhausted coupon to reject further discounts")
	}
}

func TestExpiredCouponRejected(t *testing.T) {
	// A coupon cannot be minted already expired; force the state to test the guard.
	c, _ := coupon.NewPercentage("late", 0.1, 0, 0, nil)
	past := time.Now().UTC().Add(-time.Hour)
	c.ExpiresAt = &past
	if _, err := c.DiscountFor(10000, "EUR", 1); err == nil {
		t.Error("expected an expired coupon to be rejected")
	}
}

func TestDeactivate(t *testing.T) {
	c, _ := coupon.NewPercentage("gone", 0.1, 0, 0, nil)
	c.Deactivate()
	if _, err := c.DiscountFor(10000, "EUR", 1); err == nil {
		t.Error("expected a deactivated coupon to be rejected")
	}
}
