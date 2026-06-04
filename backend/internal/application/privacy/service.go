// Package privacyapp implements GDPR self-service: a data export (right of
// access / portability) that aggregates everything the platform holds for a
// user, and an erasure that anonymises the profile and drops preference data
// while retaining records the platform is legally required to keep.
package privacyapp

import (
	"context"

	"github.com/airhost/backend/internal/domain/audit"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/dispute"
	"github.com/airhost/backend/internal/domain/favorite"
	"github.com/airhost/backend/internal/domain/fraud"
	"github.com/airhost/backend/internal/domain/houserules"
	"github.com/airhost/backend/internal/domain/identity"
	"github.com/airhost/backend/internal/domain/messagetemplate"
	"github.com/airhost/backend/internal/domain/notification"
	"github.com/airhost/backend/internal/domain/payment"
	"github.com/airhost/backend/internal/domain/payout"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/pushtoken"
	"github.com/airhost/backend/internal/domain/review"
	"github.com/airhost/backend/internal/domain/savedsearch"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/splitpayment"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
)

// exportPage fetches a generous slice per collection; an individual's history
// in this app is modest.
var exportPage = shared.Page{Limit: 1000, Offset: 0}

// Service orchestrates privacy use cases.
//
// The erase pipeline (S90 / WF-GAP-009) is intentionally NOT wrapped in a
// single Unit of Work: it spans aggregates whose Postgres tables sit in
// different functional clusters (KYC, payments, fraud, audit) and the
// project's UoW only carries a small subset of the repository ports. Each
// erase step is independently idempotent — re-running a successful step
// is a no-op — so the pipeline retries from the first failure on resume
// rather than re-anonymising from scratch.
type Service struct {
	users            user.Repository
	bookings         booking.Repository
	payments         payment.Repository
	favorites        favorite.Repository
	notifications    notification.Repository
	payouts          payout.Repository
	reviews          review.Repository
	pushTokens       pushtoken.Repository
	savedSearches    savedsearch.Repository
	identities       identity.Repository
	disputes         dispute.Repository
	splitPayments    splitpayment.Repository
	cohosts          property.CohostRepository
	messageTemplates messagetemplate.Repository
	houseRules       houserules.Repository
	fraudAssessments fraud.Repository
	auditEvents      audit.Repository
}

// NewService wires the privacy application service over the read models it
// aggregates for an export and the writes it performs for an erasure.
//
// The constructor takes every repository that holds user-mapped data — adding
// a new PII-bearing table to the platform means adding a parameter here so
// the erasure pipeline cannot silently drift out of date with the schema.
func NewService(
	users user.Repository,
	bookings booking.Repository,
	payments payment.Repository,
	favorites favorite.Repository,
	notifications notification.Repository,
	payouts payout.Repository,
	reviews review.Repository,
	pushTokens pushtoken.Repository,
	savedSearches savedsearch.Repository,
	identities identity.Repository,
	disputes dispute.Repository,
	splitPayments splitpayment.Repository,
	cohosts property.CohostRepository,
	messageTemplates messagetemplate.Repository,
	houseRules houserules.Repository,
	fraudAssessments fraud.Repository,
	auditEvents audit.Repository,
) *Service {
	return &Service{
		users:            users,
		bookings:         bookings,
		payments:         payments,
		favorites:        favorites,
		notifications:    notifications,
		payouts:          payouts,
		reviews:          reviews,
		pushTokens:       pushTokens,
		savedSearches:    savedSearches,
		identities:       identities,
		disputes:         disputes,
		splitPayments:    splitPayments,
		cohosts:          cohosts,
		messageTemplates: messageTemplates,
		houseRules:       houseRules,
		fraudAssessments: fraudAssessments,
		auditEvents:      auditEvents,
	}
}

// Export is the full set of personal data AirHost holds for a user.
type Export struct {
	User           *user.User
	Bookings       []*booking.Booking
	Payments       []*payment.Payment
	FavoriteIDs    []uuid.UUID
	Notifications  []*notification.Notification
	PayoutEntries  []*payout.Entry
	ReviewsAboutMe []*review.Review
}

// Export aggregates every personal-data record tied to the user.
func (s *Service) Export(ctx context.Context, userID uuid.UUID) (*Export, error) {
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	exp := &Export{User: u}

	if bs, err := s.bookings.ListByGuest(ctx, userID, exportPage); err != nil {
		return nil, err
	} else {
		exp.Bookings = bs.Items
	}
	if ps, err := s.payments.ListByGuest(ctx, userID, exportPage); err != nil {
		return nil, err
	} else {
		exp.Payments = ps.Items
	}
	if fs, err := s.favorites.ListPropertyIDs(ctx, userID, exportPage); err != nil {
		return nil, err
	} else {
		exp.FavoriteIDs = fs.Items
	}
	if ns, err := s.notifications.ListByUser(ctx, userID, exportPage); err != nil {
		return nil, err
	} else {
		exp.Notifications = ns.Items
	}
	if es, err := s.payouts.ListByHost(ctx, userID, exportPage); err != nil {
		return nil, err
	} else {
		exp.PayoutEntries = es.Items
	}
	if rs, err := s.reviews.ListAboutGuest(ctx, userID, exportPage); err != nil {
		return nil, err
	} else {
		exp.ReviewsAboutMe = rs.Items
	}
	return exp, nil
}

// Erase fulfils a right-to-erasure request. The profile is anonymised FIRST so
// the operation is idempotent on retry and the account is immediately
// de-identified; then personal data with no retention basis is removed or
// scrubbed.
//
// Order matters. We sweep leaf tables (those with no downstream consumers)
// first and the audit log LAST, so:
//   - if any intermediate step fails, the user is already anonymised — a
//     resume picks up where it left off, against the same (already
//     anonymised) user row;
//   - the audit row records exactly which tables were touched and the row
//     counts — it is the ONE survivor of the erase, the platform's
//     compliance proof.
//
// Tables split into two strategies:
//   - HARD-DELETE: push_tokens, saved_searches, identity_documents,
//     identity_verifications, cohosts, message_templates, favorites,
//     notifications. Pure preference / device / permission data with no
//     retention basis once the user erases.
//   - ANONYMISE: users, reviews-authored, disputes, splits (shares),
//     house-rule acceptances, fraud assessments, audit log. The row is
//     legally or operationally required to survive (regulator, host /
//     counterparty interest, forensic evidence) — only the PII link to
//     this account is severed.
//
// Bookings, payments and payouts are retained (now referencing the de-
// identified account) to honour legal/accounting obligations. Message
// bodies are likewise retained: the other participant has a legitimate
// interest in the conversation, and the sender is already de-identified.
func (s *Service) Erase(ctx context.Context, userID uuid.UUID) error {
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	// Capture the pre-anonymise email so we can match against split-payment
	// share rows that key on the invite address rather than the user id.
	originalEmail := u.Email

	// Anonymise the profile + deactivate first; a retry after a later
	// failure is harmless because every downstream step is idempotent.
	u.Anonymize()
	if err := s.users.Update(ctx, u); err != nil {
		return err
	}

	// tablesErased collects the names of every table the pipeline swept
	// (regardless of whether any rows were present) so the audit row
	// records exactly what the erase considered. rowsAffected tallies the
	// total row writes for an at-a-glance compliance metric; ports that
	// don't surface a row count contribute 0.
	tablesErased := make([]string, 0, 16)
	rowsAffected := 0
	add := func(name string, n int) {
		tablesErased = append(tablesErased, name)
		if n > 0 {
			rowsAffected += n
		}
	}

	// Best-effort removal of the remaining personal data; failures here
	// must not leave the account un-anonymised (it already is). We capture
	// the first error and surface it AFTER attempting every step so a
	// single broken downstream cannot prevent the rest of the cleanup.
	var firstErr error
	keep := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// --- Leaf tables (no downstream consumers) ---------------------------

	// Push tokens — device identifiers, hard-delete.
	keep(s.pushTokens.DeleteByUser(ctx, userID))
	add("push_tokens", 0) // pushtoken.DeleteByUser predates this work and doesn't return a count

	// Saved searches — pure preference data, hard-delete.
	if n, err := s.savedSearches.EraseByUser(ctx, userID); err != nil {
		keep(err)
	} else {
		add("saved_searches", n)
	}

	// Identity documents / verifications — hard-delete (most sensitive PII).
	if n, err := s.identities.EraseByUser(ctx, userID); err != nil {
		keep(err)
	} else {
		add("identity_verifications", n)
	}

	// Co-host grants — hard-delete (per-listing permission set, no retention).
	if n, err := s.cohosts.EraseByUser(ctx, userID); err != nil {
		keep(err)
	} else {
		add("cohosts", n)
	}

	// Message templates — hard-delete (host playbook, no retention).
	if n, err := s.messageTemplates.EraseByHost(ctx, userID); err != nil {
		keep(err)
	} else {
		add("message_templates", n)
	}

	// Favorites (wishlists) — hard-delete via the per-property API.
	if favs, err := s.favorites.ListPropertyIDs(ctx, userID, exportPage); err == nil {
		for _, pid := range favs.Items {
			keep(s.favorites.Remove(ctx, userID, pid))
		}
		add("favorites", len(favs.Items))
	} else {
		keep(err)
	}

	// Notifications — hard-delete.
	keep(s.notifications.DeleteByUser(ctx, userID))
	add("notifications", 0) // notification.DeleteByUser predates this work and doesn't return a count

	// --- Anonymise (rows retained, links severed) ------------------------

	// Reviews — anonymise the free-text body; rating + row are kept so
	// listing/guest aggregates stay intact.
	keep(s.reviews.AnonymizeByAuthor(ctx, userID))
	add("reviews_authored", 0) // review.AnonymizeByAuthor predates this work and doesn't return a count

	// Disputes — keep the row (legal artefact, counterparty interest); blank
	// the user's free-text reason / response / evidence notes.
	if n, err := s.disputes.AnonymizeByUser(ctx, userID); err != nil {
		keep(err)
	} else {
		add("disputes", n)
	}

	// Split-payment shares — keep the split row (multi-payer record); blank
	// the email + payer-id on shares this user was invited to pay.
	if n, err := s.splitPayments.AnonymizeByUser(ctx, userID, originalEmail); err != nil {
		keep(err)
	} else {
		add("split_payment_shares", n)
	}

	// House-rule acceptances — keep the row (legal proof joined to the
	// booking); zero the guest_id link to this account.
	if n, err := s.houseRules.AnonymizeAcceptancesByGuest(ctx, userID); err != nil {
		keep(err)
	} else {
		add("house_rule_acceptances", n)
	}

	// Fraud assessments — keep the row (forensic record per booking); zero
	// the guest_id link to this account.
	if n, err := s.fraudAssessments.AnonymizeByGuest(ctx, userID); err != nil {
		keep(err)
	} else {
		add("fraud_assessments", n)
	}

	// Audit trail — keep every row (compliance evidence); zero links to
	// this user as actor or as user-target. MUST run before recording the
	// erase event below, so the AnonymizeBySubject pass cannot retroactively
	// clobber the gdpr_erase row we are about to write.
	if n, err := s.auditEvents.AnonymizeBySubject(ctx, userID); err != nil {
		keep(err)
	} else {
		add("audit_events", n)
	}

	// --- Compliance record (the ONE survivor) ----------------------------
	//
	// Append a gdpr_erase audit event LAST. This row is the proof of the
	// erase: it carries the table list + a status flag for partial success
	// so an auditor can reconstruct what happened even if a downstream
	// step errored.
	status := "ok"
	if firstErr != nil {
		status = "partial: " + firstErr.Error()
	}
	ev, evErr := audit.New(
		audit.SystemActor,
		audit.ActionGDPRErase,
		audit.TargetUser,
		userID,
		map[string]any{
			"tables_erased": tablesErased,
			"rows_affected": rowsAffected,
			"status":        status,
		},
		"",
	)
	if evErr == nil {
		if err := s.auditEvents.Create(ctx, ev); err != nil && firstErr == nil {
			firstErr = err
		}
	} else if firstErr == nil {
		firstErr = evErr
	}
	return firstErr
}
