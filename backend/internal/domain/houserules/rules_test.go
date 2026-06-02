package houserules

import (
	"errors"
	"strings"
	"testing"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// TestNew_HappyPath_StartsAtVersion1 confirms the first authoring
// always lands at version 1 — every other version-handling test
// downstream relies on this invariant.
func TestNew_HappyPath_StartsAtVersion1(t *testing.T) {
	prop := uuid.New()
	r, err := New(prop, []string{"No smoking", "No parties"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if r.Version != 1 {
		t.Errorf("Version = %d, want 1", r.Version)
	}
	if len(r.Items) != 2 {
		t.Errorf("Items = %v, want 2 items", r.Items)
	}
	if r.PropertyID != prop {
		t.Errorf("PropertyID = %v, want %v", r.PropertyID, prop)
	}
}

// TestNew_RejectsEmptyProperty proves the propertyID guard fires —
// a Save with a zero PropertyID would corrupt the unique index in
// postgres so the domain rejects it before persistence is attempted.
func TestNew_RejectsEmptyProperty(t *testing.T) {
	_, err := New(uuid.Nil, []string{"x"})
	if !errors.Is(err, shared.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

// TestNew_TrimsAndRejectsBlank confirms whitespace-only entries
// are rejected (a host saving "  " by accident would otherwise
// produce a blank rule the UI renders as a phantom bullet).
func TestNew_TrimsAndRejectsBlank(t *testing.T) {
	r, err := New(uuid.New(), []string{"  Quiet hours  "})
	if err != nil {
		t.Fatalf("trim: %v", err)
	}
	if r.Items[0] != "Quiet hours" {
		t.Errorf("Items[0] = %q, want trimmed 'Quiet hours'", r.Items[0])
	}
	if _, err := New(uuid.New(), []string{"   "}); !errors.Is(err, shared.ErrValidation) {
		t.Errorf("expected ErrValidation for blank-only rule, got %v", err)
	}
}

// TestNew_RejectsOversizedItem trips the per-rule length cap. The
// cap exists to keep the UI from breaking; the domain enforces it.
func TestNew_RejectsOversizedItem(t *testing.T) {
	huge := strings.Repeat("x", maxItemLength+1)
	_, err := New(uuid.New(), []string{huge})
	if !errors.Is(err, shared.ErrValidation) {
		t.Errorf("expected ErrValidation for oversized rule, got %v", err)
	}
}

// TestNew_RejectsTooManyItems trips the item-count cap.
func TestNew_RejectsTooManyItems(t *testing.T) {
	many := make([]string, maxItems+1)
	for i := range many {
		many[i] = "rule"
	}
	_, err := New(uuid.New(), many)
	if !errors.Is(err, shared.ErrValidation) {
		t.Errorf("expected ErrValidation for too many rules, got %v", err)
	}
}

// TestBump_IncrementsVersion_LeavesOriginalUntouched is the test that
// guards the "history is immutable" invariant. If a future refactor
// makes Bump mutate the receiver, this test fails — and so do all
// of S47's compliance promises.
func TestBump_IncrementsVersion_LeavesOriginalUntouched(t *testing.T) {
	r, err := New(uuid.New(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	nxt, err := r.Bump([]string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("Bump: %v", err)
	}
	if nxt.Version != 2 {
		t.Errorf("next Version = %d, want 2", nxt.Version)
	}
	if r.Version != 1 {
		t.Errorf("original mutated: Version = %d, want 1", r.Version)
	}
	if len(r.Items) != 2 || len(nxt.Items) != 3 {
		t.Errorf("items mutated: r=%d nxt=%d", len(r.Items), len(nxt.Items))
	}
}

// TestActive_OnlyTrueWithItems shows the "active = needs ack" gate.
// A cleared rule set (empty items even at version > 0) lets the
// booking flow skip the acknowledgement requirement.
func TestActive_OnlyTrueWithItems(t *testing.T) {
	if (*Rules)(nil).Active() {
		t.Errorf("nil receiver should not be active")
	}
	empty, _ := New(uuid.New(), nil)
	if empty.Active() {
		t.Errorf("empty rule set should not be active")
	}
	one, _ := New(uuid.New(), []string{"only"})
	if !one.Active() {
		t.Errorf("non-empty rule set should be active")
	}
}

// TestNewAcceptance_RejectsInvalidInputs is the symmetric guard for
// the Acceptance entity. Each missing field produces a 422.
func TestNewAcceptance_RejectsInvalidInputs(t *testing.T) {
	cases := []struct {
		name                                string
		booking, guest, property            uuid.UUID
		version                             int
	}{
		{"missing booking", uuid.Nil, uuid.New(), uuid.New(), 1},
		{"missing guest", uuid.New(), uuid.Nil, uuid.New(), 1},
		{"missing property", uuid.New(), uuid.New(), uuid.Nil, 1},
		{"zero version", uuid.New(), uuid.New(), uuid.New(), 0},
		{"negative version", uuid.New(), uuid.New(), uuid.New(), -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewAcceptance(c.booking, c.guest, c.property, c.version)
			if !errors.Is(err, shared.ErrValidation) {
				t.Errorf("expected ErrValidation, got %v", err)
			}
		})
	}
}

// TestNewAcceptance_HappyPath confirms the constructor populates
// every field including a non-zero AcceptedAt.
func TestNewAcceptance_HappyPath(t *testing.T) {
	b, g, p := uuid.New(), uuid.New(), uuid.New()
	a, err := NewAcceptance(b, g, p, 3)
	if err != nil {
		t.Fatalf("NewAcceptance: %v", err)
	}
	if a.BookingID != b || a.GuestID != g || a.PropertyID != p || a.AcceptedVersion != 3 {
		t.Errorf("unexpected fields: %+v", a)
	}
	if a.AcceptedAt.IsZero() {
		t.Errorf("AcceptedAt should be set")
	}
}
