package port

import (
	"context"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/coupon"
	"github.com/airhost/backend/internal/domain/dispute"
	"github.com/airhost/backend/internal/domain/experiencebooking"
	"github.com/airhost/backend/internal/domain/identity"
	"github.com/airhost/backend/internal/domain/message"
	"github.com/airhost/backend/internal/domain/payment"
	"github.com/airhost/backend/internal/domain/review"
	"github.com/airhost/backend/internal/domain/splitpayment"
)

// Tx exposes the repositories that participate in a single atomic unit of work,
// plus the outbox. Writes made through these and the event(s) appended to the
// Outbox commit together, so a domain change and its published events are never
// out of step (no event is lost between the write committing and being
// recorded).
type Tx struct {
	Bookings      booking.Repository
	Messages      message.Repository
	Identity      identity.Repository
	SplitPayments splitpayment.Repository
	// ExperienceBookings, when wired by the UnitOfWork, lets the
	// ExperienceBooking service route Create/Confirm/Cancel/Complete writes
	// through the same atomic-commit-with-outbox path the property-booking
	// service uses (S87 — WF-GAP-020).
	ExperienceBookings experiencebooking.Repository
	// Disputes, when wired, lets the dispute service write Open / admin
	// decisions atomically with the outbox (S89 — WF-GAP-003/013).
	Disputes dispute.Repository
	// Coupons, when wired, lets the booking service record a promo-code
	// redemption inside the same transaction as the booking write — so a
	// crash between persisting the booking and incrementing uses_count
	// can no longer let the same code be redeemed past its cap (S101 —
	// WF-GAP-006).
	Coupons coupon.Repository
	// Payments, when wired by the UnitOfWork, lets the payment subscriber
	// route Create/Update writes through the same atomic-commit-with-outbox
	// path the booking service uses. Without it, a crash between the
	// payment row landing and the outbox event being appended would lose
	// the PaymentAuthorized / PaymentCaptured / PaymentRefunded event
	// (S123 — Payment UoW slice).
	Payments payment.Repository
	// Reviews, when wired by the UnitOfWork, lets the review service route
	// Create writes through the same atomic-commit-with-outbox path the
	// booking / messaging services already use. Without it, a crash
	// between the reviews INSERT and the ReviewSubmitted outbox append
	// would lose the event (S136 — Review UoW slice).
	Reviews review.Repository
	Outbox  event.OutboxStore
}

// UnitOfWork runs a function inside a transaction. On success the writes and the
// appended outbox events are committed atomically and the events are then
// dispatched; on error everything is rolled back.
type UnitOfWork interface {
	Run(ctx context.Context, fn func(tx Tx) error) error
}
