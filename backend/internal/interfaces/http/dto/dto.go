// Package dto holds the HTTP view models and the presenters that map domain
// aggregates onto them, keeping transport shapes out of the domain.
package dto

import (
	"time"

	alertstateapp "github.com/airhost/backend/internal/application/alertstate"
	analyticsapp "github.com/airhost/backend/internal/application/analytics"
	bookingapp "github.com/airhost/backend/internal/application/booking"
	payoutapp "github.com/airhost/backend/internal/application/payout"
	"github.com/airhost/backend/internal/application/port"
	privacyapp "github.com/airhost/backend/internal/application/privacy"
	reportapp "github.com/airhost/backend/internal/application/report"
	reviewapp "github.com/airhost/backend/internal/application/review"
	"github.com/airhost/backend/internal/domain/block"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/coupon"
	"github.com/airhost/backend/internal/domain/dispute"
	"github.com/airhost/backend/internal/domain/favorite"
	"github.com/airhost/backend/internal/domain/identity"
	"github.com/airhost/backend/internal/domain/message"
	"github.com/airhost/backend/internal/domain/messagetemplate"
	"github.com/airhost/backend/internal/domain/notification"
	"github.com/airhost/backend/internal/domain/offer"
	"github.com/airhost/backend/internal/domain/payment"
	"github.com/airhost/backend/internal/domain/payout"
	"github.com/airhost/backend/internal/domain/pricerule"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/pushtoken"
	"github.com/airhost/backend/internal/domain/report"
	"github.com/airhost/backend/internal/domain/review"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/splitpayment"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
)

// EmailPreferencesView renders a user's transactional email opt-ins.
type EmailPreferencesView struct {
	Bookings bool `json:"bookings"`
	Messages bool `json:"messages"`
}

// PushPreferencesView renders a user's native push opt-ins. Account category
// is not exposed because it cannot be opted out of (security notifications).
type PushPreferencesView struct {
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
	PushPreferences  PushPreferencesView  `json:"pushPreferences"`
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
		PushPreferences: PushPreferencesView{
			Bookings: u.PushPrefs.Bookings,
			Messages: u.PushPrefs.Messages,
		},
		CreatedAt: u.CreatedAt,
	}
}

// DisputeEvidenceView renders one piece of evidence attached to a dispute.
type DisputeEvidenceView struct {
	ID      uuid.UUID `json:"id"`
	URL     string    `json:"url,omitempty"`
	Note    string    `json:"note,omitempty"`
	AddedBy uuid.UUID `json:"addedBy"`
	AddedAt time.Time `json:"addedAt"`
}

// DisputeView renders a Resolution Center case. DueAt is the SLA deadline
// (OpenedAt + 7 days); Overdue is true when the case is still pending and
// `now` has passed DueAt — the admin queue surfaces those first so
// moderators triage them ahead of fresh cases.
type DisputeView struct {
	ID                   uuid.UUID             `json:"id"`
	BookingID            uuid.UUID             `json:"bookingId"`
	OpenerID             uuid.UUID             `json:"openerId"`
	Kind                 string                `json:"kind"`
	Reason               string                `json:"reason"`
	RequestedAmountCents int64                 `json:"requestedAmountCents"`
	Currency             string                `json:"currency"`
	Status               string                `json:"status"`
	HostResponse         string                `json:"hostResponse,omitempty"`
	Resolution           string                `json:"resolution,omitempty"`
	AdminID              uuid.UUID             `json:"adminId,omitempty"`
	OpenedAt             time.Time             `json:"openedAt"`
	DueAt                time.Time             `json:"dueAt"`
	Overdue              bool                  `json:"overdue"`
	DecidedAt            *time.Time            `json:"decidedAt,omitempty"`
	UpdatedAt            time.Time             `json:"updatedAt"`
	Evidence             []DisputeEvidenceView `json:"evidence"`
}

// FromDispute maps a dispute aggregate to its view, stamping the SLA fields
// from the current wall clock. Computing Overdue at presentation time keeps
// the queue accurate without a scheduled job mutating the aggregate.
func FromDispute(d *dispute.Dispute) DisputeView {
	evs := make([]DisputeEvidenceView, 0, len(d.Evidence))
	for _, e := range d.Evidence {
		evs = append(evs, DisputeEvidenceView{
			ID: e.ID, URL: e.URL, Note: e.Note, AddedBy: e.AddedBy, AddedAt: e.AddedAt,
		})
	}
	now := time.Now().UTC()
	return DisputeView{
		ID:                   d.ID,
		BookingID:            d.BookingID,
		OpenerID:             d.OpenerID,
		Kind:                 string(d.Kind),
		Reason:               d.Reason,
		RequestedAmountCents: d.RequestedAmountCents,
		Currency:             d.Currency,
		Status:               string(d.Status),
		HostResponse:         d.HostResponse,
		Resolution:           d.Resolution,
		AdminID:              d.AdminID,
		OpenedAt:             d.OpenedAt,
		DueAt:                d.DueAt(),
		Overdue:              d.IsOverdueAt(now),
		DecidedAt:            d.DecidedAt,
		UpdatedAt:            d.UpdatedAt,
		Evidence:             evs,
	}
}

// PushTokenView renders a registered device token.
type PushTokenView struct {
	ID        uuid.UUID `json:"id"`
	Platform  string    `json:"platform"`
	Token     string    `json:"token"`
	LastSeen  time.Time `json:"lastSeen"`
	CreatedAt time.Time `json:"createdAt"`
}

// FromPushToken maps a push-token aggregate to its view.
func FromPushToken(t *pushtoken.Token) PushTokenView {
	return PushTokenView{
		ID:        t.ID,
		Platform:  string(t.Platform),
		Token:     t.Token,
		LastSeen:  t.LastSeen,
		CreatedAt: t.CreatedAt,
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
	WeekendPriceCents  int64       `json:"weekendPriceCents"`
	InstantBook        bool        `json:"instantBook"`
	MinNights          int         `json:"minNights"`
	MaxNights          int         `json:"maxNights"`
	GuestsIncluded     int         `json:"guestsIncluded"`
	ExtraGuestFee      MoneyView         `json:"extraGuestFee"`
	SecurityDeposit    MoneyView         `json:"securityDeposit"`
	// Arrival is host-only: omitted on public reads, populated on host edit
	// reads via FromPropertyForHost. Guests fetch it through the dedicated
	// /bookings/:id/arrival endpoint, gated by the reveal window.
	Arrival            *ArrivalInfoView  `json:"arrival,omitempty"`
	AverageRating      float64           `json:"averageRating"`
	ReviewCount        int               `json:"reviewCount"`
	HostIsSuperhost    bool              `json:"hostIsSuperhost"`
	CreatedAt          time.Time         `json:"createdAt"`
}

// ArrivalInfoView is the wire shape for a listing's check-in and wifi info.
// It carries the credentials in the clear because the only endpoints serving
// it (host edit + /bookings/:id/arrival inside the reveal window) are
// authenticated and authorised before the body is built.
type ArrivalInfoView struct {
	CheckInMethod string `json:"checkInMethod"`
	Instructions  string `json:"arrivalInstructions"`
	WifiSSID      string `json:"wifiSsid"`
	WifiPassword  string `json:"wifiPassword"`
}

// FromArrivalInfo maps the domain value to its wire shape.
func FromArrivalInfo(a property.ArrivalInfo) ArrivalInfoView {
	return ArrivalInfoView{
		CheckInMethod: string(a.CheckInMethod),
		Instructions:  a.Instructions,
		WifiSSID:      a.WifiSSID,
		WifiPassword:  a.WifiPassword,
	}
}

// FromPropertyForHost is FromProperty plus the host-only Arrival block. Use
// this when the caller is verified to be the listing's host (e.g. the
// /host/properties feed or an edit GET).
func FromPropertyForHost(p *property.Property) PropertyView {
	v := FromProperty(p)
	a := FromArrivalInfo(p.Arrival)
	v.Arrival = &a
	return v
}

// CohostView is the wire shape for a per-listing co-host grant. It carries
// just enough user identification (id + email) for the host UI to recognise
// the invitee without exposing the rest of the profile.
type CohostView struct {
	ID          uuid.UUID `json:"id"`
	PropertyID  uuid.UUID `json:"propertyId"`
	UserID      uuid.UUID `json:"userId"`
	Email       string    `json:"email,omitempty"`
	DisplayName string    `json:"displayName,omitempty"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// FromCohost maps the domain grant to its wire shape. The optional `u` lets
// callers fold in the invitee's email/display name when they have it; passing
// nil omits those fields.
func FromCohost(c *property.Cohost, u *user.User) CohostView {
	perms := make([]string, 0, len(c.Permissions))
	for _, p := range c.Permissions {
		perms = append(perms, string(p))
	}
	v := CohostView{
		ID:          c.ID,
		PropertyID:  c.PropertyID,
		UserID:      c.UserID,
		Permissions: perms,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
	}
	if u != nil {
		v.Email = u.Email
		v.DisplayName = u.FullName
	}
	return v
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
		WeekendPriceCents:  p.PricingPolicy.WeekendPriceCents,
		InstantBook:        p.InstantBook,
		MinNights:          p.MinNights,
		MaxNights:          p.MaxNights,
		GuestsIncluded:     p.GuestsIncluded,
		ExtraGuestFee:      fromMoney(p.ExtraGuestFee),
		SecurityDeposit:    fromMoney(p.SecurityDeposit),
		AverageRating:      p.AverageRating,
		ReviewCount:        p.ReviewCount,
		HostIsSuperhost:    p.HostIsSuperhost,
		CreatedAt:          p.CreatedAt,
	}
}

// BookingView is the public representation of a reservation, including the
// full price breakdown.
type BookingView struct {
	ID              uuid.UUID `json:"id"`
	PropertyID      uuid.UUID `json:"propertyId"`
	GuestID         uuid.UUID `json:"guestId"`
	CheckIn         string    `json:"checkIn"`
	CheckOut        string    `json:"checkOut"`
	Nights          int       `json:"nights"`
	Guests          int       `json:"guests"`
	Subtotal        MoneyView `json:"subtotal"`
	Discount        MoneyView `json:"discount"`
	CleaningFee     MoneyView `json:"cleaningFee"`
	ExtraGuestFee   MoneyView `json:"extraGuestFee"`
	ServiceFee      MoneyView `json:"serviceFee"`
	Tax             MoneyView `json:"tax"`
	SecurityDeposit MoneyView `json:"securityDeposit"`
	TotalPrice      MoneyView `json:"totalPrice"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"createdAt"`
}

// FromBooking maps a booking aggregate to its view.
func FromBooking(b *booking.Booking) BookingView {
	return BookingView{
		ID:              b.ID,
		PropertyID:      b.PropertyID,
		GuestID:         b.GuestID,
		CheckIn:         b.Dates.CheckIn.Format("2006-01-02"),
		CheckOut:        b.Dates.CheckOut.Format("2006-01-02"),
		Nights:          b.Dates.Nights(),
		Guests:          b.Guests,
		Subtotal:        fromMoney(b.Pricing.Subtotal),
		Discount:        fromMoney(b.Pricing.Discount),
		CleaningFee:     fromMoney(b.Pricing.CleaningFee),
		ExtraGuestFee:   fromMoney(b.Pricing.ExtraGuestFee),
		ServiceFee:      fromMoney(b.Pricing.ServiceFee),
		Tax:             fromMoney(b.Pricing.Tax),
		SecurityDeposit: fromMoney(b.Pricing.SecurityDeposit),
		TotalPrice:      fromMoney(b.Pricing.Total),
		Status:          string(b.Status),
		CreatedAt:       b.CreatedAt,
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

// CategoryRatingsView renders a guest's per-aspect sub-ratings (1..5 each).
type CategoryRatingsView struct {
	Cleanliness   int `json:"cleanliness"`
	Accuracy      int `json:"accuracy"`
	Communication int `json:"communication"`
	Location      int `json:"location"`
	CheckIn       int `json:"checkIn"`
	Value         int `json:"value"`
}

// CategoryAveragesView renders mean per-aspect ratings across a property's reviews.
type CategoryAveragesView struct {
	Cleanliness   float64 `json:"cleanliness"`
	Accuracy      float64 `json:"accuracy"`
	Communication float64 `json:"communication"`
	Location      float64 `json:"location"`
	CheckIn       float64 `json:"checkIn"`
	Value         float64 `json:"value"`
}

// ReviewView is the public representation of a review.
type ReviewView struct {
	ID          uuid.UUID            `json:"id"`
	Kind        string               `json:"kind"`
	PropertyID  uuid.UUID            `json:"propertyId"`
	AuthorID    uuid.UUID            `json:"authorId"`
	GuestID     uuid.UUID            `json:"guestId"`
	Rating      int                  `json:"rating"`
	Comment     string               `json:"comment"`
	Categories  *CategoryRatingsView `json:"categories,omitempty"`
	Response    string               `json:"response,omitempty"`
	RespondedAt *time.Time           `json:"respondedAt,omitempty"`
	CreatedAt   time.Time            `json:"createdAt"`
	UpdatedAt   *time.Time           `json:"updatedAt,omitempty"`
}

// FromReview maps a review aggregate to its view.
func FromReview(r *review.Review) ReviewView {
	v := ReviewView{
		ID:          r.ID,
		Kind:        string(r.Kind),
		PropertyID:  r.PropertyID,
		AuthorID:    r.AuthorID,
		GuestID:     r.GuestID,
		Rating:      r.Rating,
		Comment:     r.Comment,
		Response:    r.Response,
		RespondedAt: r.RespondedAt,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
	if r.Categories.Any() {
		v.Categories = &CategoryRatingsView{
			Cleanliness:   r.Categories.Cleanliness,
			Accuracy:      r.Categories.Accuracy,
			Communication: r.Categories.Communication,
			Location:      r.Categories.Location,
			CheckIn:       r.Categories.CheckIn,
			Value:         r.Categories.Value,
		}
	}
	return v
}

// ReviewSummaryView renders aggregate rating stats about a subject.
type ReviewSummaryView struct {
	SubjectID     uuid.UUID             `json:"subjectId"`
	AverageRating float64               `json:"averageRating"`
	Count         int64                 `json:"count"`
	Categories    *CategoryAveragesView `json:"categories,omitempty"`
}

// FromReviewSummary maps a review summary to its view.
func FromReviewSummary(s review.Summary) ReviewSummaryView {
	v := ReviewSummaryView{SubjectID: s.SubjectID, AverageRating: s.AverageRating, Count: s.Count}
	if s.Categories.Any() {
		v.Categories = &CategoryAveragesView{
			Cleanliness:   s.Categories.Cleanliness,
			Accuracy:      s.Categories.Accuracy,
			Communication: s.Categories.Communication,
			Location:      s.Categories.Location,
			CheckIn:       s.Categories.CheckIn,
			Value:         s.Categories.Value,
		}
	}
	return v
}

// CollectionView is the public representation of a wishlist collection, with the
// number of listings saved in it.
type CollectionView struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Count      int       `json:"count"`
	ShareToken string    `json:"shareToken,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

// FromCollection maps a collection (with its count) to its view.
func FromCollection(c favorite.CollectionWithCount) CollectionView {
	return CollectionView{
		ID:         c.Collection.ID,
		Name:       c.Collection.Name,
		Count:      c.Count,
		ShareToken: c.Collection.ShareToken,
		CreatedAt:  c.Collection.CreatedAt,
	}
}

// FromNewCollection maps a freshly created collection (count 0) to its view.
func FromNewCollection(c *favorite.Collection) CollectionView {
	return CollectionView{ID: c.ID, Name: c.Name, Count: 0, CreatedAt: c.CreatedAt}
}

// CouponView is the public representation of a promo code.
type CouponView struct {
	ID             uuid.UUID  `json:"id"`
	Code           string     `json:"code"`
	Kind           string     `json:"kind"`
	Percent        float64    `json:"percent,omitempty"`
	Amount         *MoneyView `json:"amount,omitempty"`
	MinNights      int        `json:"minNights"`
	MaxRedemptions int        `json:"maxRedemptions"`
	Redemptions    int        `json:"redemptions"`
	ExpiresAt      *time.Time `json:"expiresAt,omitempty"`
	Active         bool       `json:"active"`
	CreatedAt      time.Time  `json:"createdAt"`
}

// FromCoupon maps a coupon aggregate to its view.
func FromCoupon(c *coupon.Coupon) CouponView {
	v := CouponView{
		ID:             c.ID,
		Code:           c.Code,
		Kind:           string(c.Kind),
		MinNights:      c.MinNights,
		MaxRedemptions: c.MaxRedemptions,
		Redemptions:    c.Redemptions,
		ExpiresAt:      c.ExpiresAt,
		Active:         c.Active,
		CreatedAt:      c.CreatedAt,
	}
	switch c.Kind {
	case coupon.KindPercentage:
		v.Percent = c.Percent
	case coupon.KindFixed:
		if m, err := shared.NewMoney(c.AmountCents, c.Currency); err == nil {
			mv := fromMoney(m)
			v.Amount = &mv
		}
	}
	return v
}

// CouponPreviewView renders the discount a code would yield for a stay.
type CouponPreviewView struct {
	Code     string    `json:"code"`
	Discount MoneyView `json:"discount"`
}

// FromCouponPreview maps a coupon preview to its view.
func FromCouponPreview(p bookingapp.CouponPreview) CouponPreviewView {
	m, _ := shared.NewMoney(p.DiscountCents, p.Currency)
	return CouponPreviewView{Code: p.Code, Discount: fromMoney(m)}
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

// AttachmentView renders a message's optional file attachment.
type AttachmentView struct {
	URL         string `json:"url"`
	ContentType string `json:"contentType"`
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
}

// MessageView is the public representation of a message.
type MessageView struct {
	ID             uuid.UUID       `json:"id"`
	ConversationID uuid.UUID       `json:"conversationId"`
	SenderID       uuid.UUID       `json:"senderId"`
	Body           string          `json:"body"`
	Attachment     *AttachmentView `json:"attachment,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
}

// FromMessage maps a message entity to its view.
func FromMessage(m *message.Message) MessageView {
	v := MessageView{
		ID:             m.ID,
		ConversationID: m.ConversationID,
		SenderID:       m.SenderID,
		Body:           m.Body,
		CreatedAt:      m.CreatedAt,
	}
	if m.Attachment != nil {
		v.Attachment = &AttachmentView{
			URL:         m.Attachment.URL,
			ContentType: m.Attachment.ContentType,
			Filename:    m.Attachment.Filename,
			Size:        m.Attachment.Size,
		}
	}
	return v
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
	ID               uuid.UUID            `json:"id"`
	BookingID        uuid.UUID            `json:"bookingId"`
	Amount           MoneyView            `json:"amount"`
	Status           string               `json:"status"`
	RefundedCents    int64                `json:"refundedCents"`
	DamageClaimCents int64                `json:"damageClaimCents"`
	FailureReason    string               `json:"failureReason,omitempty"`
	Adjustments      []PaymentAdjustment  `json:"adjustments,omitempty"`
	CreatedAt        time.Time            `json:"createdAt"`
}

// PaymentAdjustment is a single audit-ledger entry on a payment (partial
// refund or damage claim) attributed to a source aggregate.
type PaymentAdjustment struct {
	ID          uuid.UUID `json:"id"`
	Kind        string    `json:"kind"`
	AmountCents int64     `json:"amountCents"`
	Reason      string    `json:"reason,omitempty"`
	RefKind     string    `json:"refKind,omitempty"`
	RefID       string    `json:"refId,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

// FromPayment maps a payment aggregate to its view.
func FromPayment(p *payment.Payment) PaymentView {
	adjs := make([]PaymentAdjustment, 0, len(p.Adjustments))
	for _, a := range p.Adjustments {
		refID := ""
		if a.RefID != (uuid.UUID{}) {
			refID = a.RefID.String()
		}
		adjs = append(adjs, PaymentAdjustment{
			ID:          a.ID,
			Kind:        string(a.Kind),
			AmountCents: a.AmountCents,
			Reason:      a.Reason,
			RefKind:     a.RefKind,
			RefID:       refID,
			CreatedAt:   a.CreatedAt,
		})
	}
	return PaymentView{
		ID:               p.ID,
		BookingID:        p.BookingID,
		Amount:           fromMoney(p.Amount),
		Status:           string(p.Status),
		RefundedCents:    p.RefundedCents,
		DamageClaimCents: p.DamageClaimCents,
		FailureReason:    p.FailureReason,
		Adjustments:      adjs,
		CreatedAt:        p.CreatedAt,
	}
}

// DepositView is the public representation of a security-deposit hold.
type DepositView struct {
	ID            uuid.UUID           `json:"id"`
	BookingID     uuid.UUID           `json:"bookingId"`
	Amount        MoneyView           `json:"amount"`
	CapturedCents int64               `json:"capturedCents"`
	Remaining     int64               `json:"remainingCents"`
	Status        string              `json:"status"`
	FailureReason string              `json:"failureReason,omitempty"`
	Adjustments   []PaymentAdjustment `json:"adjustments,omitempty"`
	CreatedAt     time.Time           `json:"createdAt"`
	ReleasedAt    *time.Time          `json:"releasedAt,omitempty"`
}

// FromDeposit maps a DepositHold aggregate to its public view.
func FromDeposit(d *payment.DepositHold) DepositView {
	adjs := make([]PaymentAdjustment, 0, len(d.Adjustments))
	for _, a := range d.Adjustments {
		refID := ""
		if a.RefID != (uuid.UUID{}) {
			refID = a.RefID.String()
		}
		adjs = append(adjs, PaymentAdjustment{
			ID:          a.ID,
			Kind:        string(a.Kind),
			AmountCents: a.AmountCents,
			Reason:      a.Reason,
			RefKind:     a.RefKind,
			RefID:       refID,
			CreatedAt:   a.CreatedAt,
		})
	}
	return DepositView{
		ID:            d.ID,
		BookingID:     d.BookingID,
		Amount:        fromMoney(d.Amount),
		CapturedCents: d.CapturedCents,
		Remaining:     d.Remaining(),
		Status:        string(d.Status),
		FailureReason: d.FailureReason,
		Adjustments:   adjs,
		CreatedAt:     d.CreatedAt,
		ReleasedAt:    d.ReleasedAt,
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

// AvailableBalanceView renders a host's withdrawable balance in one currency.
type AvailableBalanceView struct {
	Currency  string    `json:"currency"`
	Available MoneyView `json:"available"`
}

// FromAvailableBalances maps the available-balance read-model to its view.
func FromAvailableBalances(balances []payoutapp.AvailableBalance) []AvailableBalanceView {
	out := make([]AvailableBalanceView, 0, len(balances))
	for _, b := range balances {
		amount, _ := shared.NewMoney(b.AvailableCents, b.Currency)
		out = append(out, AvailableBalanceView{Currency: b.Currency, Available: fromMoney(amount)})
	}
	return out
}

// PayoutAccountStatusView renders a host's payout-onboarding state.
type PayoutAccountStatusView struct {
	HasAccount bool `json:"hasAccount"`
	Enabled    bool `json:"enabled"`
}

// FromPayoutAccountStatus maps the payout-account status to its view.
func FromPayoutAccountStatus(s payoutapp.PayoutAccountStatus) PayoutAccountStatusView {
	return PayoutAccountStatusView{HasAccount: s.HasAccount, Enabled: s.Enabled}
}

// DisbursementView is the public representation of a host payout.
type DisbursementView struct {
	ID         uuid.UUID `json:"id"`
	Amount     MoneyView `json:"amount"`
	Status     string    `json:"status"`
	GatewayRef string    `json:"gatewayRef,omitempty"`
	Failure    string    `json:"failure,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

// FromDisbursement maps a disbursement aggregate to its view.
func FromDisbursement(d *payout.Disbursement) DisbursementView {
	amount, _ := shared.NewMoney(d.AmountCents, d.Currency)
	return DisbursementView{
		ID:         d.ID,
		Amount:     fromMoney(amount),
		Status:     string(d.Status),
		GatewayRef: d.GatewayRef,
		Failure:    d.Failure,
		CreatedAt:  d.CreatedAt,
	}
}

// DataExportView is the GDPR data-export document handed to a user.
type DataExportView struct {
	Profile             UserView           `json:"profile"`
	Bookings            []BookingView      `json:"bookings"`
	Payments            []PaymentView      `json:"payments"`
	FavoritePropertyIDs []uuid.UUID        `json:"favoritePropertyIds"`
	Notifications       []NotificationView `json:"notifications"`
	Earnings            []PayoutEntryView  `json:"earnings"`
	ReviewsAboutMe      []ReviewView       `json:"reviewsAboutMe"`
}

// FromExport maps an aggregated personal-data export to its view, reusing the
// per-aggregate presenters so the export matches the rest of the API.
func FromExport(e *privacyapp.Export) DataExportView {
	v := DataExportView{
		Profile:             FromUser(e.User),
		FavoritePropertyIDs: e.FavoriteIDs,
	}
	for _, b := range e.Bookings {
		v.Bookings = append(v.Bookings, FromBooking(b))
	}
	for _, p := range e.Payments {
		v.Payments = append(v.Payments, FromPayment(p))
	}
	for _, n := range e.Notifications {
		v.Notifications = append(v.Notifications, FromNotification(n))
	}
	for _, en := range e.PayoutEntries {
		v.Earnings = append(v.Earnings, FromPayoutEntry(en))
	}
	for _, r := range e.ReviewsAboutMe {
		v.ReviewsAboutMe = append(v.ReviewsAboutMe, FromReview(r))
	}
	if v.FavoritePropertyIDs == nil {
		v.FavoritePropertyIDs = []uuid.UUID{}
	}
	return v
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

// ReportView is the public representation of a listing report. PropertyTitle is
// populated for the administrator moderation queue.
type ReportView struct {
	ID            uuid.UUID  `json:"id"`
	TargetType    string     `json:"targetType"`
	TargetID      uuid.UUID  `json:"targetId"`
	PropertyID    uuid.UUID  `json:"propertyId"`
	PropertyTitle string     `json:"propertyTitle,omitempty"`
	ReporterID    uuid.UUID  `json:"reporterId"`
	Reason        string     `json:"reason"`
	Note          string     `json:"note,omitempty"`
	Status        string     `json:"status"`
	Resolution    string     `json:"resolution,omitempty"`
	ResolvedAt    *time.Time `json:"resolvedAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
}

// FromReport maps a report aggregate to its view (no listing title).
func FromReport(r *report.Report) ReportView {
	return ReportView{
		ID:         r.ID,
		TargetType: string(r.TargetType),
		TargetID:   r.TargetID,
		PropertyID: r.PropertyID,
		ReporterID: r.ReporterID,
		Reason:     string(r.Reason),
		Note:       r.Note,
		Status:     string(r.Status),
		Resolution: r.Resolution,
		ResolvedAt: r.ResolvedAt,
		CreatedAt:  r.CreatedAt,
	}
}

// FromEnrichedReport maps an enriched report (with listing title) to its view.
func FromEnrichedReport(e reportapp.EnrichedReport) ReportView {
	v := FromReport(e.Report)
	v.PropertyTitle = e.PropertyTitle
	return v
}

// OfferView is the public representation of a host's offer to a guest.
type OfferView struct {
	ID         uuid.UUID `json:"id"`
	PropertyID uuid.UUID `json:"propertyId"`
	HostID     uuid.UUID `json:"hostId"`
	GuestID    uuid.UUID `json:"guestId"`
	CheckIn    string    `json:"checkIn"`
	CheckOut   string    `json:"checkOut"`
	Guests     int       `json:"guests"`
	PriceCents int64     `json:"priceCents"`
	Currency   string    `json:"currency"`
	Message    string    `json:"message,omitempty"`
	Kind       string    `json:"kind"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

// FromOffer maps an offer aggregate to its view.
func FromOffer(o *offer.Offer) OfferView {
	return OfferView{
		ID:         o.ID,
		PropertyID: o.PropertyID,
		HostID:     o.HostID,
		GuestID:    o.GuestID,
		CheckIn:    o.CheckIn.Format("2006-01-02"),
		CheckOut:   o.CheckOut.Format("2006-01-02"),
		Guests:     o.Guests,
		PriceCents: o.PriceCents,
		Currency:   o.Currency,
		Message:    o.Message,
		Kind:       string(o.Kind),
		Status:     string(o.Status),
		CreatedAt:  o.CreatedAt,
		ExpiresAt:  o.ExpiresAt,
	}
}

// PageView wraps a paginated list response.
type PageView[T any] struct {
	Items  []T   `json:"items"`
	Total  int64 `json:"total"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
}

// SilenceMatcherView renders one Alertmanager silence matcher.
type SilenceMatcherView struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsRegex bool   `json:"isRegex"`
	IsEqual bool   `json:"isEqual"`
}

// SilenceView renders an Alertmanager silence (a maintenance mute window).
type SilenceView struct {
	ID        string               `json:"id"`
	Matchers  []SilenceMatcherView `json:"matchers"`
	StartsAt  time.Time            `json:"startsAt"`
	EndsAt    time.Time            `json:"endsAt"`
	CreatedBy string               `json:"createdBy"`
	Comment   string               `json:"comment"`
	Status    string               `json:"status"`
}

// PendingReviewView renders a completed stay awaiting the guest's review.
type PendingReviewView struct {
	BookingID     uuid.UUID `json:"bookingId"`
	PropertyID    uuid.UUID `json:"propertyId"`
	PropertyTitle string    `json:"propertyTitle"`
	CheckIn       time.Time `json:"checkIn"`
	CheckOut      time.Time `json:"checkOut"`
}

// FromPendingReview maps a pending-review record to its view.
func FromPendingReview(p reviewapp.PendingReview) PendingReviewView {
	return PendingReviewView{
		BookingID:     p.BookingID,
		PropertyID:    p.PropertyID,
		PropertyTitle: p.PropertyTitle,
		CheckIn:       p.CheckIn,
		CheckOut:      p.CheckOut,
	}
}

// AlertStateView renders the latest known state of an alert (firing/resolved)
// for the internal console.
type AlertStateView struct {
	Fingerprint string    `json:"fingerprint"`
	AlertName   string    `json:"alertName"`
	Severity    string    `json:"severity"`
	Status      string    `json:"status"`
	Summary     string    `json:"summary"`
	Description string    `json:"description"`
	RunbookURL  string    `json:"runbookUrl"`
	StartsAt    time.Time `json:"startsAt"`
	EndsAt      time.Time `json:"endsAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// FromAlertState maps an alert-state record to its view.
func FromAlertState(s alertstateapp.State) AlertStateView {
	return AlertStateView{
		Fingerprint: s.Fingerprint,
		AlertName:   s.AlertName,
		Severity:    s.Severity,
		Status:      s.Status,
		Summary:     s.Summary,
		Description: s.Description,
		RunbookURL:  s.RunbookURL,
		StartsAt:    s.StartsAt,
		EndsAt:      s.EndsAt,
		UpdatedAt:   s.UpdatedAt,
	}
}

// PriceRuleView is the public representation of a seasonal/per-date price
// override on a listing.
type PriceRuleView struct {
	ID         uuid.UUID `json:"id"`
	PropertyID uuid.UUID `json:"propertyId"`
	StartDate  string    `json:"startDate"`
	EndDate    string    `json:"endDate"`
	PriceCents int64     `json:"priceCents"`
	Currency   string    `json:"currency"`
	Label      string    `json:"label"`
	CreatedAt  time.Time `json:"createdAt"`
}

// FromPriceRule maps a price-rule aggregate to its view.
func FromPriceRule(r *pricerule.Rule) PriceRuleView {
	return PriceRuleView{
		ID:         r.ID,
		PropertyID: r.PropertyID,
		StartDate:  r.StartDate.Format("2006-01-02"),
		EndDate:    r.EndDate.Format("2006-01-02"),
		PriceCents: r.PriceCents,
		Currency:   r.Currency,
		Label:      r.Label,
		CreatedAt:  r.CreatedAt,
	}
}

// FromSilence maps a silence (from the AlertSilencer port) to its view.
func FromSilence(s port.Silence) SilenceView {
	matchers := make([]SilenceMatcherView, 0, len(s.Matchers))
	for _, m := range s.Matchers {
		matchers = append(matchers, SilenceMatcherView{
			Name:    m.Name,
			Value:   m.Value,
			IsRegex: m.IsRegex,
			IsEqual: m.IsEqual,
		})
	}
	return SilenceView{
		ID:        s.ID,
		Matchers:  matchers,
		StartsAt:  s.StartsAt,
		EndsAt:    s.EndsAt,
		CreatedBy: s.CreatedBy,
		Comment:   s.Comment,
		Status:    s.Status,
	}
}

// MessageTemplateView is the wire shape for a host's saved reply template.
type MessageTemplateView struct {
	ID        uuid.UUID `json:"id"`
	Label     string    `json:"label"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// FromMessageTemplate maps the aggregate to its wire shape.
func FromMessageTemplate(t *messagetemplate.Template) MessageTemplateView {
	return MessageTemplateView{
		ID:        t.ID,
		Label:     t.Label,
		Body:      t.Body,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
}

// SplitShareView is the wire shape for one traveller's portion of a split.
type SplitShareView struct {
	ID          uuid.UUID  `json:"id"`
	PayerEmail  string     `json:"payerEmail"`
	PayerUserID *uuid.UUID `json:"payerUserId,omitempty"`
	AmountCents int64      `json:"amountCents"`
	Status      string     `json:"status"`
	PaidAt      *time.Time `json:"paidAt,omitempty"`
}

// SplitPaymentView is the wire shape for a split-payment plan.
type SplitPaymentView struct {
	ID          uuid.UUID        `json:"id"`
	BookingID   uuid.UUID        `json:"bookingId"`
	OrganizerID uuid.UUID        `json:"organizerId"`
	Currency    string           `json:"currency"`
	TotalCents  int64            `json:"totalCents"`
	Status      string           `json:"status"`
	Shares      []SplitShareView `json:"shares"`
	CreatedAt   time.Time        `json:"createdAt"`
	CompletedAt *time.Time       `json:"completedAt,omitempty"`
	CancelledAt *time.Time       `json:"cancelledAt,omitempty"`
}

// FromSplitPayment maps the aggregate to its wire shape.
func FromSplitPayment(sp *splitpayment.SplitPayment) SplitPaymentView {
	shares := make([]SplitShareView, 0, len(sp.Shares))
	for _, s := range sp.Shares {
		shares = append(shares, SplitShareView{
			ID:          s.ID,
			PayerEmail:  s.PayerEmail,
			PayerUserID: s.PayerUserID,
			AmountCents: s.AmountCents,
			Status:      string(s.Status),
			PaidAt:      s.PaidAt,
		})
	}
	return SplitPaymentView{
		ID:          sp.ID,
		BookingID:   sp.BookingID,
		OrganizerID: sp.OrganizerID,
		Currency:    sp.Currency,
		TotalCents:  sp.TotalCents,
		Status:      string(sp.Status),
		Shares:      shares,
		CreatedAt:   sp.CreatedAt,
		CompletedAt: sp.CompletedAt,
		CancelledAt: sp.CancelledAt,
	}
}
