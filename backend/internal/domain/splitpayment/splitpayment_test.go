package splitpayment

import (
	"errors"
	"testing"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

func TestNewSplitPaymentInvariants(t *testing.T) {
	bookingID, organizerID := uuid.New(), uuid.New()

	t.Run("happy path", func(t *testing.T) {
		sp, err := New(bookingID, organizerID, "Org@Test.dev", "EUR", 30000, []ShareInput{
			{Email: "org@test.dev", AmountCents: 10000},
			{Email: "friend@test.dev", AmountCents: 20000},
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if sp.Status != StatusPending {
			t.Fatalf("status = %q, want %q", sp.Status, StatusPending)
		}
		if len(sp.Shares) != 2 {
			t.Fatalf("shares = %d, want 2", len(sp.Shares))
		}
		if sp.Currency != "EUR" {
			t.Fatalf("currency = %q, want EUR", sp.Currency)
		}
		// Emails are case-folded.
		if sp.Shares[0].PayerEmail != "org@test.dev" {
			t.Fatalf("first share email = %q, want org@test.dev", sp.Shares[0].PayerEmail)
		}
	})

	cases := []struct {
		name   string
		total  int64
		shares []ShareInput
		want   error
	}{
		{
			"one share rejected",
			10000,
			[]ShareInput{{Email: "org@test.dev", AmountCents: 10000}},
			shared.ErrValidation,
		},
		{
			"shares must sum exactly",
			30000,
			[]ShareInput{
				{Email: "org@test.dev", AmountCents: 10000},
				{Email: "friend@test.dev", AmountCents: 15000},
			},
			shared.ErrValidation,
		},
		{
			"negative share rejected",
			10000,
			[]ShareInput{
				{Email: "org@test.dev", AmountCents: 15000},
				{Email: "friend@test.dev", AmountCents: -5000},
			},
			shared.ErrValidation,
		},
		{
			"duplicate email rejected",
			20000,
			[]ShareInput{
				{Email: "org@test.dev", AmountCents: 10000},
				{Email: "ORG@test.dev", AmountCents: 10000},
			},
			shared.ErrValidation,
		},
		{
			"organizer must be a payer",
			20000,
			[]ShareInput{
				{Email: "a@test.dev", AmountCents: 10000},
				{Email: "b@test.dev", AmountCents: 10000},
			},
			shared.ErrValidation,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(bookingID, organizerID, "org@test.dev", "EUR", tc.total, tc.shares); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestMarkSharePaid(t *testing.T) {
	sp, err := New(uuid.New(), uuid.New(), "org@test.dev", "EUR", 30000, []ShareInput{
		{Email: "org@test.dev", AmountCents: 10000},
		{Email: "friend@test.dev", AmountCents: 20000},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	orgShare := sp.Shares[0]
	friendShare := sp.Shares[1]

	// Wrong email → forbidden.
	if err := sp.MarkSharePaid(orgShare.ID, uuid.New(), "stranger@test.dev"); !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("wrong email: got %v, want ErrForbidden", err)
	}

	// Unknown share id → not found.
	if err := sp.MarkSharePaid(uuid.New(), uuid.New(), "org@test.dev"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("unknown share: got %v, want ErrNotFound", err)
	}

	// First share authorised.
	if err := sp.MarkSharePaid(orgShare.ID, uuid.New(), "ORG@test.dev"); err != nil {
		t.Fatalf("mark first paid: %v", err)
	}
	if sp.AllPaid() {
		t.Fatalf("AllPaid should be false with one share still pending")
	}

	// Double-paying the same share rejected.
	if err := sp.MarkSharePaid(orgShare.ID, uuid.New(), "org@test.dev"); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("double-pay: got %v, want ErrConflict", err)
	}

	// Second share authorised.
	if err := sp.MarkSharePaid(friendShare.ID, uuid.New(), "friend@test.dev"); err != nil {
		t.Fatalf("mark second paid: %v", err)
	}
	if !sp.AllPaid() {
		t.Fatalf("AllPaid should be true after both shares paid")
	}

	// Now we can complete.
	if err := sp.MarkCompleted(); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}
	if sp.Status != StatusCompleted {
		t.Fatalf("status = %q, want %q", sp.Status, StatusCompleted)
	}
	if sp.CompletedAt == nil {
		t.Fatalf("CompletedAt should be set")
	}

	// After completion, no more share mutations.
	if err := sp.MarkSharePaid(orgShare.ID, uuid.New(), "org@test.dev"); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("post-completion pay: got %v, want ErrConflict", err)
	}
}

func TestCancelTransitions(t *testing.T) {
	sp, _ := New(uuid.New(), uuid.New(), "org@test.dev", "EUR", 30000, []ShareInput{
		{Email: "org@test.dev", AmountCents: 10000},
		{Email: "friend@test.dev", AmountCents: 20000},
	})
	if err := sp.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if sp.Status != StatusCancelled || sp.CancelledAt == nil {
		t.Fatalf("after cancel: status=%q cancelledAt=%v", sp.Status, sp.CancelledAt)
	}
	// Double-cancel rejected.
	if err := sp.Cancel(); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("double cancel: got %v, want ErrConflict", err)
	}
	// Cancelled splits can't be completed.
	if err := sp.MarkCompleted(); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("complete after cancel: got %v, want ErrConflict", err)
	}
}

func TestHasPayerCaseInsensitive(t *testing.T) {
	sp, _ := New(uuid.New(), uuid.New(), "org@test.dev", "EUR", 20000, []ShareInput{
		{Email: "Org@Test.dev", AmountCents: 10000},
		{Email: "friend@test.dev", AmountCents: 10000},
	})
	for _, addr := range []string{"org@test.dev", "ORG@TEST.DEV", "  Org@Test.dev  ", "friend@test.dev"} {
		if !sp.HasPayer(addr) {
			t.Fatalf("HasPayer(%q) = false, want true", addr)
		}
	}
	if sp.HasPayer("stranger@test.dev") {
		t.Fatalf("HasPayer(stranger) = true, want false")
	}
}
