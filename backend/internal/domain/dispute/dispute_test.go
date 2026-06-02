package dispute_test

import (
	"testing"
	"time"

	"github.com/airhost/backend/internal/domain/dispute"
	"github.com/google/uuid"
)

func TestDueAtAndIsOverdue(t *testing.T) {
	d, err := dispute.New(uuid.New(), uuid.New(), dispute.KindRefund, "x", 100, "EUR")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	want := d.OpenedAt.Add(dispute.SLAWindow)
	if !d.DueAt().Equal(want) {
		t.Fatalf("DueAt = %v, want %v", d.DueAt(), want)
	}
	// Fresh dispute is not overdue.
	if d.IsOverdueAt(d.OpenedAt.Add(1 * time.Minute)) {
		t.Fatalf("fresh dispute reported overdue")
	}
	// Right at the deadline is NOT yet overdue (strictly-after semantics).
	if d.IsOverdueAt(d.DueAt()) {
		t.Fatalf("at-the-deadline reported overdue, want strictly-after")
	}
	// One second past is overdue.
	if !d.IsOverdueAt(d.DueAt().Add(1 * time.Second)) {
		t.Fatalf("past-deadline not reported overdue")
	}
	// Terminal disputes are never overdue — the moderator's clock stopped
	// at the decision (even a late decision doesn't keep ticking).
	admin := uuid.New()
	if err := d.AdminResolve(admin, "decided"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if d.IsOverdueAt(d.DueAt().Add(30 * 24 * time.Hour)) {
		t.Fatalf("terminal dispute reported overdue")
	}
}

func TestNew_HappyPath(t *testing.T) {
	booking := uuid.New()
	opener := uuid.New()
	d, err := dispute.New(booking, opener, dispute.KindRefund, "Dirty linens", 5000, "eur")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if d.Status != dispute.StatusOpen {
		t.Fatalf("status = %s, want open", d.Status)
	}
	if d.Currency != "EUR" {
		t.Fatalf("currency normalisation: got %q", d.Currency)
	}
	if d.RequestedAmountCents != 5000 {
		t.Fatalf("amount: got %d", d.RequestedAmountCents)
	}
}

func TestNew_RejectsBadInputs(t *testing.T) {
	booking := uuid.New()
	opener := uuid.New()
	cases := []struct {
		name      string
		bookingID uuid.UUID
		opener    uuid.UUID
		kind      dispute.Kind
		reason    string
		amount    int64
		currency  string
	}{
		{"missing booking", uuid.Nil, opener, dispute.KindRefund, "x", 100, "EUR"},
		{"missing opener", booking, uuid.Nil, dispute.KindRefund, "x", 100, "EUR"},
		{"bad kind", booking, opener, dispute.Kind("bogus"), "x", 100, "EUR"},
		{"empty reason", booking, opener, dispute.KindRefund, "   ", 100, "EUR"},
		{"refund without amount", booking, opener, dispute.KindRefund, "x", 0, "EUR"},
		{"damage without currency", booking, opener, dispute.KindDamage, "x", 100, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := dispute.New(tc.bookingID, tc.opener, tc.kind, tc.reason, tc.amount, tc.currency); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestNew_OtherKindHasNoMoneyFigure(t *testing.T) {
	d, err := dispute.New(uuid.New(), uuid.New(), dispute.KindOther, "Argumentative host", 999, "EUR")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if d.RequestedAmountCents != 0 {
		t.Fatalf("amount should be zeroed for 'other' kind, got %d", d.RequestedAmountCents)
	}
}

func TestAddEvidence(t *testing.T) {
	d, _ := dispute.New(uuid.New(), uuid.New(), dispute.KindRefund, "x", 100, "EUR")
	contributor := uuid.New()
	if _, err := d.AddEvidence(contributor, "", "Note here"); err != nil {
		t.Fatalf("add note: %v", err)
	}
	if _, err := d.AddEvidence(contributor, "http://x/y.jpg", ""); err != nil {
		t.Fatalf("add url: %v", err)
	}
	if len(d.Evidence) != 2 {
		t.Fatalf("expected 2 evidence, got %d", len(d.Evidence))
	}
	if _, err := d.AddEvidence(contributor, "", ""); err == nil {
		t.Fatalf("expected error on empty evidence")
	}
}

func TestHostRespond_TransitionsToUnderReview(t *testing.T) {
	d, _ := dispute.New(uuid.New(), uuid.New(), dispute.KindDamage, "Broken TV", 30000, "EUR")
	if err := d.HostRespond("Counter-claim: tenant did it"); err != nil {
		t.Fatalf("respond: %v", err)
	}
	if d.Status != dispute.StatusUnderReview {
		t.Fatalf("status = %s, want under_review", d.Status)
	}
}

func TestResolveAndReject_AreTerminal(t *testing.T) {
	admin := uuid.New()

	d, _ := dispute.New(uuid.New(), uuid.New(), dispute.KindRefund, "x", 100, "EUR")
	if err := d.AdminResolve(admin, "Refunding 50%"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if d.Status != dispute.StatusResolved || d.DecidedAt == nil {
		t.Fatalf("post-resolve state: %+v", d)
	}
	if err := d.AdminResolve(admin, "second attempt"); err == nil {
		t.Fatalf("expected error on second decision")
	}

	d2, _ := dispute.New(uuid.New(), uuid.New(), dispute.KindDamage, "x", 100, "EUR")
	if err := d2.AdminReject(admin, "Insufficient evidence"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if d2.Status != dispute.StatusRejected {
		t.Fatalf("post-reject status: %s", d2.Status)
	}
	if _, err := d2.AddEvidence(uuid.New(), "", "n"); err == nil {
		t.Fatalf("expected error: cannot append to closed dispute")
	}
}
