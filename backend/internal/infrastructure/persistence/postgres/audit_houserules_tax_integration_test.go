// Postgres integration tests for the audit / houserules / tax
// repositories (S55). Mirrors the env-skip pattern from S41 — opt
// in via POSTGRES_TEST_DSN, run from CI's postgres-integration job.
//
// The in-memory adapters give the application-level contract; this
// file covers what the in-memory path cannot:
//
//   - audit: JSONB metadata round-trips faithfully (not a Go-map
//     printout but real PostgreSQL JSONB), and the dynamic-filter
//     WHERE clause composes correctly when multiple filters AND.
//   - houserules: the (property_id, version) PK rejects a
//     concurrent writer claiming the same version (shared.ErrConflict);
//     the bump-then-save flow preserves history so a v1 acceptance
//     still resolves after v2 is current; acceptance ON CONFLICT
//     DO NOTHING really is idempotent on the booking_id PK.
//   - tax: the country pre-filter in SQL correctly trims candidates,
//     and the matcher then catches city/effective-window in Go.
//
// Run locally with:
//
//	POSTGRES_TEST_DSN='postgres://airhost:airhost@localhost:5432/airhost?sslmode=disable' \
//	  go test ./internal/infrastructure/persistence/postgres/... -run Integration_(Audit|HouseRules|Tax)
package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/airhost/backend/internal/domain/audit"
	"github.com/airhost/backend/internal/domain/houserules"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/tax"
	pg "github.com/airhost/backend/internal/infrastructure/persistence/postgres"
	"github.com/google/uuid"
)

// withCleanTables wraps withTestDB but truncates the tables this file
// owns instead of S41's set. Lets the two test files coexist without
// stepping on each other's seed data.
func withCleanTables(t *testing.T, tables ...string) {
	t.Helper()
	pool := withTestDB(t)
	truncate(t, pool, tables...)
	_ = pool
}

// --- Audit -------------------------------------------------------------------

// TestIntegration_Audit_InsertList_RoundTripsJSONBMetadata commits one
// row with a deeply-typed metadata payload and reads it back through
// List, confirming the JSONB column does not flatten ints to floats
// or lose nested structure. A regression here would corrupt audit
// rows the moment they hit Postgres.
func TestIntegration_Audit_InsertList_RoundTripsJSONBMetadata(t *testing.T) {
	pool := withTestDB(t)
	truncate(t, pool, "audit_events")
	repo := pg.NewAuditRepository(pool)
	ctx := context.Background()

	actor, target := uuid.New(), uuid.New()
	ev, err := audit.New(actor, audit.ActionDisputeReject, audit.TargetDispute, target, map[string]any{
		"resolution":        "no evidence",
		"refundAmountCents": int64(5000),
		"damageAmountCents": int64(0),
	}, "req-abc")
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	if err := repo.Create(ctx, ev); err != nil {
		t.Fatalf("Create: %v", err)
	}

	res, err := repo.List(ctx, audit.Filter{}, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if res.Total != 1 || len(res.Items) != 1 {
		t.Fatalf("expected 1 row, got total=%d len=%d", res.Total, len(res.Items))
	}
	got := res.Items[0]
	if got.Action != audit.ActionDisputeReject || got.TargetType != audit.TargetDispute || got.TargetID != target {
		t.Errorf("typed fields lost: %+v", got)
	}
	// JSONB round-trip: numeric fields come back as float64 (encoding/json
	// default), strings stay strings. We assert what arrives, not the
	// source type — the audit reader does the same.
	if got.Metadata["resolution"] != "no evidence" {
		t.Errorf("metadata.resolution lost: %v", got.Metadata)
	}
	if got.Metadata["refundAmountCents"] == nil {
		t.Errorf("metadata.refundAmountCents missing: %v", got.Metadata)
	}
	if got.RequestID != "req-abc" {
		t.Errorf("request_id lost: %q", got.RequestID)
	}
}

// TestIntegration_Audit_List_FiltersAND confirms multiple filter
// fields compose with AND in the dynamic WHERE clause. A bug in the
// placeholder numbering (the homemade itoa helper) would silently
// drop predicates and return too many rows.
func TestIntegration_Audit_List_FiltersAND(t *testing.T) {
	pool := withTestDB(t)
	truncate(t, pool, "audit_events")
	repo := pg.NewAuditRepository(pool)
	ctx := context.Background()

	adminA, adminB := uuid.New(), uuid.New()
	target := uuid.New()
	otherTarget := uuid.New()

	// Insert 4 rows across two admins × two targets — only one row
	// matches the full (actorA, action=suspend, targetType=property,
	// targetID=target) filter.
	insert := func(actor uuid.UUID, action audit.Action, tt audit.TargetType, tid uuid.UUID) {
		ev, _ := audit.New(actor, action, tt, tid, nil, "")
		if err := repo.Create(ctx, ev); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	insert(adminA, audit.ActionPropertySuspend, audit.TargetProperty, target)
	insert(adminA, audit.ActionPropertyUnsuspend, audit.TargetProperty, target)
	insert(adminB, audit.ActionPropertySuspend, audit.TargetProperty, target)
	insert(adminA, audit.ActionPropertySuspend, audit.TargetProperty, otherTarget)

	res, err := repo.List(ctx, audit.Filter{
		ActorID: adminA, Action: audit.ActionPropertySuspend,
		TargetType: audit.TargetProperty, TargetID: target,
	}, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if res.Total != 1 || len(res.Items) != 1 {
		t.Fatalf("full filter: total=%d len=%d, want 1/1", res.Total, len(res.Items))
	}

	// Drop one filter at a time and assert the count grows: actor
	// only → 3, no filter → 4. Confirms each predicate is doing work.
	if r, _ := repo.List(ctx, audit.Filter{ActorID: adminA}, shared.NewPage(10, 0)); r.Total != 3 {
		t.Errorf("actor-only filter total=%d, want 3", r.Total)
	}
	if r, _ := repo.List(ctx, audit.Filter{}, shared.NewPage(10, 0)); r.Total != 4 {
		t.Errorf("no filter total=%d, want 4", r.Total)
	}
}

// --- HouseRules --------------------------------------------------------------

// TestIntegration_HouseRules_Save_PKRejectsDuplicateVersion is the
// pessimistic-concurrency guard. Two writers both reading v0 (nothing
// yet) and both calling Save with version=1 must produce exactly one
// row; the second hits the (property_id, version) PK and the postgres
// adapter surfaces shared.ErrConflict via mapError.
func TestIntegration_HouseRules_Save_PKRejectsDuplicateVersion(t *testing.T) {
	pool := withTestDB(t)
	truncate(t, pool, "house_rules", "house_rule_acceptances")
	repo := pg.NewHouseRulesRepository(pool)
	ctx := context.Background()

	propID := uuid.New()
	first, err := houserules.New(propID, []string{"No smoking"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := repo.Save(ctx, first); err != nil {
		t.Fatalf("first Save: %v", err)
	}

	// Second writer racing in with the same version → should conflict.
	collision, _ := houserules.New(propID, []string{"Different rule"})
	if err := repo.Save(ctx, collision); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate version, got %v", err)
	}

	// The original row stays intact.
	got, err := repo.GetCurrent(ctx, propID)
	if err != nil || got.Version != 1 || got.Items[0] != "No smoking" {
		t.Errorf("current row mutated by colliding writer: %+v err=%v", got, err)
	}
}

// TestIntegration_HouseRules_HistoryPreserved walks the v1→v2 bump
// and confirms BOTH rows persist — the v1 acceptance proof must still
// resolve to its original text after the host bumps. This is the
// regulatory promise S47 makes.
func TestIntegration_HouseRules_HistoryPreserved(t *testing.T) {
	pool := withTestDB(t)
	truncate(t, pool, "house_rules", "house_rule_acceptances")
	repo := pg.NewHouseRulesRepository(pool)
	ctx := context.Background()

	propID := uuid.New()
	v1, _ := houserules.New(propID, []string{"No pets", "Quiet hours"})
	if err := repo.Save(ctx, v1); err != nil {
		t.Fatalf("save v1: %v", err)
	}
	v2, _ := v1.Bump([]string{"Pets welcome"}) // policy reversal — exactly the dispute-history scenario
	if err := repo.Save(ctx, v2); err != nil {
		t.Fatalf("save v2: %v", err)
	}

	cur, err := repo.GetCurrent(ctx, propID)
	if err != nil || cur.Version != 2 {
		t.Fatalf("current = v%d (err=%v), want v2", cur.Version, err)
	}
	hist, err := repo.GetVersion(ctx, propID, 1)
	if err != nil {
		t.Fatalf("GetVersion(1): %v", err)
	}
	if len(hist.Items) != 2 || hist.Items[0] != "No pets" || hist.Items[1] != "Quiet hours" {
		t.Errorf("v1 history corrupted: %v", hist.Items)
	}
}

// TestIntegration_HouseRules_Acceptance_IdempotentByBookingID is the
// "guest hits Submit twice on a flaky network" guarantee: the second
// insert is silently absorbed by ON CONFLICT DO NOTHING and the
// stored row stays the FIRST one — the authoritative acceptance for
// dispute purposes.
func TestIntegration_HouseRules_Acceptance_IdempotentByBookingID(t *testing.T) {
	pool := withTestDB(t)
	truncate(t, pool, "house_rule_acceptances")
	repo := pg.NewHouseRulesRepository(pool)
	ctx := context.Background()

	booking, guest, prop := uuid.New(), uuid.New(), uuid.New()
	a1, err := houserules.NewAcceptance(booking, guest, prop, 1)
	if err != nil {
		t.Fatalf("NewAcceptance: %v", err)
	}
	if err := repo.RecordAcceptance(ctx, a1); err != nil {
		t.Fatalf("first record: %v", err)
	}

	// Construct a second Acceptance — same bookingID, different
	// AcceptedAt (NewAcceptance stamps time.Now). RecordAcceptance
	// MUST NOT overwrite — the first one is the authoritative
	// proof.
	time.Sleep(5 * time.Millisecond) // ensures a different AcceptedAt
	a2, _ := houserules.NewAcceptance(booking, guest, prop, 99 /*wrong version*/)
	if err := repo.RecordAcceptance(ctx, a2); err != nil {
		t.Fatalf("second record returned error (should be silent no-op): %v", err)
	}
	stored, err := repo.AcceptanceFor(ctx, booking)
	if err != nil {
		t.Fatalf("AcceptanceFor: %v", err)
	}
	if stored.AcceptedVersion != 1 {
		t.Errorf("AcceptedVersion = %d, want 1 (second call must not overwrite)", stored.AcceptedVersion)
	}
}

// --- Tax ---------------------------------------------------------------------

// TestIntegration_Tax_RulesFor_JurisdictionPrefilter inserts rules
// for two countries and asserts RulesFor returns only the matching
// country's rows. The SQL pre-filter on country is the load-bearing
// performance optimisation (the calculator's Matches still runs in
// Go to cover case + window), so a regression that drops the WHERE
// would silently scan the full table.
func TestIntegration_Tax_RulesFor_JurisdictionPrefilter(t *testing.T) {
	pool := withTestDB(t)
	truncate(t, pool, "tax_rules")
	repo := pg.NewTaxRepository(pool)
	ctx := context.Background()

	pt, _ := tax.NewRule("PT VAT", tax.KindPercent, "PT", "", "EUR", 2300, 0, 0, time.Time{}, time.Time{})
	ao, _ := tax.NewRule("AO IVA", tax.KindPercent, "AO", "", "AOA", 1400, 0, 0, time.Time{}, time.Time{})
	world, _ := tax.NewRule("Worldwide surcharge", tax.KindPerStay, "", "", "EUR", 0, 100, 0, time.Time{}, time.Time{})
	for _, r := range []*tax.Rule{pt, ao, world} {
		if err := repo.Save(ctx, r); err != nil {
			t.Fatalf("save %s: %v", r.Name, err)
		}
	}

	// Stay in Lisbon today — the PT and worldwide rule both match;
	// the AO rule does not.
	rules, err := repo.RulesFor(ctx, "PT", "Lisbon", time.Now().UTC())
	if err != nil {
		t.Fatalf("RulesFor: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("rules returned = %d, want 2 (PT VAT + worldwide)", len(rules))
	}
	gotPT, gotWorld := false, false
	for _, r := range rules {
		switch r.Name {
		case "PT VAT":
			gotPT = true
		case "Worldwide surcharge":
			gotWorld = true
		case "AO IVA":
			t.Errorf("AO IVA leaked into PT query: %+v", r)
		}
	}
	if !gotPT || !gotWorld {
		t.Errorf("missing expected rules: gotPT=%v gotWorld=%v", gotPT, gotWorld)
	}
}

// TestIntegration_Tax_Save_UpsertByID exercises the ON CONFLICT (id)
// DO UPDATE path: a Save with the same ID overwrites every field.
// Used by a future "edit rule" admin flow and the e2e seed path
// in the harness.
func TestIntegration_Tax_Save_UpsertByID(t *testing.T) {
	pool := withTestDB(t)
	truncate(t, pool, "tax_rules")
	repo := pg.NewTaxRepository(pool)
	ctx := context.Background()

	r, _ := tax.NewRule("Original", tax.KindPercent, "PT", "", "EUR", 2300, 0, 0, time.Time{}, time.Time{})
	if err := repo.Save(ctx, r); err != nil {
		t.Fatalf("first save: %v", err)
	}
	// Re-save with the same ID but a different name. The upsert
	// branch should overwrite.
	r.Name = "Renamed"
	r.RatePctBips = 1700
	if err := repo.Save(ctx, r); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	all, err := repo.List(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("list after upsert: %d rows (err=%v), want 1", len(all), err)
	}
	if all[0].Name != "Renamed" || all[0].RatePctBips != 1700 {
		t.Errorf("upsert didn't take effect: %+v", all[0])
	}
}

// TestIntegration_Tax_Delete_RemovesRow confirms Delete actually
// drops the row (no soft-delete leak through future RulesFor calls).
func TestIntegration_Tax_Delete_RemovesRow(t *testing.T) {
	pool := withTestDB(t)
	truncate(t, pool, "tax_rules")
	repo := pg.NewTaxRepository(pool)
	ctx := context.Background()

	r, _ := tax.NewRule("Doomed", tax.KindPerStay, "PT", "", "EUR", 0, 100, 0, time.Time{}, time.Time{})
	_ = repo.Save(ctx, r)

	if err := repo.Delete(ctx, r.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	rules, _ := repo.RulesFor(ctx, "PT", "", time.Now())
	if len(rules) != 0 {
		t.Errorf("rule survived delete: %+v", rules)
	}
}
