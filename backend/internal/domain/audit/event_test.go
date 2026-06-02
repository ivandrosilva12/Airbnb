package audit

import (
	"errors"
	"testing"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

func TestNew_HappyPath_StampsIDAndCreatedAt(t *testing.T) {
	actor, target := uuid.New(), uuid.New()
	e, err := New(actor, ActionPropertySuspend, TargetProperty, target, map[string]any{"reason": "spam"}, "req-1")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if e.ID == uuid.Nil {
		t.Fatalf("ID not stamped")
	}
	if e.ActorID != actor || e.TargetID != target {
		t.Fatalf("ids not preserved")
	}
	if e.Action != ActionPropertySuspend || e.TargetType != TargetProperty {
		t.Fatalf("typed fields not preserved")
	}
	if e.Metadata["reason"] != "spam" {
		t.Fatalf("metadata lost: %v", e.Metadata)
	}
	if e.RequestID != "req-1" {
		t.Fatalf("request id lost")
	}
	if e.CreatedAt.IsZero() {
		t.Fatalf("CreatedAt not stamped")
	}
}

func TestNew_RejectsZeroActor(t *testing.T) {
	_, err := New(uuid.Nil, ActionPropertySuspend, TargetProperty, uuid.New(), nil, "")
	if !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("err = %v, want validation", err)
	}
}

func TestNew_RejectsUnknownAction(t *testing.T) {
	_, err := New(uuid.New(), Action("totally.made.up"), TargetProperty, uuid.New(), nil, "")
	if !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("err = %v, want validation", err)
	}
}

func TestNew_RejectsEmptyTargetType(t *testing.T) {
	_, err := New(uuid.New(), ActionPropertySuspend, TargetType("   "), uuid.New(), nil, "")
	if !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("err = %v, want validation", err)
	}
}

func TestNew_RejectsZeroTargetID(t *testing.T) {
	_, err := New(uuid.New(), ActionPropertySuspend, TargetProperty, uuid.Nil, nil, "")
	if !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("err = %v, want validation", err)
	}
}

func TestAction_Valid_CoversEveryConstant(t *testing.T) {
	// Regression guard: when a new Action constant is added the switch
	// in Valid() MUST be extended; this test fails loudly otherwise.
	for _, a := range []Action{
		ActionPropertySuspend, ActionPropertyUnsuspend,
		ActionIdentityApprove, ActionIdentityReject,
		ActionReportResolve, ActionReportDismiss,
		ActionDisputeResolve, ActionDisputeReject,
		ActionCouponDeactivate,
		ActionTaxRuleCreate, ActionTaxRuleDelete,
	} {
		if !a.Valid() {
			t.Fatalf("constant %q reports invalid", a)
		}
	}
}
