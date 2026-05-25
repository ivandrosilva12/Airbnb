// Package privacyapp implements GDPR self-service: a data export (right of
// access / portability) that aggregates everything the platform holds for a
// user, and an erasure that anonymises the profile and drops preference data
// while retaining records the platform is legally required to keep.
package privacyapp

import (
	"context"

	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/favorite"
	"github.com/airhost/backend/internal/domain/notification"
	"github.com/airhost/backend/internal/domain/payment"
	"github.com/airhost/backend/internal/domain/payout"
	"github.com/airhost/backend/internal/domain/review"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
)

// exportPage fetches a generous slice per collection; an individual's history
// in this app is modest.
var exportPage = shared.Page{Limit: 1000, Offset: 0}

// Service orchestrates privacy use cases.
type Service struct {
	users         user.Repository
	bookings      booking.Repository
	payments      payment.Repository
	favorites     favorite.Repository
	notifications notification.Repository
	payouts       payout.Repository
	reviews       review.Repository
}

// NewService wires the privacy application service over the read models it
// aggregates for an export and the writes it performs for an erasure.
func NewService(
	users user.Repository,
	bookings booking.Repository,
	payments payment.Repository,
	favorites favorite.Repository,
	notifications notification.Repository,
	payouts payout.Repository,
	reviews review.Repository,
) *Service {
	return &Service{
		users: users, bookings: bookings, payments: payments, favorites: favorites,
		notifications: notifications, payouts: payouts, reviews: reviews,
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
// scrubbed: the wishlist and notifications are deleted, and the free-text of any
// reviews the user authored is blanked (the rating and the review's existence
// are kept so listing/guest aggregates stay intact).
//
// Bookings, payments and payouts are retained (now referencing the de-identified
// account) to honour legal/accounting obligations. Message bodies are likewise
// retained: the other participant has a legitimate interest in the conversation,
// and the sender is already de-identified.
func (s *Service) Erase(ctx context.Context, userID uuid.UUID) error {
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return err
	}

	// Anonymise + deactivate first; a retry after a later failure is harmless.
	u.Anonymize()
	if err := s.users.Update(ctx, u); err != nil {
		return err
	}

	// Best-effort removal of the remaining personal data; failures here must not
	// leave the account un-anonymised (it already is), so they are logged-and-skipped
	// by returning the first error only after attempting all.
	var firstErr error
	if favs, err := s.favorites.ListPropertyIDs(ctx, userID, exportPage); err == nil {
		for _, pid := range favs.Items {
			if e := s.favorites.Remove(ctx, userID, pid); e != nil && firstErr == nil {
				firstErr = e
			}
		}
	} else if firstErr == nil {
		firstErr = err
	}
	if err := s.notifications.DeleteByUser(ctx, userID); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := s.reviews.AnonymizeByAuthor(ctx, userID); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}
