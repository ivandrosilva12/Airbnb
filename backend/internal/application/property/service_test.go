package propertyapp_test

import (
	"context"
	"testing"

	propertyapp "github.com/airhost/backend/internal/application/property"
	"github.com/airhost/backend/internal/domain/audit"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

// noopIdentityVerifier satisfies propertyapp.IdentityVerifier without
// touching real KYC state. The Suspend / Unsuspend paths never call it
// (KYC gating is only consulted on Publish), so a no-op is sufficient
// to keep the constructor happy.
type noopIdentityVerifier struct{}

func (noopIdentityVerifier) IsVerified(_ context.Context, _ uuid.UUID) (bool, error) {
	return true, nil
}

// serviceFixture wires the property application service against in-memory
// repos and seeds a suspended-able listing owned by a host. Each test gets
// its own fixture so concurrent test runs don't share state.
type serviceFixture struct {
	svc      *propertyapp.Service
	props    *memory.PropertyRepository
	host     uuid.UUID
	admin    uuid.UUID
	property *property.Property
}

func newServiceFixture(t *testing.T) *serviceFixture {
	t.Helper()
	ctx := context.Background()
	props := memory.NewPropertyRepository()
	// Storage is unused by Suspend / Unsuspend. The handler-tested upload
	// path lives elsewhere; passing nil here would blow up if anyone wires
	// a CreateInput with a photo, which this test doesn't do.
	svc := propertyapp.NewService(props, nil, noopIdentityVerifier{}, false)

	host := uuid.New()
	admin := uuid.New()
	addr := property.Address{Line1: "1 Main", City: "Lisbon", Country: "PT", PostalCode: "1000-001"}
	price, _ := shared.NewMoney(10000, "EUR")
	cleaning, _ := shared.NewMoney(0, "EUR")
	p, err := property.NewProperty(host, "Test Flat", "desc", property.TypeApartment, addr, price, cleaning, 2, 1, 1, 1, nil)
	if err != nil {
		t.Fatalf("seed property: %v", err)
	}
	if err := props.Create(ctx, p); err != nil {
		t.Fatalf("save property: %v", err)
	}
	return &serviceFixture{svc: svc, props: props, host: host, admin: admin, property: p}
}

// recordingAuditor records propertyapp.AuditRecord calls so a test can assert
// the property service wrote exactly one audit row, with the expected
// action, actor, subject (target) id and metadata shape (S124). Mirrors
// the cohost service's recordingAuditor in cohost_service_test.go — both
// services share the same package-local Auditor interface, so the
// recorder is identical.
type recordingAuditor struct {
	records []propertyapp.AuditRecord
}

func (f *recordingAuditor) Record(_ context.Context, rec propertyapp.AuditRecord) error {
	f.records = append(f.records, rec)
	return nil
}

// TestSuspend_RecordsAuditEvent verifies that a successful Suspend appends
// a "property.suspended" audit record with the acting admin as actor, the
// listing as subject (target id), and the listing title in metadata so
// the row is self-contained for compliance review even if the listing is
// later deleted (S124). Mirrors TestInvite_RecordsAuditEvent.
func TestSuspend_RecordsAuditEvent(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	auditor := &recordingAuditor{}
	f.svc.WithAuditor(auditor)

	if _, err := f.svc.Suspend(ctx, f.admin, f.property.ID); err != nil {
		t.Fatalf("suspend: %v", err)
	}

	if len(auditor.records) != 1 {
		t.Fatalf("audit record count = %d, want 1", len(auditor.records))
	}
	rec := auditor.records[0]
	if rec.Action != audit.ActionPropertySuspended {
		t.Fatalf("action = %q, want %q", rec.Action, audit.ActionPropertySuspended)
	}
	if rec.ActorID != f.admin {
		t.Fatalf("actor id = %v, want admin id %v", rec.ActorID, f.admin)
	}
	if rec.TargetID != f.property.ID {
		t.Fatalf("target id = %v, want property id %v", rec.TargetID, f.property.ID)
	}
	if rec.TargetType != audit.TargetProperty {
		t.Fatalf("target type = %q, want %q", rec.TargetType, audit.TargetProperty)
	}
	if got, want := rec.Metadata["title"], f.property.Title; got != want {
		t.Fatalf("metadata title = %v, want %v", got, want)
	}
}

// TestUnsuspend_RecordsAuditEvent verifies the mirror path: a successful
// Unsuspend appends a "property.unsuspended" audit record with the same
// shape (S124).
func TestUnsuspend_RecordsAuditEvent(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	// Suspend first (with no auditor) so Unsuspend has a state to flip.
	if _, err := f.svc.Suspend(ctx, f.admin, f.property.ID); err != nil {
		t.Fatalf("seed suspend: %v", err)
	}
	auditor := &recordingAuditor{}
	f.svc.WithAuditor(auditor)

	if _, err := f.svc.Unsuspend(ctx, f.admin, f.property.ID); err != nil {
		t.Fatalf("unsuspend: %v", err)
	}

	if len(auditor.records) != 1 {
		t.Fatalf("audit record count = %d, want 1", len(auditor.records))
	}
	rec := auditor.records[0]
	if rec.Action != audit.ActionPropertyUnsuspended {
		t.Fatalf("action = %q, want %q", rec.Action, audit.ActionPropertyUnsuspended)
	}
	if rec.ActorID != f.admin {
		t.Fatalf("actor id = %v, want admin id %v", rec.ActorID, f.admin)
	}
	if rec.TargetID != f.property.ID {
		t.Fatalf("target id = %v, want property id %v", rec.TargetID, f.property.ID)
	}
	if rec.TargetType != audit.TargetProperty {
		t.Fatalf("target type = %q, want %q", rec.TargetType, audit.TargetProperty)
	}
}

// TestSuspend_NoAuditor_NoOp confirms the nil-auditor short-circuit: a
// Service constructed without WithAuditor still suspends successfully and
// does not panic on the nil sink. Locks the "older tests keep passing"
// invariant the package documents.
func TestSuspend_NoAuditor_NoOp(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	if _, err := f.svc.Suspend(ctx, f.admin, f.property.ID); err != nil {
		t.Fatalf("suspend without auditor: %v", err)
	}
}
