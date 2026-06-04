// Postgres integration test for the GDPR erase sweep (S90 /
// WF-GAP-009). The in-memory harness in privacy/service_test.go proves
// the orchestration is correct; this file proves the actual Postgres
// queries (DELETEs + UPDATEs) behave the way the orchestrator expects
// — only the targeted user is touched, hard-delete tables come back
// empty, anonymise tables retain the row with PII columns scrubbed,
// and the gdpr_erase audit row lands.
//
// Skipped unless POSTGRES_TEST_DSN is set (see integration_test.go).
package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/airhost/backend/internal/domain/audit"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/dispute"
	"github.com/airhost/backend/internal/domain/fraud"
	"github.com/airhost/backend/internal/domain/houserules"
	"github.com/airhost/backend/internal/domain/identity"
	"github.com/airhost/backend/internal/domain/messagetemplate"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/pushtoken"
	"github.com/airhost/backend/internal/domain/savedsearch"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/splitpayment"
	"github.com/airhost/backend/internal/domain/user"
	pg "github.com/airhost/backend/internal/infrastructure/persistence/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestIntegration_PrivacyErase_Sweep seeds every PII-bearing table and
// runs the Postgres-level erase primitives in sequence (the same order
// the privacy service uses). Asserts hard-delete tables empty for the
// erased user, anonymise tables retain rows with PII scrubbed, and
// counterparty data is untouched.
func TestIntegration_PrivacyErase_Sweep(t *testing.T) {
	pool := withTestDBForPrivacy(t)
	ctx := context.Background()

	// --- Seed counterparty + erased user --------------------------------
	users := pg.NewUserRepository(pool)
	jane, _ := user.NewUser("kc-jane-"+uuid.NewString(), "jane-"+uuid.NewString()+"@test.dev", "Jane", user.RoleGuest)
	if err := users.Create(ctx, jane); err != nil {
		t.Fatalf("seed jane: %v", err)
	}
	bob, _ := user.NewUser("kc-bob-"+uuid.NewString(), "bob-"+uuid.NewString()+"@test.dev", "Bob", user.RoleGuest)
	if err := users.Create(ctx, bob); err != nil {
		t.Fatalf("seed bob: %v", err)
	}
	originalJaneEmail := jane.Email

	// Need a property + booking for cascade-FK tables (disputes, splits).
	host, _ := user.NewUser("kc-host-"+uuid.NewString(), "host-"+uuid.NewString()+"@test.dev", "Host", user.RoleHost)
	if err := users.Create(ctx, host); err != nil {
		t.Fatalf("seed host: %v", err)
	}
	addr := property.Address{Line1: "1 Main", City: "Lisbon", Country: "PT"}
	price, _ := shared.NewMoney(10000, "EUR")
	zero, _ := shared.NewMoney(0, "EUR")
	prop, err := property.NewProperty(host.ID, "Test Flat", "desc", property.TypeApartment, addr, price, zero, 2, 1, 1, 1, nil)
	if err != nil {
		t.Fatalf("new property: %v", err)
	}
	props := pg.NewPropertyRepository(pool)
	if err := props.Create(ctx, prop); err != nil {
		t.Fatalf("seed property: %v", err)
	}
	bookingID := seedBookingForUser(t, pool, prop.ID, jane.ID)

	// --- Seed every table -----------------------------------------------

	pushTokens := pg.NewPushTokenRepository(pool)
	ptJane, _ := pushtoken.New(jane.ID, pushtoken.PlatformAndroid, "device-jane-"+uuid.NewString())
	if err := pushTokens.Save(ctx, ptJane); err != nil {
		t.Fatalf("seed push token jane: %v", err)
	}
	ptBob, _ := pushtoken.New(bob.ID, pushtoken.PlatformAndroid, "device-bob-"+uuid.NewString())
	if err := pushTokens.Save(ctx, ptBob); err != nil {
		t.Fatalf("seed push token bob: %v", err)
	}

	savedSearches := pg.NewSavedSearchRepository(pool)
	if err := savedSearches.Create(ctx, &savedsearch.SavedSearch{
		ID: uuid.New(), UserID: jane.ID, Name: "Beach", Query: "city=Lisboa",
		AlertsEnabled: false, LastNotifiedAt: time.Now().UTC(), CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed saved search: %v", err)
	}

	identities := pg.NewIdentityRepository(pool)
	ver, err := identity.NewVerification(jane.ID, identity.DocPassport, "doc-ref-1", "Jane Doe")
	if err != nil {
		t.Fatalf("new verification: %v", err)
	}
	if err := identities.Create(ctx, ver); err != nil {
		t.Fatalf("seed verification: %v", err)
	}

	disputes := pg.NewDisputeRepository(pool)
	d, err := dispute.New(bookingID, jane.ID, dispute.KindRefund,
		"My phone is 555-1234 please refund", 100, "EUR")
	if err != nil {
		t.Fatalf("new dispute: %v", err)
	}
	if _, err := d.AddEvidence(jane.ID, "https://example.com/proof.png", "see attached, my name is Jane"); err != nil {
		t.Fatalf("add evidence: %v", err)
	}
	if err := disputes.Save(ctx, d); err != nil {
		t.Fatalf("seed dispute: %v", err)
	}

	splitPayments := pg.NewSplitPaymentRepository(pool)
	sp, err := splitpayment.New(bookingID, jane.ID, originalJaneEmail, "EUR", 10000,
		[]splitpayment.ShareInput{
			{Email: originalJaneEmail, AmountCents: 5000},
			{Email: bob.Email, AmountCents: 5000},
		})
	if err != nil {
		t.Fatalf("new split: %v", err)
	}
	if err := splitPayments.Create(ctx, sp); err != nil {
		t.Fatalf("seed split: %v", err)
	}

	cohosts := pg.NewCohostRepository(pool)
	cohostProp, err := property.NewProperty(host.ID, "Co-listing", "x", property.TypeApartment, addr, price, zero, 2, 1, 1, 1, nil)
	if err != nil {
		t.Fatalf("new co-prop: %v", err)
	}
	if err := props.Create(ctx, cohostProp); err != nil {
		t.Fatalf("seed co-prop: %v", err)
	}
	c, err := property.NewCohost(cohostProp.ID, jane.ID, []property.CohostPermission{property.PermManageCalendar})
	if err != nil {
		t.Fatalf("new cohost: %v", err)
	}
	if err := cohosts.Create(ctx, c); err != nil {
		t.Fatalf("seed cohost: %v", err)
	}

	templates := pg.NewMessageTemplateRepository(pool)
	mt, err := messagetemplate.New(jane.ID, "Welcome", "Hello!")
	if err != nil {
		t.Fatalf("new template: %v", err)
	}
	if err := templates.Create(ctx, mt); err != nil {
		t.Fatalf("seed template: %v", err)
	}

	houseRules := pg.NewHouseRulesRepository(pool)
	ack, err := houserules.NewAcceptance(bookingID, jane.ID, prop.ID, 1)
	if err != nil {
		t.Fatalf("new acceptance: %v", err)
	}
	if err := houseRules.RecordAcceptance(ctx, ack); err != nil {
		t.Fatalf("seed acceptance: %v", err)
	}

	fraudRepo := pg.NewFraudRepository(pool)
	fa, err := fraud.New(bookingID, jane.ID, []fraud.Signal{
		{Name: fraud.SignalNameNewAccount, Impact: 30, Note: "fresh"},
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("new assessment: %v", err)
	}
	if err := fraudRepo.Save(ctx, fa); err != nil {
		t.Fatalf("seed assessment: %v", err)
	}

	auditRepo := pg.NewAuditRepository(pool)
	prior, err := audit.New(uuid.New(), audit.ActionUserSuspend, audit.TargetUser, jane.ID,
		map[string]any{"reason": "test"}, "req-1")
	if err != nil {
		t.Fatalf("new audit event: %v", err)
	}
	if err := auditRepo.Create(ctx, prior); err != nil {
		t.Fatalf("seed audit: %v", err)
	}

	// --- Run each erase primitive in the same order the service uses ---

	if err := pushTokens.DeleteByUser(ctx, jane.ID); err != nil {
		t.Fatalf("erase push tokens: %v", err)
	}
	if n, err := savedSearches.EraseByUser(ctx, jane.ID); err != nil || n != 1 {
		t.Fatalf("erase saved searches: n=%d err=%v", n, err)
	}
	if n, err := identities.EraseByUser(ctx, jane.ID); err != nil || n != 1 {
		t.Fatalf("erase identities: n=%d err=%v", n, err)
	}
	if n, err := cohosts.EraseByUser(ctx, jane.ID); err != nil || n != 1 {
		t.Fatalf("erase cohosts: n=%d err=%v", n, err)
	}
	if n, err := templates.EraseByHost(ctx, jane.ID); err != nil || n != 1 {
		t.Fatalf("erase templates: n=%d err=%v", n, err)
	}
	if n, err := disputes.AnonymizeByUser(ctx, jane.ID); err != nil || n != 1 {
		t.Fatalf("anonymise disputes: n=%d err=%v", n, err)
	}
	if n, err := splitPayments.AnonymizeByUser(ctx, jane.ID, originalJaneEmail); err != nil || n != 1 {
		t.Fatalf("anonymise splits: n=%d err=%v", n, err)
	}
	if n, err := houseRules.AnonymizeAcceptancesByGuest(ctx, jane.ID); err != nil || n != 1 {
		t.Fatalf("anonymise acceptances: n=%d err=%v", n, err)
	}
	if n, err := fraudRepo.AnonymizeByGuest(ctx, jane.ID); err != nil || n != 1 {
		t.Fatalf("anonymise fraud: n=%d err=%v", n, err)
	}
	if n, err := auditRepo.AnonymizeBySubject(ctx, jane.ID); err != nil || n != 1 {
		t.Fatalf("anonymise audit: n=%d err=%v", n, err)
	}
	// Append the gdpr_erase row (the audit-log step that the privacy
	// service performs last; we mirror it here so the integration test
	// covers the full sequence including the survivor).
	ev, err := audit.New(audit.SystemActor, audit.ActionGDPRErase, audit.TargetUser, jane.ID,
		map[string]any{"tables_erased": []string{"all"}, "rows_affected": 10}, "")
	if err != nil {
		t.Fatalf("build gdpr_erase event: %v", err)
	}
	if err := auditRepo.Create(ctx, ev); err != nil {
		t.Fatalf("record gdpr_erase: %v", err)
	}

	// --- HARD-DELETE assertions ----------------------------------------

	if toks, _ := pushTokens.ListByUser(ctx, jane.ID); len(toks) != 0 {
		t.Errorf("push_tokens for jane = %d, want 0", len(toks))
	}
	if toks, _ := pushTokens.ListByUser(ctx, bob.ID); len(toks) != 1 {
		t.Errorf("push_tokens for bob = %d, want 1 (cross-user damage)", len(toks))
	}
	if rows, _ := savedSearches.ListByUser(ctx, jane.ID); len(rows) != 0 {
		t.Errorf("saved_searches for jane = %d, want 0", len(rows))
	}
	if _, err := identities.FindLatestByUser(ctx, jane.ID); err == nil {
		t.Errorf("identity_verifications: expected ErrNotFound, got a row")
	}
	if grants, _ := cohosts.ListByUser(ctx, jane.ID); len(grants) != 0 {
		t.Errorf("cohosts for jane = %d, want 0", len(grants))
	}
	if tmpls, _ := templates.ListByHost(ctx, jane.ID); len(tmpls) != 0 {
		t.Errorf("message_templates for jane = %d, want 0", len(tmpls))
	}

	// --- ANONYMISE assertions ------------------------------------------

	reloaded, err := disputes.FindByID(ctx, d.ID)
	if err != nil {
		t.Fatalf("reload dispute: %v", err)
	}
	if reloaded.Reason == "My phone is 555-1234 please refund" {
		t.Errorf("dispute reason was not redacted")
	}
	if len(reloaded.Evidence) != 1 {
		t.Fatalf("dispute evidence count = %d", len(reloaded.Evidence))
	}
	if reloaded.Evidence[0].Note == "see attached, my name is Jane" {
		t.Errorf("dispute evidence note was not redacted")
	}

	splitReloaded, err := splitPayments.FindByID(ctx, sp.ID)
	if err != nil {
		t.Fatalf("reload split: %v", err)
	}
	for _, sh := range splitReloaded.Shares {
		if sh.PayerEmail == originalJaneEmail {
			t.Errorf("split share for jane still carries her email")
		}
	}

	ackReloaded, err := houseRules.AcceptanceFor(ctx, bookingID)
	if err != nil {
		t.Fatalf("reload acceptance: %v", err)
	}
	if ackReloaded.GuestID != uuid.Nil {
		t.Errorf("acceptance guest_id = %v, want zero UUID", ackReloaded.GuestID)
	}

	faReloaded, err := fraudRepo.FindByBookingID(ctx, bookingID)
	if err != nil {
		t.Fatalf("reload fraud: %v", err)
	}
	if faReloaded.GuestID != uuid.Nil {
		t.Errorf("fraud guest_id = %v, want zero UUID", faReloaded.GuestID)
	}

	// Audit: prior event survives, but target_id scrubbed; gdpr_erase
	// event lives with non-zero target.
	page, err := auditRepo.List(ctx, audit.Filter{}, shared.NewPage(50, 0))
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	var priorAfter, eraseEvent *audit.Event
	for _, e := range page.Items {
		if e.ID == prior.ID {
			priorAfter = e
		}
		if e.Action == audit.ActionGDPRErase {
			eraseEvent = e
		}
	}
	if priorAfter == nil {
		t.Fatalf("prior audit event was deleted")
	}
	if priorAfter.TargetID != uuid.Nil {
		t.Errorf("prior audit target_id = %v, want zero UUID", priorAfter.TargetID)
	}
	if eraseEvent == nil {
		t.Fatalf("gdpr_erase audit row missing")
	}
	if eraseEvent.TargetID != jane.ID {
		t.Errorf("gdpr_erase target_id = %v, want %v", eraseEvent.TargetID, jane.ID)
	}
}

// withTestDBForPrivacy mirrors withTestDB from integration_test.go but
// truncates the additional tables this test seeds. Keeping the two
// helpers separate avoids forcing every other integration test to
// truncate tables it doesn't care about.
func withTestDBForPrivacy(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := withTestDB(t)
	truncate(t, pool,
		"push_tokens", "saved_searches", "identity_verifications",
		"cohosts", "message_templates", "audit_events",
		"dispute_evidence", "disputes",
		"split_payment_shares", "split_payments",
		"house_rule_acceptances", "fraud_assessments",
		"bookings", "properties", "users",
	)
	return pool
}

// seedBookingForUser inserts a minimal Booking aggregate for the user
// on the given property — needed so the dispute / split / acceptance /
// fraud rows have a valid booking_id FK.
func seedBookingForUser(t *testing.T, pool *pgxpool.Pool, propID, guestID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	dates, err := booking.NewDateRange(time.Now().AddDate(0, 0, 30), time.Now().AddDate(0, 0, 32))
	if err != nil {
		t.Fatalf("dates: %v", err)
	}
	price, _ := shared.NewMoney(10000, "EUR")
	zero, _ := shared.NewMoney(0, "EUR")
	b, err := booking.NewBooking(propID, guestID, dates, 2, price, zero, 0.10, booking.Discounts{})
	if err != nil {
		t.Fatalf("new booking: %v", err)
	}
	bookings := pg.NewBookingRepository(pool)
	if err := bookings.Create(ctx, b); err != nil {
		t.Fatalf("seed booking: %v", err)
	}
	return b.ID
}
