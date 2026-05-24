// Package dto holds the HTTP view models and the presenters that map domain
// aggregates onto them, keeping transport shapes out of the domain.
package dto

import (
	"time"

	analyticsapp "github.com/airhost/backend/internal/application/analytics"
	bookingapp "github.com/airhost/backend/internal/application/booking"
	"github.com/airhost/backend/internal/domain/block"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/identity"
	"github.com/airhost/backend/internal/domain/message"
	"github.com/airhost/backend/internal/domain/notification"
	"github.com/airhost/backend/internal/domain/payment"
	"github.com/airhost/backend/internal/domain/payout"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/review"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
)

// EmailPreferencesView renders a user's transactional email opt-ins.
type EmailPreferencesView struct {
	Bookings bool `json:"bookings"`
	Messages bool `json:"messages"`
}

// UserView is the public representation of a user.
type UserView struct {
	ID               uuid.UUID            `json:"id"`
	Email            string               `json:"email"`
	FullName         string               `json:"fullName"`
	Role             string               `json:"role"`
	AvatarURL        string               `json:"avatarUrl"`
	EmailPreferences EmailPreferencesView `json:"emailPreferences"`
	CreatedAt        time.Time            `json:"createdAt"`
}

// FromUser maps a user aggregate to its view.
func FromUser(u *user.User) UserView {
	return UserView{
		ID:        u.ID,
		Email:     u.Email,
		FullName:  u.FullName,
		Role:      string(u.Role),
		AvatarURL: u.AvatarURL,
		EmailPreferences: EmailPreferencesView{
			Bookings: u.EmailPrefs.Bookings,
			Messages: u.EmailPrefs.Messages,
		},
		CreatedAt: u.CreatedAt,
	}
}

// MoneyView renders a money value object.
type MoneyView struct {
	AmountCents int64  `json:"amountCents"`
	Currency    string `json:"currency"`
	Display     string `json:"display"`
}

func fromMoney(m shared.Money) MoneyView {
	return MoneyView{AmountCents: m.AmountCents(), Currency: m.Currency(), Display: m.String()}
}

// PhotoView renders a property photo.
type PhotoView struct {
	ID       uuid.UUID `json:"id"`
	URL      string    `json:"url"`
	Position int       `json:"position"`
}

// AddressView renders an address value object.
type AddressView struct {
	Line1      string  `json:"line1"`
	City       string  `json:"city"`
	Country    string  `json:"country"`
	PostalCode string  `json:"postalCode"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
}

// PropertyView is the public representation of a listing.
type PropertyView struct {
	ID                 uuid.UUID   `json:"id"`
	HostID             uuid.UUID   `json:"hostId"`
	Title              string      `json:"title"`
	Description        string      `json:"description"`
	Type               string      `json:"type"`
	Status             string      `json:"status"`
	Address            AddressView `json:"address"`
	PricePerNight      MoneyView   `json:"pricePerNight"`
	CleaningFee        MoneyView   `json:"cleaningFee"`
	MaxGuests          int         `json:"maxGuests"`
	Bedrooms           int         `json:"bedrooms"`
	Beds               int         `json:"beds"`
	Bathrooms          int         `json:"bathrooms"`
	Amenities          []string    `json:"amenities"`
	Photos             []PhotoView `json:"photos"`
	CancellationPolicy string      `json:"cancellationPolicy"`
	WeeklyDiscountPct  float64     `json:"weeklyDiscountPct"`
	MonthlyDiscountPct float64     `json:"monthlyDiscountPct"`
	TaxRatePct         float64     `json:"taxRatePct"`
	AverageRating      float64     `json:"averageRating"`
	ReviewCount        int         `json:"reviewCount"`
	CreatedAt          time.Time   `json:"createdAt"`
}

// FromProperty maps a property aggregate to its view.
func FromProperty(p *property.Property) PropertyView {
	photos := make([]PhotoView, 0, len(p.Photos))
	for _, ph := range p.Photos {
		photos = append(photos, PhotoView{ID: ph.ID, URL: ph.URL, Position: ph.Position})
	}
	return PropertyView{
		ID:          p.ID,
		HostID:      p.HostID,
		Title:       p.Title,
		Description: p.Description,
		Type:        string(p.Type),
		Status:      string(p.Status),
		Address: AddressView{
			Line1:      p.Address.Line1,
			City:       p.Address.City,
			Country:    p.Address.Country,
			PostalCode: p.Address.PostalCode,
			Latitude:   p.Address.Latitude,
			Longitude:  p.Address.Longitude,
		},
		PricePerNight:      fromMoney(p.PricePerNight),
		CleaningFee:        fromMoney(p.CleaningFee),
		MaxGuests:          p.MaxGuests,
		Bedrooms:           p.Bedrooms,
		Beds:               p.Beds,
		Bathrooms:          p.Bathrooms,
		Amenities:          p.Amenities,
		Photos:             photos,
		CancellationPolicy: string(p.CancellationPolicy),
		WeeklyDiscountPct:  p.PricingPolicy.WeeklyDiscountPct,
		MonthlyDiscountPct: p.PricingPolicy.MonthlyDiscountPct,
		TaxRatePct:         p.PricingPolicy.TaxRatePct,
		AverageRating:      p.AverageRating,
		ReviewCount:        p.ReviewCount,
		CreatedAt:          p.CreatedAt,
	}
}

// BookingView is the public representation of a reservation, including the
// full price breakdown.
type BookingView struct {
	ID          uuid.UUID `json:"id"`
	PropertyID  uuid.UUID `json:"propertyId"`
	GuestID     uuid.UUID `json:"guestId"`
	CheckIn     string    `json:"checkIn"`
	CheckOut    string    `json:"checkOut"`
	Nights      int       `json:"nights"`
	Guests      int       `json:"guests"`
	Subtotal    MoneyView `json:"subtotal"`
	Discount    MoneyView `json:"discount"`
	CleaningFee MoneyView `json:"cleaningFee"`
	ServiceFee  MoneyView `json:"serviceFee"`
	Tax         MoneyView `json:"tax"`
	TotalPrice  MoneyView `json:"totalPrice"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
}

// FromBooking maps a booking aggregate to its view.
func FromBooking(b *booking.Booking) BookingView {
	return BookingView{
		ID:          b.ID,
		PropertyID:  b.PropertyID,
		GuestID:     b.GuestID,
		CheckIn:     b.Dates.CheckIn.Format("2006-01-02"),
		CheckOut:    b.Dates.CheckOut.Format("2006-01-02"),
		Nights:      b.Dates.Nights(),
		Guests:      b.Guests,
		Subtotal:    fromMoney(b.Pricing.Subtotal),
		Discount:    fromMoney(b.Pricing.Discount),
		CleaningFee: fromMoney(b.Pricing.CleaningFee),
		ServiceFee:  fromMoney(b.Pricing.ServiceFee),
		Tax:         fromMoney(b.Pricing.Tax),
		TotalPrice:  fromMoney(b.Pricing.Total),
		Status:      string(b.Status),
		CreatedAt:   b.CreatedAt,
	}
}

// BookedRangeView renders an occupied window in the availability response.
type BookedRangeView struct {
	CheckIn  string `json:"checkIn"`
	CheckOut string `json:"checkOut"`
	Status   string `json:"status"`
}

// FromBookedRanges maps application booked ranges to their view models.
func FromBookedRanges(ranges []bookingapp.BookedRange) []BookedRangeView {
	out := make([]BookedRangeView, 0, len(ranges))
	for _, r := range ranges {
		out = append(out, BookedRangeView{
			CheckIn:  r.CheckIn.Format("2006-01-02"),
			CheckOut: r.CheckOut.Format("2006-01-02"),
			Status:   r.Status,
		})
	}
	return out
}

// ReviewView is the public representation of a review.
type ReviewView struct {
	ID         uuid.UUID `json:"id"`
	Kind       string    `json:"kind"`
	PropertyID uuid.UUID `json:"propertyId"`
	AuthorID   uuid.UUID `json:"authorId"`
	GuestID    uuid.UUID `json:"guestId"`
	Rating     int       `json:"rating"`
	Comment    string    `json:"comment"`
	CreatedAt  time.Time `json:"createdAt"`
}

// FromReview maps a review aggregate to its view.
func FromReview(r *review.Review) ReviewView {
	return ReviewView{
		ID:         r.ID,
		Kind:       string(r.Kind),
		PropertyID: r.PropertyID,
		AuthorID:   r.AuthorID,
		GuestID:    r.GuestID,
		Rating:     r.Rating,
		Comment:    r.Comment,
		CreatedAt:  r.CreatedAt,
	}
}

// ReviewSummaryView renders aggregate rating stats about a subject.
type ReviewSummaryView struct {
	SubjectID     uuid.UUID `json:"subjectId"`
	AverageRating float64   `json:"averageRating"`
	Count         int64     `json:"count"`
}

// FromReviewSummary maps a review summary to its view.
func FromReviewSummary(s review.Summary) ReviewSummaryView {
	return ReviewSummaryView{SubjectID: s.SubjectID, AverageRating: s.AverageRating, Count: s.Count}
}

// ConversationView is the public representation of a message thread.
type ConversationView struct {
	ID            uuid.UUID `json:"id"`
	PropertyID    uuid.UUID `json:"propertyId"`
	HostID        uuid.UUID `json:"hostId"`
	GuestID       uuid.UUID `json:"guestId"`
	CreatedAt     time.Time `json:"createdAt"`
	LastMessageAt time.Time `json:"lastMessageAt"`
	UnreadCount   int64     `json:"unreadCount"`
}

// FromConversation maps a conversation aggregate to its view, with the viewer's
// unread count.
func FromConversation(c *message.Conversation, unread int64) ConversationView {
	return ConversationView{
		ID:            c.ID,
		PropertyID:    c.PropertyID,
		HostID:        c.HostID,
		GuestID:       c.GuestID,
		CreatedAt:     c.CreatedAt,
		LastMessageAt: c.LastMessageAt,
		UnreadCount:   unread,
	}
}

// MessageView is the public representation of a message.
type MessageView struct {
	ID             uuid.UUID `json:"id"`
	ConversationID uuid.UUID `json:"conversationId"`
	SenderID       uuid.UUID `json:"senderId"`
	Body           string    `json:"body"`
	CreatedAt      time.Time `json:"createdAt"`
}

// FromMessage maps a message entity to its view.
func FromMessage(m *message.Message) MessageView {
	return MessageView{
		ID:             m.ID,
		ConversationID: m.ConversationID,
		SenderID:       m.SenderID,
		Body:           m.Body,
		CreatedAt:      m.CreatedAt,
	}
}

// NotificationView is the public representation of a notification.
type NotificationView struct {
	ID        uuid.UUID  `json:"id"`
	Type      string     `json:"type"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	RelatedID *uuid.UUID `json:"relatedId,omitempty"`
	Read      bool       `json:"read"`
	CreatedAt time.Time  `json:"createdAt"`
}

// FromNotification maps a notification aggregate to its view.
func FromNotification(n *notification.Notification) NotificationView {
	v := NotificationView{
		ID:        n.ID,
		Type:      string(n.Type),
		Title:     n.Title,
		Body:      n.Body,
		Read:      n.IsRead(),
		CreatedAt: n.CreatedAt,
	}
	if n.RelatedID != uuid.Nil {
		id := n.RelatedID
		v.RelatedID = &id
	}
	return v
}

// NotificationListView is the list response, including the unread count.
type NotificationListView struct {
	Items  []NotificationView `json:"items"`
	Total  int64              `json:"total"`
	Unread int64              `json:"unread"`
}

// PaymentView is the public representation of a payment.
type PaymentView struct {
	ID            uuid.UUID `json:"id"`
	BookingID     uuid.UUID `json:"bookingId"`
	Amount        MoneyView `json:"amount"`
	Status        string    `json:"status"`
	RefundedCents int64     `json:"refundedCents"`
	FailureReason string    `json:"failureReason,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

// FromPayment maps a payment aggregate to its view.
func FromPayment(p *payment.Payment) PaymentView {
	return PaymentView{
		ID:            p.ID,
		BookingID:     p.BookingID,
		Amount:        fromMoney(p.Amount),
		Status:        string(p.Status),
		RefundedCents: p.RefundedCents,
		FailureReason: p.FailureReason,
		CreatedAt:     p.CreatedAt,
	}
}

// HostMetricsView renders a host's dashboard analytics.
type HostMetricsView struct {
	Listings         int       `json:"listings"`
	Published        int       `json:"published"`
	Bookings         int       `json:"bookings"`
	Pending          int       `json:"pending"`
	Confirmed        int       `json:"confirmed"`
	Completed        int       `json:"completed"`
	Cancelled        int       `json:"cancelled"`
	UpcomingCheckins int       `json:"upcomingCheckins"`
	NightsBooked     int       `json:"nightsBooked"`
	CapturedRevenue  MoneyView `json:"capturedRevenue"`
	PendingRevenue   MoneyView `json:"pendingRevenue"`
	AverageRating    float64   `json:"averageRating"`
	ReviewCount      int       `json:"reviewCount"`
}

// FromHostMetrics maps the analytics read-model to its view.
func FromHostMetrics(m analyticsapp.HostMetrics) HostMetricsView {
	captured, _ := shared.NewMoney(m.CapturedCents, m.Currency)
	pending, _ := shared.NewMoney(m.PendingCents, m.Currency)
	return HostMetricsView{
		Listings:         m.Listings,
		Published:        m.Published,
		Bookings:         m.Bookings,
		Pending:          m.Pending,
		Confirmed:        m.Confirmed,
		Completed:        m.Completed,
		Cancelled:        m.Cancelled,
		UpcomingCheckins: m.UpcomingCheckins,
		NightsBooked:     m.NightsBooked,
		CapturedRevenue:  fromMoney(captured),
		PendingRevenue:   fromMoney(pending),
		AverageRating:    m.AverageRating,
		ReviewCount:      m.ReviewCount,
	}
}

// PayoutEntryView is the public representation of a host-earnings ledger line.
type PayoutEntryView struct {
	ID         uuid.UUID `json:"id"`
	BookingID  uuid.UUID `json:"bookingId"`
	PropertyID uuid.UUID `json:"propertyId"`
	Kind       string    `json:"kind"`
	Amount     MoneyView `json:"amount"`
	CreatedAt  time.Time `json:"createdAt"`
}

// FromPayoutEntry maps a ledger entry to its view.
func FromPayoutEntry(e *payout.Entry) PayoutEntryView {
	return PayoutEntryView{
		ID:         e.ID,
		BookingID:  e.BookingID,
		PropertyID: e.PropertyID,
		Kind:       string(e.Kind),
		Amount:     fromMoney(e.Amount),
		CreatedAt:  e.CreatedAt,
	}
}

// EarningsBalanceView renders a host's balance within a single currency.
type EarningsBalanceView struct {
	Currency string    `json:"currency"`
	Earned   MoneyView `json:"earned"`
	Refunded MoneyView `json:"refunded"`
	Net      MoneyView `json:"net"`
}

// EarningsSummaryView is the host earnings summary response.
type EarningsSummaryView struct {
	Balances []EarningsBalanceView `json:"balances"`
}

// FromBalances maps the payout balances read-model to its view.
func FromBalances(balances []payout.Balance) EarningsSummaryView {
	out := make([]EarningsBalanceView, 0, len(balances))
	for _, b := range balances {
		earned, _ := shared.NewMoney(b.EarnedCents, b.Currency)
		refunded, _ := shared.NewMoney(b.RefundedCents, b.Currency)
		net, _ := shared.NewMoney(b.NetCents(), b.Currency)
		out = append(out, EarningsBalanceView{
			Currency: b.Currency,
			Earned:   fromMoney(earned),
			Refunded: fromMoney(refunded),
			Net:      fromMoney(net),
		})
	}
	return EarningsSummaryView{Balances: out}
}

// BlockView is the public representation of a host calendar block.
type BlockView struct {
	ID        uuid.UUID `json:"id"`
	CheckIn   string    `json:"checkIn"`
	CheckOut  string    `json:"checkOut"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"createdAt"`
}

// FromBlock maps a block aggregate to its view.
func FromBlock(b *block.Block) BlockView {
	return BlockView{
		ID:        b.ID,
		CheckIn:   b.Dates.CheckIn.Format("2006-01-02"),
		CheckOut:  b.Dates.CheckOut.Format("2006-01-02"),
		Reason:    b.Reason,
		CreatedAt: b.CreatedAt,
	}
}

// VerificationView is the public representation of a KYC identity-verification
// request. The document reference is intentionally omitted from the view to
// avoid echoing potentially sensitive data back to clients.
type VerificationView struct {
	ID              uuid.UUID  `json:"id"`
	UserID          uuid.UUID  `json:"userId"`
	Status          string     `json:"status"`
	DocumentType    string     `json:"documentType"`
	LegalName       string     `json:"legalName"`
	RejectionReason string     `json:"rejectionReason,omitempty"`
	ReviewedAt      *time.Time `json:"reviewedAt,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
}

// FromVerification maps a verification aggregate to its view.
func FromVerification(v *identity.Verification) VerificationView {
	return VerificationView{
		ID:              v.ID,
		UserID:          v.UserID,
		Status:          string(v.Status),
		DocumentType:    string(v.DocumentType),
		LegalName:       v.LegalName,
		RejectionReason: v.RejectionReason,
		ReviewedAt:      v.ReviewedAt,
		CreatedAt:       v.CreatedAt,
	}
}

// PageView wraps a paginated list response.
type PageView[T any] struct {
	Items  []T   `json:"items"`
	Total  int64 `json:"total"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
}
