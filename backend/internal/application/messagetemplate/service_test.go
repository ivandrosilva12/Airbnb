package messagetemplateapp_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	messagetemplateapp "github.com/airhost/backend/internal/application/messagetemplate"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

// TestServiceCRUD exercises the happy path through Create → Update → Delete
// with the in-memory repo. Ownership is implicit (single actor) so the
// negative gates live in their own tests below.
func TestServiceCRUD(t *testing.T) {
	repo := memory.NewMessageTemplateRepository()
	svc := messagetemplateapp.NewService(repo)
	ctx := context.Background()
	host := uuid.New()

	tpl, err := svc.Create(ctx, host, "WiFi", "Wifi password is foo")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if tpl.Label != "WiFi" {
		t.Fatalf("label = %q, want WiFi", tpl.Label)
	}

	updated, err := svc.Update(ctx, host, tpl.ID, "WiFi v2", "Wifi password is bar")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Body != "Wifi password is bar" {
		t.Fatalf("body = %q, not updated", updated.Body)
	}

	mine, err := svc.ListMine(ctx, host)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(mine) != 1 {
		t.Fatalf("list len = %d, want 1", len(mine))
	}

	if err := svc.Delete(ctx, host, tpl.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	mine, _ = svc.ListMine(ctx, host)
	if len(mine) != 0 {
		t.Fatalf("after delete list len = %d, want 0", len(mine))
	}
}

// TestServiceOwnershipGate confirms a host cannot edit or delete another
// host's template. Returns shared.ErrForbidden via the owned() gate.
func TestServiceOwnershipGate(t *testing.T) {
	repo := memory.NewMessageTemplateRepository()
	svc := messagetemplateapp.NewService(repo)
	ctx := context.Background()
	alice := uuid.New()
	bob := uuid.New()

	tpl, err := svc.Create(ctx, alice, "Greeting", "Hi there!")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := svc.Update(ctx, bob, tpl.ID, "Hijack", "evil"); !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("update by stranger: err = %v, want ErrForbidden", err)
	}
	if err := svc.Delete(ctx, bob, tpl.ID); !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("delete by stranger: err = %v, want ErrForbidden", err)
	}

	// Bob's ListMine is empty even though Alice has one.
	bobs, _ := svc.ListMine(ctx, bob)
	if len(bobs) != 0 {
		t.Fatalf("bob ListMine len = %d, want 0", len(bobs))
	}
}

// TestServiceLabelLengthInvariant confirms the domain length cap is enforced
// at the service boundary (no separate validation needed in transport).
func TestServiceLabelLengthInvariant(t *testing.T) {
	repo := memory.NewMessageTemplateRepository()
	svc := messagetemplateapp.NewService(repo)
	ctx := context.Background()
	host := uuid.New()

	long := strings.Repeat("a", 200)
	if _, err := svc.Create(ctx, host, long, "body"); err == nil {
		t.Fatalf("expected error on overlong label, got nil")
	}
}

// TestServiceUpdateMissingReturnsNotFound — the FindByID-then-gate path
// surfaces ErrNotFound (not ErrForbidden) when the template simply doesn't
// exist, so callers can distinguish "stale id" from "wrong actor".
func TestServiceUpdateMissingReturnsNotFound(t *testing.T) {
	repo := memory.NewMessageTemplateRepository()
	svc := messagetemplateapp.NewService(repo)
	ctx := context.Background()
	host := uuid.New()

	if _, err := svc.Update(ctx, host, uuid.New(), "x", "y"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("update missing: err = %v, want ErrNotFound", err)
	}
}
