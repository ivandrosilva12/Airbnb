// Package dto holds the HTTP view models and the presenters that map domain
// aggregates onto them, keeping transport shapes out of the domain.
package dto

import (
	"time"

	bookingapp "github.com/airhost/backend/internal/application/booking"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/message"
	"github.com/airhost/backend/internal/domain/notification"
	"github.com/airhost/backend/internal/domain/payment"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/review"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
)

// UserView is the public representation of a user.
type UserView struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	FullName  string    `json:"fullName"`
	Role      string    `json:"role"`
	AvatarURL string    `json:"avatarUrl"`
	CreatedAt time.Time `json:"createdAt"`
}

// FromUser maps a user aggregate to its view.
func FromUser(u *user.User) UserView {
	return UserView{
		ID:        u.ID,
		Email:     u.Email,
		FullName:  u.FullName,
		Role:      string(u.Role),
		AvatarURL: u.AvatarURL,
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
	ID            uuid.UUID   `json:"id"`
	HostID        uuid.UUID   `json:"hostId"`
	Title         string      `json:"title"`
	Description   string      `json:"description"`
	Type          string      `json:"type"`
	Status        string      `json:"status"`
	Address       AddressView `json:"address"`
	PricePerNight MoneyView   `json:"pricePerNight"`
	CleaningFee   MoneyView   `json:"cleaningFee"`
	MaxGuests     int         `json:"maxGuests"`
	Bedrooms      int         `json:"bedrooms"`
	Beds          int         `json:"beds"`
	Bathrooms     int         `json:"bathrooms"`
	Amenities     []string    `json:"amenities"`
	Photos        []PhotoView `json:"photos"`
	CreatedAt     time.Time   `json:"createdAt"`
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
		PricePerNight: fromMoney(p.PricePerNight),
		CleaningFee:   fromMoney(p.CleaningFee),
		MaxGuests:     p.MaxGuests,
		Bedrooms:      p.Bedrooms,
		Beds:          p.Beds,
		Bathrooms:     p.Bathrooms,
		Amenities:     p.Amenities,
		Photos:        photos,
		CreatedAt:     p.CreatedAt,
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
	CleaningFee MoneyView `json:"cleaningFee"`
	ServiceFee  MoneyView `json:"serviceFee"`
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
		CleaningFee: fromMoney(b.Pricing.CleaningFee),
		ServiceFee:  fromMoney(b.Pricing.ServiceFee),
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
			Status:   string(r.Status),
		})
	}
	return out
}

// ReviewView is the public representation of a review.
type ReviewView struct {
	ID         uuid.UUID `json:"id"`
	PropertyID uuid.UUID `json:"propertyId"`
	GuestID    uuid.UUID `json:"guestId"`
	Rating     int       `json:"rating"`
	Comment    string    `json:"comment"`
	CreatedAt  time.Time `json:"createdAt"`
}

// FromReview maps a review aggregate to its view.
func FromReview(r *review.Review) ReviewView {
	return ReviewView{
		ID:         r.ID,
		PropertyID: r.PropertyID,
		GuestID:    r.GuestID,
		Rating:     r.Rating,
		Comment:    r.Comment,
		CreatedAt:  r.CreatedAt,
	}
}

// ReviewSummaryView renders aggregate rating stats.
type ReviewSummaryView struct {
	PropertyID    uuid.UUID `json:"propertyId"`
	AverageRating float64   `json:"averageRating"`
	Count         int64     `json:"count"`
}

// FromReviewSummary maps a review summary to its view.
func FromReviewSummary(s review.Summary) ReviewSummaryView {
	return ReviewSummaryView{PropertyID: s.PropertyID, AverageRating: s.AverageRating, Count: s.Count}
}

// ConversationView is the public representation of a message thread.
type ConversationView struct {
	ID            uuid.UUID `json:"id"`
	PropertyID    uuid.UUID `json:"propertyId"`
	HostID        uuid.UUID `json:"hostId"`
	GuestID       uuid.UUID `json:"guestId"`
	CreatedAt     time.Time `json:"createdAt"`
	LastMessageAt time.Time `json:"lastMessageAt"`
}

// FromConversation maps a conversation aggregate to its view.
func FromConversation(c *message.Conversation) ConversationView {
	return ConversationView{
		ID:            c.ID,
		PropertyID:    c.PropertyID,
		HostID:        c.HostID,
		GuestID:       c.GuestID,
		CreatedAt:     c.CreatedAt,
		LastMessageAt: c.LastMessageAt,
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
		FailureReason: p.FailureReason,
		CreatedAt:     p.CreatedAt,
	}
}

// PageView wraps a paginated list response.
type PageView[T any] struct {
	Items  []T   `json:"items"`
	Total  int64 `json:"total"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
}
