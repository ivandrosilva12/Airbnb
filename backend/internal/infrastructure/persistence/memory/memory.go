// Package memory provides in-memory implementations of the domain repository
// ports. They are goroutine-safe and used both by the test suite and as a
// lightweight backend for local experimentation without PostgreSQL.
package memory

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/message"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/review"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
)

// Compile-time guarantees that the in-memory repos satisfy the domain ports.
var (
	_ user.Repository     = (*UserRepository)(nil)
	_ property.Repository = (*PropertyRepository)(nil)
	_ booking.Repository  = (*BookingRepository)(nil)
	_ review.Repository   = (*ReviewRepository)(nil)
	_ message.Repository  = (*MessageRepository)(nil)
)

// --- Users -------------------------------------------------------------------

// UserRepository is an in-memory user.Repository.
type UserRepository struct {
	mu sync.RWMutex
	m  map[uuid.UUID]user.User
}

// NewUserRepository builds an empty in-memory user repository.
func NewUserRepository() *UserRepository { return &UserRepository{m: map[uuid.UUID]user.User{}} }

func (r *UserRepository) Create(_ context.Context, u *user.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.m {
		if existing.KeycloakSub == u.KeycloakSub || existing.Email == u.Email {
			return shared.ErrConflict
		}
	}
	r.m[u.ID] = *u
	return nil
}

func (r *UserRepository) Update(_ context.Context, u *user.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.m[u.ID]; !ok {
		return shared.ErrNotFound
	}
	r.m[u.ID] = *u
	return nil
}

func (r *UserRepository) FindByID(_ context.Context, id uuid.UUID) (*user.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if u, ok := r.m[id]; ok {
		c := u
		return &c, nil
	}
	return nil, shared.ErrNotFound
}

func (r *UserRepository) FindByKeycloakSub(_ context.Context, sub string) (*user.User, error) {
	return r.findBy(func(u user.User) bool { return u.KeycloakSub == sub })
}

func (r *UserRepository) FindByEmail(_ context.Context, email string) (*user.User, error) {
	return r.findBy(func(u user.User) bool { return u.Email == email })
}

func (r *UserRepository) findBy(pred func(user.User) bool) (*user.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, u := range r.m {
		if pred(u) {
			c := u
			return &c, nil
		}
	}
	return nil, shared.ErrNotFound
}

// --- Properties --------------------------------------------------------------

// PropertyRepository is an in-memory property.Repository.
type PropertyRepository struct {
	mu sync.RWMutex
	m  map[uuid.UUID]property.Property
}

// NewPropertyRepository builds an empty in-memory property repository.
func NewPropertyRepository() *PropertyRepository {
	return &PropertyRepository{m: map[uuid.UUID]property.Property{}}
}

func (r *PropertyRepository) Create(_ context.Context, p *property.Property) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[p.ID] = *p
	return nil
}

func (r *PropertyRepository) Update(_ context.Context, p *property.Property) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.m[p.ID]; !ok {
		return shared.ErrNotFound
	}
	r.m[p.ID] = *p
	return nil
}

func (r *PropertyRepository) Delete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.m[id]; !ok {
		return shared.ErrNotFound
	}
	delete(r.m, id)
	return nil
}

func (r *PropertyRepository) FindByID(_ context.Context, id uuid.UUID) (*property.Property, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if p, ok := r.m[id]; ok {
		c := p
		return &c, nil
	}
	return nil, shared.ErrNotFound
}

func (r *PropertyRepository) ListByHost(_ context.Context, hostID uuid.UUID, page shared.Page) (shared.PageResult[*property.Property], error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []*property.Property
	for _, p := range r.m {
		if p.HostID == hostID {
			c := p
			all = append(all, &c)
		}
	}
	return paginate(all, page), nil
}

func (r *PropertyRepository) Search(_ context.Context, c property.SearchCriteria) (shared.PageResult[*property.Property], error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var matched []*property.Property
	for _, p := range r.m {
		if p.Status != property.StatusPublished {
			continue
		}
		if c.City != "" && !strings.Contains(strings.ToLower(p.Address.City), strings.ToLower(c.City)) {
			continue
		}
		if c.Country != "" && !strings.EqualFold(p.Address.Country, c.Country) {
			continue
		}
		if c.Type != "" && p.Type != c.Type {
			continue
		}
		if c.MinGuests > 0 && p.MaxGuests < c.MinGuests {
			continue
		}
		if c.MaxPrice > 0 && p.PricePerNight.AmountCents() > c.MaxPrice {
			continue
		}
		if len(c.Amenities) > 0 && !containsAll(p.Amenities, c.Amenities) {
			continue
		}
		cp := p
		matched = append(matched, &cp)
	}
	return paginate(matched, c.Page), nil
}

// --- Bookings ----------------------------------------------------------------

// BookingRepository is an in-memory booking.Repository.
type BookingRepository struct {
	mu sync.RWMutex
	m  map[uuid.UUID]booking.Booking
}

// NewBookingRepository builds an empty in-memory booking repository.
func NewBookingRepository() *BookingRepository {
	return &BookingRepository{m: map[uuid.UUID]booking.Booking{}}
}

func (r *BookingRepository) Create(_ context.Context, b *booking.Booking) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[b.ID] = *b
	return nil
}

func (r *BookingRepository) Update(_ context.Context, b *booking.Booking) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.m[b.ID]; !ok {
		return shared.ErrNotFound
	}
	r.m[b.ID] = *b
	return nil
}

func (r *BookingRepository) FindByID(_ context.Context, id uuid.UUID) (*booking.Booking, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if b, ok := r.m[id]; ok {
		c := b
		return &c, nil
	}
	return nil, shared.ErrNotFound
}

func (r *BookingRepository) ListByGuest(_ context.Context, guestID uuid.UUID, page shared.Page) (shared.PageResult[*booking.Booking], error) {
	return r.list(func(b booking.Booking) bool { return b.GuestID == guestID }, page)
}

func (r *BookingRepository) ListByProperty(_ context.Context, propertyID uuid.UUID, page shared.Page) (shared.PageResult[*booking.Booking], error) {
	return r.list(func(b booking.Booking) bool { return b.PropertyID == propertyID }, page)
}

func (r *BookingRepository) list(pred func(booking.Booking) bool, page shared.Page) (shared.PageResult[*booking.Booking], error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []*booking.Booking
	for _, b := range r.m {
		if pred(b) {
			c := b
			all = append(all, &c)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	return paginate(all, page), nil
}

func (r *BookingRepository) HasOverlap(_ context.Context, propertyID uuid.UUID, dates booking.DateRange) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, b := range r.m {
		if b.PropertyID == propertyID && b.IsActive() && b.Dates.Overlaps(dates) {
			return true, nil
		}
	}
	return false, nil
}

func (r *BookingRepository) ListActiveInRange(_ context.Context, propertyID uuid.UUID, from, to time.Time) ([]*booking.Booking, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*booking.Booking
	for _, b := range r.m {
		if b.PropertyID != propertyID || !b.IsActive() {
			continue
		}
		if b.Dates.CheckIn.Before(to) && from.Before(b.Dates.CheckOut) {
			c := b
			out = append(out, &c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dates.CheckIn.Before(out[j].Dates.CheckIn) })
	return out, nil
}

// --- Reviews -----------------------------------------------------------------

// ReviewRepository is an in-memory review.Repository.
type ReviewRepository struct {
	mu sync.RWMutex
	m  map[uuid.UUID]review.Review
}

// NewReviewRepository builds an empty in-memory review repository.
func NewReviewRepository() *ReviewRepository {
	return &ReviewRepository{m: map[uuid.UUID]review.Review{}}
}

func (r *ReviewRepository) Create(_ context.Context, rv *review.Review) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.m {
		if existing.BookingID == rv.BookingID {
			return shared.ErrConflict
		}
	}
	r.m[rv.ID] = *rv
	return nil
}

func (r *ReviewRepository) ExistsForBooking(_ context.Context, bookingID uuid.UUID) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, rv := range r.m {
		if rv.BookingID == bookingID {
			return true, nil
		}
	}
	return false, nil
}

func (r *ReviewRepository) ListByProperty(_ context.Context, propertyID uuid.UUID, page shared.Page) (shared.PageResult[*review.Review], error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []*review.Review
	for _, rv := range r.m {
		if rv.PropertyID == propertyID {
			c := rv
			all = append(all, &c)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	return paginate(all, page), nil
}

func (r *ReviewRepository) SummaryForProperty(_ context.Context, propertyID uuid.UUID) (review.Summary, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var sum, count int64
	for _, rv := range r.m {
		if rv.PropertyID == propertyID {
			sum += int64(rv.Rating)
			count++
		}
	}
	s := review.Summary{PropertyID: propertyID, Count: count}
	if count > 0 {
		s.AverageRating = float64(sum) / float64(count)
	}
	return s, nil
}

// --- Messaging ---------------------------------------------------------------

// MessageRepository is an in-memory message.Repository.
type MessageRepository struct {
	mu            sync.RWMutex
	conversations map[uuid.UUID]message.Conversation
	messages      map[uuid.UUID]message.Message
}

// NewMessageRepository builds an empty in-memory message repository.
func NewMessageRepository() *MessageRepository {
	return &MessageRepository{
		conversations: map[uuid.UUID]message.Conversation{},
		messages:      map[uuid.UUID]message.Message{},
	}
}

func (r *MessageRepository) CreateConversation(_ context.Context, c *message.Conversation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.conversations {
		if existing.PropertyID == c.PropertyID && existing.GuestID == c.GuestID {
			return shared.ErrConflict
		}
	}
	r.conversations[c.ID] = *c
	return nil
}

func (r *MessageRepository) UpdateConversation(_ context.Context, c *message.Conversation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.conversations[c.ID]; !ok {
		return shared.ErrNotFound
	}
	r.conversations[c.ID] = *c
	return nil
}

func (r *MessageRepository) FindConversationByID(_ context.Context, id uuid.UUID) (*message.Conversation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if c, ok := r.conversations[id]; ok {
		cp := c
		return &cp, nil
	}
	return nil, shared.ErrNotFound
}

func (r *MessageRepository) FindConversationByPropertyAndGuest(_ context.Context, propertyID, guestID uuid.UUID) (*message.Conversation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.conversations {
		if c.PropertyID == propertyID && c.GuestID == guestID {
			cp := c
			return &cp, nil
		}
	}
	return nil, shared.ErrNotFound
}

func (r *MessageRepository) ListConversationsForUser(_ context.Context, userID uuid.UUID, page shared.Page) (shared.PageResult[*message.Conversation], error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []*message.Conversation
	for _, c := range r.conversations {
		if c.HostID == userID || c.GuestID == userID {
			cp := c
			all = append(all, &cp)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].LastMessageAt.After(all[j].LastMessageAt) })
	return paginate(all, page), nil
}

func (r *MessageRepository) AddMessage(_ context.Context, m *message.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages[m.ID] = *m
	return nil
}

func (r *MessageRepository) ListMessages(_ context.Context, conversationID uuid.UUID, page shared.Page) (shared.PageResult[*message.Message], error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []*message.Message
	for _, m := range r.messages {
		if m.ConversationID == conversationID {
			cp := m
			all = append(all, &cp)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.Before(all[j].CreatedAt) })
	return paginate(all, page), nil
}

// --- helpers -----------------------------------------------------------------

func paginate[T any](items []T, page shared.Page) shared.PageResult[T] {
	total := int64(len(items))
	start := page.Offset
	if start > len(items) {
		start = len(items)
	}
	end := start + page.Limit
	if end > len(items) {
		end = len(items)
	}
	return shared.PageResult[T]{Items: items[start:end], Total: total}
}

func containsAll(haystack, needles []string) bool {
	set := make(map[string]struct{}, len(haystack))
	for _, h := range haystack {
		set[h] = struct{}{}
	}
	for _, n := range needles {
		if _, ok := set[n]; !ok {
			return false
		}
	}
	return true
}
