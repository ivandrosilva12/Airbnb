package identityapp_test

import (
	"context"
	"errors"
	"testing"

	"github.com/airhost/backend/internal/application/event"
	identityapp "github.com/airhost/backend/internal/application/identity"
	"github.com/airhost/backend/internal/domain/identity"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

// recordingPublisher captures published events for assertions.
type recordingPublisher struct{ events []event.Event }

func (p *recordingPublisher) Publish(_ context.Context, e event.Event) {
	p.events = append(p.events, e)
}

func validSubmit() identityapp.SubmitInput {
	return identityapp.SubmitInput{DocumentType: string(identity.DocPassport), DocumentRef: "scan-123", LegalName: "Ada Lovelace"}
}

func TestIdentity_SubmitApproveFlow(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewIdentityRepository()
	pub := &recordingPublisher{}
	svc := identityapp.NewService(repo, pub)

	userID := uuid.New()
	adminID := uuid.New()

	// No verification yet.
	if _, err := svc.GetMine(ctx, userID); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if verified, _ := svc.IsVerified(ctx, userID); verified {
		t.Fatal("user should not be verified before submitting")
	}

	// Submit.
	v, err := svc.Submit(ctx, userID, validSubmit())
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if v.Status != identity.StatusPending {
		t.Fatalf("expected pending, got %s", v.Status)
	}

	// Cannot submit again while pending.
	if _, err := svc.Submit(ctx, userID, validSubmit()); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("expected validation error on duplicate pending submit, got %v", err)
	}

	// Appears in the pending queue.
	pending, err := svc.ListPending(ctx, shared.NewPage(0, 0))
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if pending.Total != 1 {
		t.Fatalf("expected 1 pending, got %d", pending.Total)
	}

	// Approve publishes IdentityVerified.
	approved, err := svc.Approve(ctx, adminID, v.ID)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved.Status != identity.StatusApproved || approved.ReviewerID != adminID {
		t.Fatalf("unexpected approved state: %+v", approved)
	}
	if len(pub.events) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(pub.events))
	}
	if _, ok := pub.events[0].(event.IdentityVerified); !ok {
		t.Fatalf("expected IdentityVerified, got %T", pub.events[0])
	}
	if verified, _ := svc.IsVerified(ctx, userID); !verified {
		t.Fatal("user should be verified after approval")
	}

	// Cannot submit once already verified.
	if _, err := svc.Submit(ctx, userID, validSubmit()); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("expected validation error when already verified, got %v", err)
	}
}

func TestIdentity_RejectAllowsResubmit(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewIdentityRepository()
	svc := identityapp.NewService(repo, nil) // nil publisher → Nop

	userID := uuid.New()
	adminID := uuid.New()

	v, err := svc.Submit(ctx, userID, validSubmit())
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	// Rejection requires a reason.
	if _, err := svc.Reject(ctx, adminID, v.ID, "  "); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("expected validation error for empty reason, got %v", err)
	}

	rejected, err := svc.Reject(ctx, adminID, v.ID, "Document unreadable")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if rejected.Status != identity.StatusRejected || rejected.RejectionReason == "" {
		t.Fatalf("unexpected rejected state: %+v", rejected)
	}

	// Resubmission is allowed after rejection.
	if _, err := svc.Submit(ctx, userID, validSubmit()); err != nil {
		t.Fatalf("resubmit after rejection: %v", err)
	}

	// A decided request cannot be approved.
	if _, err := svc.Approve(ctx, adminID, v.ID); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("expected validation error approving a decided request, got %v", err)
	}
}
