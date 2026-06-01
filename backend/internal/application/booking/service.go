// Package bookingapp contains reservation use cases. It coordinates the
// property and booking aggregates to enforce cross-aggregate rules such as
// no double-booking and host/guest authorization.
package bookingapp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/airhost/backend/internal/application/event"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/block"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/coupon"
	"github.com/airhost/backend/internal/domain/pricerule"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// IdentityVerifier reports whether a user has a verified identity. The booking
// service consults it when KYC gating is enabled.
type IdentityVerifier interface {
	IsVerified(ctx context.Context, userID uuid.UUID) (bool, error)
}

// Service orchestrates booking use cases.
type Service struct {
	bookings       booking.Repository
	properties     property.Repository
	blocks         block.Repository
	coupons        coupon.Repository
	priceRules     pricerule.Repository
	serviceFeeRate float64
	identity       IdentityVerifier
	requireKYC     bool
	// highValueThresholds, when populated, triggers a KYC step-up: a
	// booking whose total reaches the per-currency threshold requires a
	// verified guest even when requireKYC is off. A currency without an
	// entry has no step-up. See IdentityConfig.HighValueThresholdsCents.
	highValueThresholds map[string]int64
	uow                 port.UnitOfWork
}

// NewService wires the booking application service. serviceFeeRate is the
// platform fee applied to each booking (e.g. 0.12 for 12%). When requireKYC is
// true, a guest must have a verified identity (per the IdentityVerifier) before
// booking. The UnitOfWork makes the booking write and its domain event commit
// atomically. priceRules provides the listing's seasonal/per-date overrides
// used when summing the night-by-night subtotal.
func NewService(bookings booking.Repository, properties property.Repository, blocks block.Repository, coupons coupon.Repository, priceRules pricerule.Repository, serviceFeeRate float64, identity IdentityVerifier, requireKYC bool, uow port.UnitOfWork) *Service {
	return &Service{bookings: bookings, properties: properties, blocks: blocks, coupons: coupons, priceRules: priceRules, serviceFeeRate: serviceFeeRate, identity: identity, requireKYC: requireKYC, uow: uow}
}

// WithHighValueThresholds configures the per-currency step-up table. Returns
// the receiver for chaining. A nil or empty map disables the high-value gate
// (only the global RequireKYCToBook flag still applies).
func (s *Service) WithHighValueThresholds(thresholds map[string]int64) *Service {
	if len(thresholds) == 0 {
		s.highValueThresholds = nil
		return s
	}
	// Defensive copy so config mutation after wiring can't leak into the
	// running service.
	cp := make(map[string]int64, len(thresholds))
	for k, v := range thresholds {
		cp[k] = v
	}
	s.highValueThresholds = cp
	return s
}

// enforceStepUp is a no-op unless the booking's total reaches the configured
// per-currency high-value threshold AND the guest is not yet verified. The
// returned error carries the threshold + currency so the transport layer can
// surface a verify-and-retry prompt with the right context.
func (s *Service) enforceStepUp(ctx context.Context, guestID uuid.UUID, totalCents int64, currency string) error {
	if len(s.highValueThresholds) == 0 {
		return nil
	}
	threshold, ok := s.highValueThresholds[currency]
	if !ok || threshold <= 0 || totalCents < threshold {
		return nil
	}
	verified, err := s.identity.IsVerified(ctx, guestID)
	if err != nil {
		return err
	}
	if verified {
		return nil
	}
	return shared.NewKYCStepUpError(threshold, currency)
}

// emit runs the booking write and records the event(s) in one transaction, so
// they commit together (or roll back together). When several events are passed
// they are appended in order; the relay dispatches them in that order after the
// commit (e.g. BookingRequested before BookingConfirmed for an instant book).
func (s *Service) emit(ctx context.Context, write func(tx port.Tx) error, evs ...event.Event) error {
	return s.uow.Run(ctx, func(tx port.Tx) error {
		if err := write(tx); err != nil {
			return err
		}
		for _, ev := range evs {
			rec, err := event.NewRecord(ev)
			if err != nil {
				return err
			}
			if err := tx.Outbox.Append(ctx, rec); err != nil {
				return err
			}
		}
		return nil
	})
}

// CreateInput carries the data required to make a reservation.
type CreateInput struct {
	GuestID    uuid.UUID
	PropertyID uuid.UUID
	CheckIn    time.Time
	CheckOut   time.Time
	Guests     int
	CouponCode string // optional promo code
	// OverridePricePerNightCents, when > 0, replaces the listing's nightly price
	// (used when a guest accepts a host's special offer). 0 uses the listing price.
	OverridePricePerNightCents int64
}

// Create makes a reservation, enforcing availability and capacity rules.
func (s *Service) Create(ctx context.Context, in CreateInput) (*booking.Booking, error) {
	prop, err := s.properties.FindByID(ctx, in.PropertyID)
	if err != nil {
		return nil, err
	}
	if err := prop.CanBeBookedBy(in.GuestID); err != nil {
		return nil, err
	}
	if s.requireKYC {
		verified, err := s.identity.IsVerified(ctx, in.GuestID)
		if err != nil {
			return nil, err
		}
		if !verified {
			return nil, shared.NewValidationError("verify your identity before booking")
		}
	}
	if in.Guests > prop.MaxGuests {
		return nil, shared.NewValidationError("number of guests exceeds property capacity")
	}

	dates, err := booking.NewDateRange(in.CheckIn, in.CheckOut)
	if err != nil {
		return nil, err
	}

	overlap, err := s.bookings.HasOverlap(ctx, in.PropertyID, dates)
	if err != nil {
		return nil, err
	}
	if overlap {
		return nil, shared.NewValidationError("selected dates are not available")
	}

	blocked, err := s.blocks.HasOverlap(ctx, in.PropertyID, dates)
	if err != nil {
		return nil, err
	}
	if blocked {
		return nil, shared.NewValidationError("selected dates are blocked by the host")
	}

	// Sum the accommodation subtotal night-by-night: seasonal rule (if any) →
	// weekend (Fri/Sat) override → listing base price. A host special offer
	// (S9) overrides every night uniformly when present.
	subtotal, err := s.nightlySubtotal(ctx, prop, dates, in.OverridePricePerNightCents)
	if err != nil {
		return nil, err
	}

	// Resolve an optional promo code into an absolute discount before pricing.
	// An invalid/expired/inapplicable code fails the booking so the guest learns
	// why rather than being silently charged full price.
	couponCents := int64(0)
	var appliedCoupon *coupon.Coupon
	if code := coupon.NormalizeCode(in.CouponCode); code != "" {
		cp, err := s.couponByCode(ctx, code)
		if err != nil {
			return nil, err
		}
		couponCents, err = cp.DiscountFor(subtotal.AmountCents(), subtotal.Currency(), dates.Nights())
		if err != nil {
			return nil, err
		}
		appliedCoupon = cp
	}

	// Enforce the listing's stay-length rules.
	nights := dates.Nights()
	if prop.MinNights > 0 && nights < prop.MinNights {
		return nil, shared.NewValidationError(fmt.Sprintf("this listing requires a stay of at least %d nights", prop.MinNights))
	}
	if prop.MaxNights > 0 && nights > prop.MaxNights {
		return nil, shared.NewValidationError(fmt.Sprintf("this listing allows a stay of at most %d nights", prop.MaxNights))
	}
	// Per-extra-guest fee, charged per night beyond the included headcount.
	// A listing that never set GuestsIncluded (0) is treated as including its
	// full capacity, matching what the client shows — so an extra-guest fee is
	// only charged once a host explicitly sets a smaller included headcount.
	includedGuests := prop.GuestsIncluded
	if includedGuests <= 0 {
		includedGuests = prop.MaxGuests
	}
	extraGuests := in.Guests - includedGuests
	if extraGuests < 0 {
		extraGuests = 0
	}
	extraGuestFeeCents := int64(extraGuests) * prop.ExtraGuestFee.AmountCents() * int64(nights)

	b, err := booking.NewBookingFromSubtotal(in.PropertyID, in.GuestID, dates, in.Guests, subtotal, prop.CleaningFee, s.serviceFeeRate, booking.Discounts{
		WeeklyPct:            prop.PricingPolicy.WeeklyDiscountPct,
		MonthlyPct:           prop.PricingPolicy.MonthlyDiscountPct,
		TaxPct:               prop.PricingPolicy.TaxRatePct,
		CouponCents:          couponCents,
		ExtraGuestFeeCents:   extraGuestFeeCents,
		SecurityDepositCents: prop.SecurityDeposit.AmountCents(),
	})
	if err != nil {
		return nil, err
	}
	// High-value step-up: a configurable per-currency threshold demands a
	// verified identity even when the platform-wide RequireKYCToBook flag is
	// off. We check AFTER pricing because the total includes the cleaning
	// fee, service fee and any tax — the threshold is the all-in amount
	// the guest will actually be charged, not the nightly subtotal. The
	// global RequireKYCToBook check above already returns ErrValidation for
	// the unconditional case; if it passed (or wasn't set), we only need
	// the step-up gate here for high-value bookings.
	if err := s.enforceStepUp(ctx, in.GuestID, b.Pricing.Total.AmountCents(), b.Pricing.Total.Currency()); err != nil {
		return nil, err
	}
	// Instant book: auto-confirm the reservation now instead of holding it for
	// the host. Both events are published in the same transaction; the relay
	// dispatches BookingRequested (payment authorize) before BookingConfirmed
	// (capture + host-earning credit + guest notification).
	events := []event.Event{
		event.BookingRequested{
			BookingID:     b.ID,
			PropertyID:    prop.ID,
			PropertyTitle: prop.Title,
			HostID:        prop.HostID,
			GuestID:       in.GuestID,
			TotalCents:    b.Pricing.Total.AmountCents(),
			Currency:      b.Pricing.Total.Currency(),
			Instant:       prop.InstantBook,
		},
	}
	if prop.InstantBook {
		if err := b.Confirm(); err != nil {
			return nil, err
		}
		events = append(events, event.BookingConfirmed{
			BookingID:     b.ID,
			PropertyID:    prop.ID,
			PropertyTitle: prop.Title,
			GuestID:       in.GuestID,
		})
	}
	if err := s.emit(ctx,
		func(tx port.Tx) error { return tx.Bookings.Create(ctx, b) },
		events...,
	); err != nil {
		return nil, err
	}
	// Record the redemption once the booking is safely persisted. Best-effort: a
	// failure here must not undo a confirmed reservation. (Under heavy concurrency
	// this can slightly overshoot MaxRedemptions; acceptable for a promo code.)
	if appliedCoupon != nil {
		if err := appliedCoupon.Redeem(); err == nil {
			_ = s.coupons.Update(ctx, appliedCoupon)
		}
	}
	return b, nil
}

// ModifyInput carries a request to change a pending booking's dates and/or
// guest count.
type ModifyInput struct {
	ActorID   uuid.UUID
	BookingID uuid.UUID
	CheckIn   time.Time
	CheckOut  time.Time
	Guests    int
}

// Modify changes the dates and/or guest count of a pending booking. It
// re-validates availability and the listing's stay rules and re-prices the stay
// at the listing's current rates. Only the booking's guest may modify it, and
// only while it is still pending — a confirmed booking's payment has already been
// captured. A BookingModified event lets the payment context adjust the
// outstanding authorization hold to the new total.
//
// Any promo code applied at creation is not carried over (see Booking.Reschedule).
func (s *Service) Modify(ctx context.Context, in ModifyInput) (*booking.Booking, error) {
	b, prop, err := s.bookingWithProperty(ctx, in.BookingID)
	if err != nil {
		return nil, err
	}
	if b.GuestID != in.ActorID {
		return nil, shared.ErrForbidden
	}
	if b.Status != booking.StatusPending {
		return nil, shared.NewValidationError("only a pending booking can be modified")
	}
	if in.Guests > prop.MaxGuests {
		return nil, shared.NewValidationError("number of guests exceeds property capacity")
	}

	dates, err := booking.NewDateRange(in.CheckIn, in.CheckOut)
	if err != nil {
		return nil, err
	}

	// Availability: the new range must be free, ignoring this booking's own
	// current dates (which it is about to vacate).
	occupied, err := s.bookings.ListActiveInRange(ctx, prop.ID, dates.CheckIn, dates.CheckOut)
	if err != nil {
		return nil, err
	}
	for _, other := range occupied {
		if other.ID != b.ID {
			return nil, shared.NewValidationError("selected dates are not available")
		}
	}
	blocked, err := s.blocks.HasOverlap(ctx, prop.ID, dates)
	if err != nil {
		return nil, err
	}
	if blocked {
		return nil, shared.NewValidationError("selected dates are blocked by the host")
	}

	nights := dates.Nights()
	if prop.MinNights > 0 && nights < prop.MinNights {
		return nil, shared.NewValidationError(fmt.Sprintf("this listing requires a stay of at least %d nights", prop.MinNights))
	}
	if prop.MaxNights > 0 && nights > prop.MaxNights {
		return nil, shared.NewValidationError(fmt.Sprintf("this listing allows a stay of at most %d nights", prop.MaxNights))
	}

	includedGuests := prop.GuestsIncluded
	if includedGuests <= 0 {
		includedGuests = prop.MaxGuests
	}
	extraGuests := in.Guests - includedGuests
	if extraGuests < 0 {
		extraGuests = 0
	}
	extraGuestFeeCents := int64(extraGuests) * prop.ExtraGuestFee.AmountCents() * int64(nights)

	// Re-price at the listing's current rates, honouring seasonal/weekend rules.
	// A guest-initiated modification does not carry the original special-offer
	// override (the host's pre-approval was for the original dates).
	subtotal, err := s.nightlySubtotal(ctx, prop, dates, 0)
	if err != nil {
		return nil, err
	}
	if err := b.RescheduleFromSubtotal(dates, in.Guests, subtotal, prop.CleaningFee, s.serviceFeeRate, booking.Discounts{
		WeeklyPct:            prop.PricingPolicy.WeeklyDiscountPct,
		MonthlyPct:           prop.PricingPolicy.MonthlyDiscountPct,
		TaxPct:               prop.PricingPolicy.TaxRatePct,
		ExtraGuestFeeCents:   extraGuestFeeCents,
		SecurityDepositCents: prop.SecurityDeposit.AmountCents(),
	}); err != nil {
		return nil, err
	}

	if err := s.emit(ctx,
		func(tx port.Tx) error { return tx.Bookings.Update(ctx, b) },
		event.BookingModified{
			BookingID:     b.ID,
			PropertyID:    prop.ID,
			PropertyTitle: prop.Title,
			HostID:        prop.HostID,
			GuestID:       b.GuestID,
			TotalCents:    b.Pricing.Total.AmountCents(),
			Currency:      b.Pricing.Total.Currency(),
		},
	); err != nil {
		return nil, err
	}
	return b, nil
}

// couponByCode loads a coupon, mapping a missing code to a friendly validation
// error rather than a bare not-found.
func (s *Service) couponByCode(ctx context.Context, code string) (*coupon.Coupon, error) {
	cp, err := s.coupons.FindByCode(ctx, code)
	if err != nil {
		if errors.Is(err, shared.ErrNotFound) {
			return nil, shared.NewValidationError("invalid coupon code")
		}
		return nil, err
	}
	return cp, nil
}

// CouponPreview is the read-model behind "apply code" in the booking UI: the
// discount a code would yield for a given property and date range.
type CouponPreview struct {
	Code          string
	DiscountCents int64
	Currency      string
}

// PreviewCoupon computes (without redeeming) the discount a code yields for a
// property and stay length, so the UI can show it before the guest books.
func (s *Service) PreviewCoupon(ctx context.Context, propertyID uuid.UUID, code string, checkIn, checkOut time.Time) (CouponPreview, error) {
	prop, err := s.properties.FindByID(ctx, propertyID)
	if err != nil {
		return CouponPreview{}, err
	}
	dates, err := booking.NewDateRange(checkIn, checkOut)
	if err != nil {
		return CouponPreview{}, err
	}
	cp, err := s.couponByCode(ctx, coupon.NormalizeCode(code))
	if err != nil {
		return CouponPreview{}, err
	}
	subtotal, err := s.nightlySubtotal(ctx, prop, dates, 0)
	if err != nil {
		return CouponPreview{}, err
	}
	cents, err := cp.DiscountFor(subtotal.AmountCents(), subtotal.Currency(), dates.Nights())
	if err != nil {
		return CouponPreview{}, err
	}
	return CouponPreview{Code: cp.Code, DiscountCents: cents, Currency: subtotal.Currency()}, nil
}

// nightlySubtotal sums the per-night accommodation cost over the stay applying
// the priority chain: special-offer override (uniform) → seasonal rule on the
// day → weekend override (Fri/Sat, when set) → listing base nightly price. The
// returned Money uses the listing's currency.
func (s *Service) nightlySubtotal(ctx context.Context, prop *property.Property, dates booking.DateRange, overridePerNightCents int64) (shared.Money, error) {
	currency := prop.PricePerNight.Currency()
	nights := int64(dates.Nights())

	// A host special offer flattens the per-night price uniformly across the
	// stay — seasonal/weekend rules are not layered on top.
	if overridePerNightCents > 0 {
		return shared.NewMoney(overridePerNightCents*nights, currency)
	}

	rules, err := s.priceRules.ListOverlapping(ctx, prop.ID, dates.CheckIn, dates.CheckOut)
	if err != nil {
		return shared.Money{}, err
	}
	basePerNight := prop.PricePerNight.AmountCents()
	weekendPerNight := prop.PricingPolicy.WeekendPriceCents

	var totalCents int64
	for day := dates.CheckIn; day.Before(dates.CheckOut); day = day.AddDate(0, 0, 1) {
		totalCents += priceForDay(day, rules, basePerNight, weekendPerNight)
	}
	return shared.NewMoney(totalCents, currency)
}

// priceForDay picks the effective nightly price for a single day:
// matching seasonal rule first, then weekend override on Fri/Sat, then base.
func priceForDay(day time.Time, rules []*pricerule.Rule, baseCents, weekendCents int64) int64 {
	for _, r := range rules {
		if r.AppliesOn(day) {
			return r.PriceCents
		}
	}
	if weekendCents > 0 {
		switch day.Weekday() {
		case time.Friday, time.Saturday:
			return weekendCents
		}
	}
	return baseCents
}

// GetByID fetches a reservation, ensuring the actor is a participant.
func (s *Service) GetByID(ctx context.Context, actorID, bookingID uuid.UUID) (*booking.Booking, error) {
	b, err := s.bookings.FindByID(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	if b.GuestID != actorID {
		// Hosts may also view bookings for their property.
		prop, err := s.properties.FindByID(ctx, b.PropertyID)
		if err != nil {
			return nil, err
		}
		if !prop.IsOwnedBy(actorID) {
			return nil, shared.ErrForbidden
		}
	}
	return b, nil
}

// ArrivalForBooking returns the property's arrival/wifi info for a booking
// the actor owns, gated by the reveal window (≤ 48h before check-in through
// check-out). Returns shared.ErrForbidden outside the window, ErrNotFound
// when the listing has no arrival info configured. Cancelled bookings never
// see arrival info — there's no stay to arrive for. Hosts viewing their own
// listing already see the data through the property edit endpoint.
func (s *Service) ArrivalForBooking(ctx context.Context, actorID, bookingID uuid.UUID, now time.Time) (property.ArrivalInfo, error) {
	b, err := s.bookings.FindByID(ctx, bookingID)
	if err != nil {
		return property.ArrivalInfo{}, err
	}
	if b.GuestID != actorID {
		return property.ArrivalInfo{}, shared.ErrForbidden
	}
	if b.Status == booking.StatusCancelled {
		return property.ArrivalInfo{}, shared.ErrForbidden
	}
	if !property.ArrivalVisibleAt(now, b.Dates.CheckIn, b.Dates.CheckOut) {
		return property.ArrivalInfo{}, shared.ErrForbidden
	}
	prop, err := s.properties.FindByID(ctx, b.PropertyID)
	if err != nil {
		return property.ArrivalInfo{}, err
	}
	if !prop.Arrival.IsConfigured() {
		return property.ArrivalInfo{}, shared.ErrNotFound
	}
	return prop.Arrival, nil
}

// ListForGuest returns a guest's reservations.
func (s *Service) ListForGuest(ctx context.Context, guestID uuid.UUID, page shared.Page) (shared.PageResult[*booking.Booking], error) {
	return s.bookings.ListByGuest(ctx, guestID, page)
}

// ListForProperty returns reservations for a property the actor hosts.
func (s *Service) ListForProperty(ctx context.Context, actorID, propertyID uuid.UUID, page shared.Page) (shared.PageResult[*booking.Booking], error) {
	prop, err := s.properties.FindByID(ctx, propertyID)
	if err != nil {
		return shared.PageResult[*booking.Booking]{}, err
	}
	if !prop.IsOwnedBy(actorID) {
		return shared.PageResult[*booking.Booking]{}, shared.ErrForbidden
	}
	return s.bookings.ListByProperty(ctx, propertyID, page)
}

// Confirm confirms a pending booking; only the host may confirm.
func (s *Service) Confirm(ctx context.Context, actorID, bookingID uuid.UUID) (*booking.Booking, error) {
	b, prop, err := s.bookingWithProperty(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	if !prop.IsOwnedBy(actorID) {
		return nil, shared.ErrForbidden
	}
	if err := b.Confirm(); err != nil {
		return nil, err
	}
	if err := s.emit(ctx,
		func(tx port.Tx) error { return tx.Bookings.Update(ctx, b) },
		event.BookingConfirmed{
			BookingID:     b.ID,
			PropertyID:    prop.ID,
			PropertyTitle: prop.Title,
			GuestID:       b.GuestID,
		},
	); err != nil {
		return nil, err
	}
	return b, nil
}

// Cancel cancels a booking; the guest or the host may cancel.
func (s *Service) Cancel(ctx context.Context, actorID, bookingID uuid.UUID) (*booking.Booking, error) {
	b, prop, err := s.bookingWithProperty(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	if b.GuestID != actorID && !prop.IsOwnedBy(actorID) {
		return nil, shared.ErrForbidden
	}
	if err := b.Cancel(); err != nil {
		return nil, err
	}

	// A host cancellation is the host's fault and always refunds in full;
	// otherwise the listing's cancellation policy decides the refund fraction.
	refundFraction := 1.0
	if !prop.IsOwnedBy(actorID) {
		today := time.Now().UTC().Truncate(24 * time.Hour)
		daysUntilCheckIn := int(b.Dates.CheckIn.Sub(today).Hours() / 24)
		refundFraction = prop.CancellationPolicy.RefundFraction(daysUntilCheckIn)
	}

	if err := s.emit(ctx,
		func(tx port.Tx) error { return tx.Bookings.Update(ctx, b) },
		event.BookingCancelled{
			BookingID:      b.ID,
			PropertyID:     prop.ID,
			PropertyTitle:  prop.Title,
			HostID:         prop.HostID,
			GuestID:        b.GuestID,
			CancelledBy:    actorID,
			RefundFraction: refundFraction,
		},
	); err != nil {
		return nil, err
	}
	return b, nil
}

// Complete marks a confirmed booking as completed once the stay is over. Only
// the host may complete it, and only after the check-out date has passed —
// after which the guest becomes eligible to leave a review.
func (s *Service) Complete(ctx context.Context, actorID, bookingID uuid.UUID) (*booking.Booking, error) {
	b, prop, err := s.bookingWithProperty(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	if !prop.IsOwnedBy(actorID) {
		return nil, shared.ErrForbidden
	}
	if time.Now().UTC().Before(b.Dates.CheckOut) {
		return nil, shared.NewValidationError("a stay can only be completed after check-out")
	}
	if err := b.Complete(); err != nil {
		return nil, err
	}
	if err := s.emit(ctx,
		func(tx port.Tx) error { return tx.Bookings.Update(ctx, b) },
		event.BookingCompleted{
			BookingID:     b.ID,
			PropertyID:    prop.ID,
			PropertyTitle: prop.Title,
			HostID:        prop.HostID,
			GuestID:       b.GuestID,
		},
	); err != nil {
		return nil, err
	}
	return b, nil
}

// BookedRange is a read-model describing an occupied window of a property. The
// status is "blocked" for host blocks, otherwise the booking status.
type BookedRange struct {
	CheckIn  time.Time
	CheckOut time.Time
	Status   string
}

// Availability returns the occupied date ranges for a property within the
// [from, to) window — both active bookings and host blocks — so clients can show
// which dates are unavailable. This is a public read and intentionally exposes no
// guest-identifying data.
func (s *Service) Availability(ctx context.Context, propertyID uuid.UUID, from, to time.Time) ([]BookedRange, error) {
	if !to.After(from) {
		return nil, shared.NewValidationError("'to' must be after 'from'")
	}
	if _, err := s.properties.FindByID(ctx, propertyID); err != nil {
		return nil, err
	}
	active, err := s.bookings.ListActiveInRange(ctx, propertyID, from, to)
	if err != nil {
		return nil, err
	}
	blocks, err := s.blocks.ListInRange(ctx, propertyID, from, to)
	if err != nil {
		return nil, err
	}

	ranges := make([]BookedRange, 0, len(active)+len(blocks))
	for _, b := range active {
		ranges = append(ranges, BookedRange{CheckIn: b.Dates.CheckIn, CheckOut: b.Dates.CheckOut, Status: string(b.Status)})
	}
	for _, bl := range blocks {
		ranges = append(ranges, BookedRange{CheckIn: bl.Dates.CheckIn, CheckOut: bl.Dates.CheckOut, Status: "blocked"})
	}
	return ranges, nil
}

func (s *Service) bookingWithProperty(ctx context.Context, bookingID uuid.UUID) (*booking.Booking, *property.Property, error) {
	b, err := s.bookings.FindByID(ctx, bookingID)
	if err != nil {
		return nil, nil, err
	}
	prop, err := s.properties.FindByID(ctx, b.PropertyID)
	if err != nil {
		return nil, nil, err
	}
	return b, prop, nil
}
