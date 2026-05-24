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

// Erase fulfils a right-to-erasure request: it anonymises the profile,
// deactivates the account and deletes personal preference data (the wishlist).
// Retained records (bookings, payments, payouts) keep referencing the now
// de-identified account to honour legal/accounting obligations.
func (s *Service) Erase(ctx context.Context, userID uuid.UUID) error {
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if favs, err := s.favorites.ListPropertyIDs(ctx, userID, exportPage); err == nil {
		for _, pid := range favs.Items {
			_ = s.favorites.Remove(ctx, userID, pid)
		}
	}
	u.Anonymize()
	return s.users.Update(ctx, u)
}
