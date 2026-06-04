package privacyapp_test

import (
	"context"
	"testing"
	"time"

	privacyapp "github.com/airhost/backend/internal/application/privacy"
	"github.com/airhost/backend/internal/domain/audit"
	"github.com/airhost/backend/internal/domain/dispute"
	"github.com/airhost/backend/internal/domain/favorite"
	"github.com/airhost/backend/internal/domain/fraud"
	"github.com/airhost/backend/internal/domain/houserules"
	"github.com/airhost/backend/internal/domain/identity"
	"github.com/airhost/backend/internal/domain/messagetemplate"
	"github.com/airhost/backend/internal/domain/notification"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/pushtoken"
	"github.com/airhost/backend/internal/domain/review"
	"github.com/airhost/backend/internal/domain/savedsearch"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/splitpayment"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/google/uuid"
)

// privacyFixture bundles the privacy service together with handles to every
// repository it writes to, so individual tests can seed and assert without
// rebuilding the wiring each time.
type privacyFixture struct {
	svc              *privacyapp.Service
	users            *memory.UserRepository
	favorites        *memory.FavoriteRepository
	notifications    *memory.NotificationRepository
	reviews          *memory.ReviewRepository
	pushTokens       *memory.PushTokenRepository
	savedSearches    *memory.SavedSearchRepository
	identities       *memory.IdentityRepository
	disputes         *memory.DisputeRepository
	splitPayments    *memory.SplitPaymentRepository
	cohosts          *memory.CohostRepository
	messageTemplates *memory.MessageTemplateRepository
	houseRules       *memory.HouseRulesRepository
	fraudAssessments *memory.FraudRepository
	auditEvents      *memory.AuditRepository
}

func newPrivacyFixture(t *testing.T) *privacyFixture {
	t.Helper()
	f := &privacyFixture{
		users:            memory.NewUserRepository(),
		favorites:        memory.NewFavoriteRepository(),
		notifications:    memory.NewNotificationRepository(),
		reviews:          memory.NewReviewRepository(),
		pushTokens:       memory.NewPushTokenRepository(),
		savedSearches:    memory.NewSavedSearchRepository(),
		identities:       memory.NewIdentityRepository(),
		disputes:         memory.NewDisputeRepository(),
		splitPayments:    memory.NewSplitPaymentRepository(),
		cohosts:          memory.NewCohostRepository(),
		messageTemplates: memory.NewMessageTemplateRepository(),
		houseRules:       memory.NewHouseRulesRepository(),
		fraudAssessments: memory.NewFraudRepository(),
		auditEvents:      memory.NewAuditRepository(),
	}
	f.svc = privacyapp.NewService(
		f.users,
		memory.NewBookingRepository(),
		memory.NewPaymentRepository(),
		f.favorites,
		f.notifications,
		memory.NewPayoutRepository(),
		f.reviews,
		f.pushTokens,
		f.savedSearches,
		f.identities,
		f.disputes,
		f.splitPayments,
		f.cohosts,
		f.messageTemplates,
		f.houseRules,
		f.fraudAssessments,
		f.auditEvents,
	)
	return f
}

func TestExport_IncludesProfileAndFavorites(t *testing.T) {
	ctx := context.Background()
	f := newPrivacyFixture(t)

	u, err := user.NewUser("sub-1", "jane@test.dev", "Jane", user.RoleGuest)
	if err != nil {
		t.Fatalf("new user: %v", err)
	}
	if err := f.users.Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	prop := uuid.New()
	if err := f.favorites.Add(ctx, favorite.New(u.ID, prop, nil)); err != nil {
		t.Fatalf("add favorite: %v", err)
	}

	exp, err := f.svc.Export(ctx, u.ID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if exp.User.Email != "jane@test.dev" {
		t.Fatalf("export email = %q", exp.User.Email)
	}
	if len(exp.FavoriteIDs) != 1 || exp.FavoriteIDs[0] != prop {
		t.Fatalf("favorites = %v, want [%s]", exp.FavoriteIDs, prop)
	}
}

func TestErase_AnonymisesAndDropsFavorites(t *testing.T) {
	ctx := context.Background()
	f := newPrivacyFixture(t)

	u, err := user.NewUser("sub-1", "jane@test.dev", "Jane", user.RoleGuest)
	if err != nil {
		t.Fatalf("new user: %v", err)
	}
	if err := f.users.Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := f.favorites.Add(ctx, favorite.New(u.ID, uuid.New(), nil)); err != nil {
		t.Fatalf("add favorite: %v", err)
	}

	if err := f.svc.Erase(ctx, u.ID); err != nil {
		t.Fatalf("erase: %v", err)
	}

	reloaded, err := f.users.FindByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.Email == "jane@test.dev" || reloaded.FullName == "Jane" || reloaded.IsActive {
		t.Fatalf("user not anonymised: %+v", reloaded)
	}
	favs, err := f.favorites.ListPropertyIDs(ctx, u.ID, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("list favorites: %v", err)
	}
	if len(favs.Items) != 0 {
		t.Fatalf("favorites after erase = %d, want 0", len(favs.Items))
	}
}

func TestErase_DeletesNotificationsAndScrubsReviewComments(t *testing.T) {
	ctx := context.Background()
	f := newPrivacyFixture(t)

	u, err := user.NewUser("sub-1", "jane@test.dev", "Jane", user.RoleGuest)
	if err != nil {
		t.Fatalf("new user: %v", err)
	}
	if err := f.users.Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := f.notifications.Create(ctx, &notification.Notification{
		ID: uuid.New(), UserID: u.ID, Type: notification.TypeBookingConfirmed,
		Title: "t", Body: "b", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed notification: %v", err)
	}
	rv := &review.Review{
		ID: uuid.New(), BookingID: uuid.New(), PropertyID: uuid.New(),
		AuthorID: u.ID, GuestID: u.ID, Kind: review.KindGuestToProperty,
		Rating: 5, Comment: "Great place — reach me at jane@test.dev", CreatedAt: time.Now().UTC(),
	}
	if err := f.reviews.Create(ctx, rv); err != nil {
		t.Fatalf("seed review: %v", err)
	}

	if err := f.svc.Erase(ctx, u.ID); err != nil {
		t.Fatalf("erase: %v", err)
	}

	notes, err := f.notifications.ListByUser(ctx, u.ID, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	if len(notes.Items) != 0 {
		t.Fatalf("notifications after erase = %d, want 0", len(notes.Items))
	}
	// The review survives (rating intact) but its free-text is scrubbed.
	page, err := f.reviews.ListByProperty(ctx, rv.PropertyID, shared.NewPage(10, 0))
	if err != nil {
		t.Fatalf("list reviews: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Comment != "" || page.Items[0].Rating != 5 {
		t.Fatalf("review after erase = %+v, want comment scrubbed + rating kept", page.Items)
	}
}

// TestErase_CoversAllPIITables exercises the full S90 / WF-GAP-009 sweep —
// every PII-bearing table added since the privacy service was first wired.
// Hard-delete tables must be empty for the user after Erase; anonymise
// tables must retain the row but with the personal link redacted; and the
// audit log must carry a single gdpr_erase event with the expected table
// list.
func TestErase_CoversAllPIITables(t *testing.T) {
	ctx := context.Background()
	f := newPrivacyFixture(t)

	// The erased user, plus a "stays alive" counterparty so we can prove
	// the erase is targeted (no cross-user collateral damage).
	jane, err := user.NewUser("sub-jane", "jane@test.dev", "Jane", user.RoleGuest)
	if err != nil {
		t.Fatalf("new jane: %v", err)
	}
	if err := f.users.Create(ctx, jane); err != nil {
		t.Fatalf("create jane: %v", err)
	}
	bob, err := user.NewUser("sub-bob", "bob@test.dev", "Bob", user.RoleGuest)
	if err != nil {
		t.Fatalf("new bob: %v", err)
	}
	if err := f.users.Create(ctx, bob); err != nil {
		t.Fatalf("create bob: %v", err)
	}

	// --- Seed every table the pipeline must visit -----------------------

	// push_tokens
	pt, _ := pushtoken.New(jane.ID, pushtoken.PlatformAndroid, "device-token-jane")
	if err := f.pushTokens.Save(ctx, pt); err != nil {
		t.Fatalf("seed push token: %v", err)
	}
	ptBob, _ := pushtoken.New(bob.ID, pushtoken.PlatformAndroid, "device-token-bob")
	if err := f.pushTokens.Save(ctx, ptBob); err != nil {
		t.Fatalf("seed push token bob: %v", err)
	}

	// saved_searches
	if err := f.savedSearches.Create(ctx, &savedsearch.SavedSearch{
		ID: uuid.New(), UserID: jane.ID, Name: "Beach", Query: "city=Lisboa",
		AlertsEnabled: true, LastNotifiedAt: time.Now().UTC(), CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed saved search: %v", err)
	}

	// identity_verifications
	ver, err := identity.NewVerification(jane.ID, identity.DocPassport, "doc-ref-1", "Jane Doe")
	if err != nil {
		t.Fatalf("new verification: %v", err)
	}
	if err := f.identities.Create(ctx, ver); err != nil {
		t.Fatalf("seed verification: %v", err)
	}

	// disputes — jane is the opener, bob the counterparty.
	dispBooking := uuid.New()
	d, err := dispute.New(dispBooking, jane.ID, dispute.KindRefund,
		"My personal phone number is 555-1234 please refund", 100, "EUR")
	if err != nil {
		t.Fatalf("new dispute: %v", err)
	}
	if err := f.disputes.Save(ctx, d); err != nil {
		t.Fatalf("seed dispute: %v", err)
	}
	// Evidence added by jane — should be scrubbed.
	d2, _ := f.disputes.FindByID(ctx, d.ID)
	if _, err := d2.AddEvidence(jane.ID, "https://example.com/proof.png", "see attached, my name is Jane"); err != nil {
		t.Fatalf("add evidence: %v", err)
	}
	if err := f.disputes.Save(ctx, d2); err != nil {
		t.Fatalf("save dispute w/ evidence: %v", err)
	}

	// split_payment_shares — jane organises a split with bob as payer.
	sp, err := splitpayment.New(uuid.New(), jane.ID, "jane@test.dev", "EUR", 10000, []splitpayment.ShareInput{
		{Email: "jane@test.dev", AmountCents: 5000},
		{Email: "bob@test.dev", AmountCents: 5000},
	})
	if err != nil {
		t.Fatalf("new split: %v", err)
	}
	if err := f.splitPayments.Create(ctx, sp); err != nil {
		t.Fatalf("seed split: %v", err)
	}

	// cohosts — jane is a co-host on a listing.
	c, err := property.NewCohost(uuid.New(), jane.ID, []property.CohostPermission{property.PermManageCalendar})
	if err != nil {
		t.Fatalf("new cohost: %v", err)
	}
	if err := f.cohosts.Create(ctx, c); err != nil {
		t.Fatalf("seed cohost: %v", err)
	}

	// message_templates — jane has a host playbook entry.
	mt, err := messagetemplate.New(jane.ID, "Greeting", "Welcome to my place!")
	if err != nil {
		t.Fatalf("new template: %v", err)
	}
	if err := f.messageTemplates.Create(ctx, mt); err != nil {
		t.Fatalf("seed template: %v", err)
	}

	// houserules_acceptances — jane accepted a property's rules.
	ack, err := houserules.NewAcceptance(uuid.New(), jane.ID, uuid.New(), 1)
	if err != nil {
		t.Fatalf("new acceptance: %v", err)
	}
	ackBooking := ack.BookingID
	if err := f.houseRules.RecordAcceptance(ctx, ack); err != nil {
		t.Fatalf("seed acceptance: %v", err)
	}

	// fraud_assessments — jane's booking was scored.
	fa, err := fraud.New(uuid.New(), jane.ID, []fraud.Signal{
		{Name: fraud.SignalNameNewAccount, Impact: 30, Note: "fresh"},
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("new assessment: %v", err)
	}
	if err := f.fraudAssessments.Save(ctx, fa); err != nil {
		t.Fatalf("seed assessment: %v", err)
	}

	// audit_events — a prior admin action took jane as a target.
	prior, err := audit.New(uuid.New(), audit.ActionUserSuspend, audit.TargetUser, jane.ID,
		map[string]any{"reason": "test"}, "req-1")
	if err != nil {
		t.Fatalf("new audit event: %v", err)
	}
	if err := f.auditEvents.Create(ctx, prior); err != nil {
		t.Fatalf("seed audit: %v", err)
	}

	// --- Run the erase --------------------------------------------------

	if err := f.svc.Erase(ctx, jane.ID); err != nil {
		t.Fatalf("erase: %v", err)
	}

	// --- HARD-DELETE assertions ----------------------------------------

	if toks, _ := f.pushTokens.ListByUser(ctx, jane.ID); len(toks) != 0 {
		t.Errorf("push_tokens after erase = %d, want 0", len(toks))
	}
	// Counterparty token MUST survive — the erase is targeted.
	if toks, _ := f.pushTokens.ListByUser(ctx, bob.ID); len(toks) != 1 {
		t.Errorf("bob push_tokens after erase = %d, want 1 (cross-user damage)", len(toks))
	}
	if ss, _ := f.savedSearches.ListByUser(ctx, jane.ID); len(ss) != 0 {
		t.Errorf("saved_searches after erase = %d, want 0", len(ss))
	}
	if _, err := f.identities.FindLatestByUser(ctx, jane.ID); err == nil {
		t.Errorf("identity_verifications after erase: expected ErrNotFound, got a row")
	}
	if grants, _ := f.cohosts.ListByUser(ctx, jane.ID); len(grants) != 0 {
		t.Errorf("cohosts after erase = %d, want 0", len(grants))
	}
	if tmpls, _ := f.messageTemplates.ListByHost(ctx, jane.ID); len(tmpls) != 0 {
		t.Errorf("message_templates after erase = %d, want 0", len(tmpls))
	}

	// --- ANONYMISE assertions ------------------------------------------

	// Disputes — row stays, free text + jane's evidence note redacted.
	reloadedDispute, err := f.disputes.FindByID(ctx, d.ID)
	if err != nil {
		t.Fatalf("reload dispute: %v", err)
	}
	if reloadedDispute.Reason == "" || reloadedDispute.Reason == "My personal phone number is 555-1234 please refund" {
		t.Errorf("dispute reason after erase = %q, want redacted", reloadedDispute.Reason)
	}
	if len(reloadedDispute.Evidence) != 1 {
		t.Fatalf("dispute evidence count = %d, want 1", len(reloadedDispute.Evidence))
	}
	if reloadedDispute.Evidence[0].Note == "see attached, my name is Jane" || reloadedDispute.Evidence[0].URL != "" {
		t.Errorf("dispute evidence after erase = %+v, want redacted note + empty URL", reloadedDispute.Evidence[0])
	}

	// Split payment — row stays, jane's share email blanked.
	reloadedSplit, err := f.splitPayments.FindByID(ctx, sp.ID)
	if err != nil {
		t.Fatalf("reload split: %v", err)
	}
	if len(reloadedSplit.Shares) != 2 {
		t.Fatalf("split shares = %d, want 2", len(reloadedSplit.Shares))
	}
	janeShareFound := false
	bobShareFound := false
	for _, sh := range reloadedSplit.Shares {
		if sh.PayerEmail == "jane@test.dev" {
			t.Errorf("split share for jane after erase still carries email %q", sh.PayerEmail)
		}
		if sh.PayerEmail == "bob@test.dev" {
			bobShareFound = true
		}
		if sh.PayerEmail == "" {
			janeShareFound = true
		}
	}
	if !janeShareFound {
		t.Errorf("jane's split share email was not blanked")
	}
	if !bobShareFound {
		t.Errorf("bob's split share email was not preserved (cross-user damage)")
	}

	// House-rules acceptance — row stays, guest_id zeroed.
	reloadedAck, err := f.houseRules.AcceptanceFor(ctx, ackBooking)
	if err != nil {
		t.Fatalf("reload acceptance: %v", err)
	}
	if reloadedAck.GuestID != uuid.Nil {
		t.Errorf("houserules acceptance guest_id = %v, want zero UUID", reloadedAck.GuestID)
	}

	// Fraud — row stays, guest_id zeroed.
	reloadedFraud, err := f.fraudAssessments.FindByBookingID(ctx, fa.BookingID)
	if err != nil {
		t.Fatalf("reload fraud: %v", err)
	}
	if reloadedFraud.GuestID != uuid.Nil {
		t.Errorf("fraud assessment guest_id = %v, want zero UUID", reloadedFraud.GuestID)
	}

	// Audit — prior event survives but target_id zeroed; one gdpr_erase
	// event was appended for jane.
	auditList, err := f.auditEvents.List(ctx, audit.Filter{}, shared.NewPage(50, 0))
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	var priorAfter *audit.Event
	var eraseEvent *audit.Event
	for _, ev := range auditList.Items {
		if ev.ID == prior.ID {
			priorAfter = ev
		}
		if ev.Action == audit.ActionGDPRErase {
			eraseEvent = ev
		}
	}
	if priorAfter == nil {
		t.Fatalf("prior audit event was deleted, should survive")
	}
	if priorAfter.TargetID != uuid.Nil {
		t.Errorf("prior audit target_id after erase = %v, want zero UUID", priorAfter.TargetID)
	}
	if eraseEvent == nil {
		t.Fatalf("gdpr_erase audit event was not recorded")
	}
	if eraseEvent.TargetID != jane.ID {
		t.Errorf("gdpr_erase target_id = %v, want %v", eraseEvent.TargetID, jane.ID)
	}
	if eraseEvent.ActorID != audit.SystemActor {
		t.Errorf("gdpr_erase actor = %v, want SystemActor", eraseEvent.ActorID)
	}
	tablesAny, ok := eraseEvent.Metadata["tables_erased"]
	if !ok {
		t.Fatalf("gdpr_erase audit lacks tables_erased metadata: %+v", eraseEvent.Metadata)
	}
	tables, _ := tablesAny.([]string)
	wantTables := []string{
		"push_tokens", "saved_searches", "identity_verifications", "cohosts",
		"message_templates", "favorites", "notifications", "reviews_authored",
		"disputes", "split_payment_shares", "house_rule_acceptances",
		"fraud_assessments", "audit_events",
	}
	wantSet := map[string]bool{}
	for _, n := range wantTables {
		wantSet[n] = false
	}
	for _, n := range tables {
		if _, expected := wantSet[n]; expected {
			wantSet[n] = true
		}
	}
	for n, seen := range wantSet {
		if !seen {
			t.Errorf("gdpr_erase audit metadata missing table %q (got %v)", n, tables)
		}
	}

	// --- USER profile assertion ----------------------------------------

	reloaded, err := f.users.FindByID(ctx, jane.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.Email == "jane@test.dev" || reloaded.IsActive {
		t.Errorf("user profile not anonymised: %+v", reloaded)
	}
}
