// Package memory provides in-memory implementations of the domain repository
// ports. They are goroutine-safe and used both by the test suite and as a
// lightweight backend for local experimentation without PostgreSQL.
package memory

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/airhost/backend/internal/domain/block"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/favorite"
	"github.com/airhost/backend/internal/domain/identity"
	"github.com/airhost/backend/internal/domain/message"
	"github.com/airhost/backend/internal/domain/notification"
	"github.com/airhost/backend/internal/domain/payment"
	"github.com/airhost/backend/internal/domain/payout"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/report"
	"github.com/airhost/backend/internal/domain/review"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
)

// Compile-time guarantees that the in-memory repos satisfy the domain ports.
var (
	_ user.Repository         = (*UserRepository)(nil)
	_ property.Repository     = (*PropertyRepository)(nil)
	_ booking.Repository      = (*BookingRepository)(nil)
	_ review.Repository       = (*ReviewRepository)(nil)
	_ message.Repository      = (*MessageRepository)(nil)
	_ favorite.Repository     = (*FavoriteRepository)(nil)
	_ notification.Repository = (*NotificationRepository)(nil)
	_ payment.Repository      = (*PaymentRepository)(nil)
	_ payout.Repository       = (*PayoutRepository)(nil)
	_ block.Repository        = (*BlockRepository)(nil)
	_ identity.Repository     = (*IdentityRepository)(nil)
	_ report.Repository       = (*ReportRepository)(nil)
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

func (r *UserRepository) FindByPayoutAccountID(_ context.Context, accountID string) (*user.User, error) {
	return r.findBy(func(u user.User) bool { return u.PayoutAccountID == accountID })
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

func (r *PropertyRepository) FindByIDs(_ context.Context, ids []uuid.UUID) ([]*property.Property, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*property.Property, 0, len(ids))
	for _, id := range ids {
		if p, ok := r.m[id]; ok {
			c := p
			out = append(out, &c)
		}
	}
	return out, nil
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
	excluded := make(map[uuid.UUID]struct{}, len(c.ExcludeIDs))
	for _, id := range c.ExcludeIDs {
		excluded[id] = struct{}{}
	}
	var matched []*property.Property
	for _, p := range r.m {
		if p.Status != property.StatusPublished {
			continue
		}
		if _, skip := excluded[p.ID]; skip {
			continue
		}
		if c.Query != "" {
			q := strings.ToLower(c.Query)
			if !strings.Contains(strings.ToLower(p.Title), q) &&
				!strings.Contains(strings.ToLower(p.Description), q) &&
				!strings.Contains(strings.ToLower(p.Address.City), q) {
				continue
			}
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
		if c.Geo != nil && c.Geo.RadiusKm > 0 &&
			haversineKm(c.Geo.Lat, c.Geo.Lng, p.Address.Latitude, p.Address.Longitude) > c.Geo.RadiusKm {
			continue
		}
		cp := p
		matched = append(matched, &cp)
	}
	sortProperties(matched, c.Sort)
	return paginate(matched, c.Page), nil
}

func (r *PropertyRepository) UpdateRating(_ context.Context, id uuid.UUID, average float64, count int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.m[id]
	if !ok {
		return shared.ErrNotFound
	}
	p.AverageRating = average
	p.ReviewCount = count
	r.m[id] = p
	return nil
}

func (r *PropertyRepository) HostRatingAggregate(_ context.Context, hostID uuid.UUID) (float64, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var (
		weighted float64
		count    int
	)
	for _, p := range r.m {
		if p.HostID == hostID && p.ReviewCount > 0 {
			weighted += p.AverageRating * float64(p.ReviewCount)
			count += p.ReviewCount
		}
	}
	if count == 0 {
		return 0, 0, nil
	}
	return weighted / float64(count), count, nil
}

func (r *PropertyRepository) SetHostSuperhost(_ context.Context, hostID uuid.UUID, superhost bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, p := range r.m {
		if p.HostID == hostID && p.HostIsSuperhost != superhost {
			p.HostIsSuperhost = superhost
			r.m[id] = p
		}
	}
	return nil
}

func sortProperties(items []*property.Property, sort property.Sort) {
	switch sort {
	case property.SortPriceAsc:
		byField(items, func(a, b *property.Property) bool {
			return a.PricePerNight.AmountCents() < b.PricePerNight.AmountCents()
		})
	case property.SortPriceDesc:
		byField(items, func(a, b *property.Property) bool {
			return a.PricePerNight.AmountCents() > b.PricePerNight.AmountCents()
		})
	case property.SortRating:
		byField(items, func(a, b *property.Property) bool { return a.AverageRating > b.AverageRating })
	default: // newest
		byField(items, func(a, b *property.Property) bool { return a.CreatedAt.After(b.CreatedAt) })
	}
}

func byField(items []*property.Property, less func(a, b *property.Property) bool) {
	sort.SliceStable(items, func(i, j int) bool { return less(items[i], items[j]) })
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

func (r *BookingRepository) ListByPropertyIDs(_ context.Context, propertyIDs []uuid.UUID, page shared.Page) (shared.PageResult[*booking.Booking], error) {
	if len(propertyIDs) == 0 {
		return shared.PageResult[*booking.Booking]{}, nil
	}
	set := make(map[uuid.UUID]struct{}, len(propertyIDs))
	for _, id := range propertyIDs {
		set[id] = struct{}{}
	}
	return r.list(func(b booking.Booking) bool { _, ok := set[b.PropertyID]; return ok }, page)
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
	// created_at DESC with an id tiebreaker, matching the Postgres repo so
	// pagination is deterministic when timestamps collide.
	sort.Slice(all, func(i, j int) bool {
		if !all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].CreatedAt.After(all[j].CreatedAt)
		}
		return all[i].ID.String() > all[j].ID.String()
	})
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

func (r *BookingRepository) BookedPropertyIDs(_ context.Context, from, to time.Time) ([]uuid.UUID, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := map[uuid.UUID]struct{}{}
	var ids []uuid.UUID
	for _, b := range r.m {
		if !b.IsActive() {
			continue
		}
		if b.Dates.CheckIn.Before(to) && from.Before(b.Dates.CheckOut) {
			if _, ok := seen[b.PropertyID]; !ok {
				seen[b.PropertyID] = struct{}{}
				ids = append(ids, b.PropertyID)
			}
		}
	}
	return ids, nil
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
	// At most one review per booking per direction (matches the DB unique index).
	for _, existing := range r.m {
		if existing.BookingID == rv.BookingID && existing.Kind == rv.Kind {
			return shared.ErrConflict
		}
	}
	r.m[rv.ID] = *rv
	return nil
}

func (r *ReviewRepository) FindByID(_ context.Context, id uuid.UUID) (*review.Review, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if rv, ok := r.m[id]; ok {
		c := rv
		return &c, nil
	}
	return nil, shared.ErrNotFound
}

func (r *ReviewRepository) Update(_ context.Context, rv *review.Review) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.m[rv.ID]; !ok {
		return shared.ErrNotFound
	}
	r.m[rv.ID] = *rv
	return nil
}

func (r *ReviewRepository) ExistsForBookingKind(_ context.Context, bookingID uuid.UUID, kind review.Kind) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, rv := range r.m {
		if rv.BookingID == bookingID && rv.Kind == kind {
			return true, nil
		}
	}
	return false, nil
}

func (r *ReviewRepository) ListByProperty(_ context.Context, propertyID uuid.UUID, page shared.Page) (shared.PageResult[*review.Review], error) {
	return r.list(func(rv review.Review) bool {
		return rv.PropertyID == propertyID && rv.Kind == review.KindGuestToProperty
	}, page)
}

func (r *ReviewRepository) ListAboutGuest(_ context.Context, guestID uuid.UUID, page shared.Page) (shared.PageResult[*review.Review], error) {
	return r.list(func(rv review.Review) bool {
		return rv.GuestID == guestID && rv.Kind == review.KindHostToGuest
	}, page)
}

func (r *ReviewRepository) list(pred func(review.Review) bool, page shared.Page) (shared.PageResult[*review.Review], error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []*review.Review
	for _, rv := range r.m {
		if pred(rv) {
			c := rv
			all = append(all, &c)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	return paginate(all, page), nil
}

func (r *ReviewRepository) SummaryForProperty(_ context.Context, propertyID uuid.UUID) (review.Summary, error) {
	return r.summary(propertyID, func(rv review.Review) bool {
		return rv.PropertyID == propertyID && rv.Kind == review.KindGuestToProperty
	}), nil
}

func (r *ReviewRepository) SummaryForGuest(_ context.Context, guestID uuid.UUID) (review.Summary, error) {
	return r.summary(guestID, func(rv review.Review) bool {
		return rv.GuestID == guestID && rv.Kind == review.KindHostToGuest
	}), nil
}

func (r *ReviewRepository) AnonymizeByAuthor(_ context.Context, authorID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, rv := range r.m {
		if rv.AuthorID == authorID {
			rv.Comment = ""
			r.m[id] = rv
		}
	}
	return nil
}

func (r *ReviewRepository) summary(subjectID uuid.UUID, pred func(review.Review) bool) review.Summary {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var sum, count int64
	for _, rv := range r.m {
		if pred(rv) {
			sum += int64(rv.Rating)
			count++
		}
	}
	s := review.Summary{SubjectID: subjectID, Count: count}
	if count > 0 {
		s.AverageRating = float64(sum) / float64(count)
	}
	return s
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

func (r *MessageRepository) ConversationUnreadCounts(_ context.Context, userID uuid.UUID) (map[uuid.UUID]int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[uuid.UUID]int64)
	for _, m := range r.messages {
		conv, ok := r.conversations[m.ConversationID]
		if !ok || !conv.HasParticipant(userID) || m.SenderID == userID {
			continue
		}
		if last := conv.LastReadFor(userID); last != nil && !m.CreatedAt.After(*last) {
			continue
		}
		out[m.ConversationID]++
	}
	return out, nil
}

func (r *MessageRepository) TotalUnread(ctx context.Context, userID uuid.UUID) (int64, error) {
	counts, _ := r.ConversationUnreadCounts(ctx, userID)
	var total int64
	for _, c := range counts {
		total += c
	}
	return total, nil
}

// --- Favorites ---------------------------------------------------------------

// FavoriteRepository is an in-memory favorite.Repository.
type FavoriteRepository struct {
	mu sync.RWMutex
	m  map[uuid.UUID][]favorite.Favorite // keyed by user id, newest last
}

// NewFavoriteRepository builds an empty in-memory favorite repository.
func NewFavoriteRepository() *FavoriteRepository {
	return &FavoriteRepository{m: map[uuid.UUID][]favorite.Favorite{}}
}

func (r *FavoriteRepository) Add(_ context.Context, f *favorite.Favorite) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.m[f.UserID] {
		if existing.PropertyID == f.PropertyID {
			return nil // idempotent
		}
	}
	r.m[f.UserID] = append(r.m[f.UserID], *f)
	return nil
}

func (r *FavoriteRepository) Remove(_ context.Context, userID, propertyID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.m[userID]
	out := list[:0]
	for _, f := range list {
		if f.PropertyID != propertyID {
			out = append(out, f)
		}
	}
	r.m[userID] = out
	return nil
}

func (r *FavoriteRepository) Exists(_ context.Context, userID, propertyID uuid.UUID) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, f := range r.m[userID] {
		if f.PropertyID == propertyID {
			return true, nil
		}
	}
	return false, nil
}

func (r *FavoriteRepository) ListPropertyIDs(_ context.Context, userID uuid.UUID, page shared.Page) (shared.PageResult[uuid.UUID], error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := r.m[userID]
	// Newest first.
	ids := make([]uuid.UUID, 0, len(list))
	for i := len(list) - 1; i >= 0; i-- {
		ids = append(ids, list[i].PropertyID)
	}
	return paginate(ids, page), nil
}

// --- Notifications -----------------------------------------------------------

// NotificationRepository is an in-memory notification.Repository.
type NotificationRepository struct {
	mu sync.RWMutex
	m  map[uuid.UUID]notification.Notification
}

// NewNotificationRepository builds an empty in-memory notification repository.
func NewNotificationRepository() *NotificationRepository {
	return &NotificationRepository{m: map[uuid.UUID]notification.Notification{}}
}

func (r *NotificationRepository) Create(_ context.Context, n *notification.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[n.ID] = *n
	return nil
}

func (r *NotificationRepository) ListByUser(_ context.Context, userID uuid.UUID, page shared.Page) (shared.PageResult[*notification.Notification], error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []*notification.Notification
	for _, n := range r.m {
		if n.UserID == userID {
			c := n
			all = append(all, &c)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	return paginate(all, page), nil
}

func (r *NotificationRepository) UnreadCount(_ context.Context, userID uuid.UUID) (int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var count int64
	for _, n := range r.m {
		if n.UserID == userID && n.ReadAt == nil {
			count++
		}
	}
	return count, nil
}

func (r *NotificationRepository) MarkRead(_ context.Context, id, userID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.m[id]
	if !ok || n.UserID != userID {
		return shared.ErrNotFound
	}
	n.MarkRead()
	r.m[id] = n
	return nil
}

func (r *NotificationRepository) MarkAllRead(_ context.Context, userID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, n := range r.m {
		if n.UserID == userID && n.ReadAt == nil {
			n.MarkRead()
			r.m[id] = n
		}
	}
	return nil
}

func (r *NotificationRepository) DeleteByUser(_ context.Context, userID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, n := range r.m {
		if n.UserID == userID {
			delete(r.m, id)
		}
	}
	return nil
}

// --- Payments ----------------------------------------------------------------

// PaymentRepository is an in-memory payment.Repository.
type PaymentRepository struct {
	mu sync.RWMutex
	m  map[uuid.UUID]payment.Payment
}

// NewPaymentRepository builds an empty in-memory payment repository.
func NewPaymentRepository() *PaymentRepository {
	return &PaymentRepository{m: map[uuid.UUID]payment.Payment{}}
}

func (r *PaymentRepository) Create(_ context.Context, p *payment.Payment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[p.ID] = *p
	return nil
}

func (r *PaymentRepository) Update(_ context.Context, p *payment.Payment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.m[p.ID]; !ok {
		return shared.ErrNotFound
	}
	r.m[p.ID] = *p
	return nil
}

func (r *PaymentRepository) FindByID(_ context.Context, id uuid.UUID) (*payment.Payment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if p, ok := r.m[id]; ok {
		c := p
		return &c, nil
	}
	return nil, shared.ErrNotFound
}

func (r *PaymentRepository) FindByBookingID(_ context.Context, bookingID uuid.UUID) (*payment.Payment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.m {
		if p.BookingID == bookingID {
			c := p
			return &c, nil
		}
	}
	return nil, shared.ErrNotFound
}

func (r *PaymentRepository) FindByGatewayRef(_ context.Context, gatewayRef string) (*payment.Payment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.m {
		if p.GatewayRef == gatewayRef {
			c := p
			return &c, nil
		}
	}
	return nil, shared.ErrNotFound
}

func (r *PaymentRepository) RevenueForBookings(_ context.Context, bookingIDs []uuid.UUID) (payment.Revenue, error) {
	want := make(map[uuid.UUID]struct{}, len(bookingIDs))
	for _, id := range bookingIDs {
		want[id] = struct{}{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	acc := payment.NewRevenueAccumulator()
	for _, p := range r.m {
		if _, ok := want[p.BookingID]; !ok {
			continue
		}
		acc.Add(p.Amount.Currency(), p.Status, p.Amount.AmountCents(), p.RefundedCents)
	}
	return acc.Dominant(), nil
}

func (r *PaymentRepository) ListByGuest(_ context.Context, guestID uuid.UUID, page shared.Page) (shared.PageResult[*payment.Payment], error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []*payment.Payment
	for _, p := range r.m {
		if p.GuestID == guestID {
			c := p
			all = append(all, &c)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	return paginate(all, page), nil
}

// --- Payouts -----------------------------------------------------------------

// PayoutRepository is an in-memory payout.Repository.
type PayoutRepository struct {
	mu sync.RWMutex
	m  map[uuid.UUID]payout.Entry
	d  map[uuid.UUID]payout.Disbursement
}

// NewPayoutRepository builds an empty in-memory payout repository.
func NewPayoutRepository() *PayoutRepository {
	return &PayoutRepository{m: map[uuid.UUID]payout.Entry{}, d: map[uuid.UUID]payout.Disbursement{}}
}

func (r *PayoutRepository) Create(_ context.Context, e *payout.Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[e.ID] = *e
	return nil
}

func (r *PayoutRepository) ListByHost(_ context.Context, hostID uuid.UUID, page shared.Page) (shared.PageResult[*payout.Entry], error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []*payout.Entry
	for _, e := range r.m {
		if e.HostID == hostID {
			c := e
			all = append(all, &c)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	return paginate(all, page), nil
}

func (r *PayoutRepository) HasEarningForBooking(_ context.Context, bookingID uuid.UUID) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.m {
		if e.BookingID == bookingID && e.Kind == payout.KindEarning {
			return true, nil
		}
	}
	return false, nil
}

func (r *PayoutRepository) BalancesByHost(_ context.Context, hostID uuid.UUID) ([]payout.Balance, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	byCurrency := map[string]*payout.Balance{}
	for _, e := range r.m {
		if e.HostID != hostID {
			continue
		}
		cur := e.Amount.Currency()
		b, ok := byCurrency[cur]
		if !ok {
			b = &payout.Balance{Currency: cur}
			byCurrency[cur] = b
		}
		if e.Kind == payout.KindRefund {
			b.RefundedCents += e.Amount.AmountCents()
		} else {
			b.EarnedCents += e.Amount.AmountCents()
		}
	}
	out := make([]payout.Balance, 0, len(byCurrency))
	for _, b := range byCurrency {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Currency < out[j].Currency })
	return out, nil
}

func (r *PayoutRepository) CreateDisbursement(_ context.Context, d *payout.Disbursement) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.d[d.ID] = *d
	return nil
}

func (r *PayoutRepository) UpdateDisbursement(_ context.Context, d *payout.Disbursement) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.d[d.ID]; !ok {
		return shared.ErrNotFound
	}
	r.d[d.ID] = *d
	return nil
}

func (r *PayoutRepository) ListDisbursementsByHost(_ context.Context, hostID uuid.UUID, page shared.Page) (shared.PageResult[*payout.Disbursement], error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []*payout.Disbursement
	for _, d := range r.d {
		if d.HostID == hostID {
			c := d
			all = append(all, &c)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].ID.String() > all[j].ID.String()
		}
		return all[i].CreatedAt.After(all[j].CreatedAt)
	})
	return paginate(all, page), nil
}

func (r *PayoutRepository) DisbursedCentsByHost(_ context.Context, hostID uuid.UUID) (map[string]int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := map[string]int64{}
	for _, d := range r.d {
		if d.HostID == hostID && d.Committed() {
			out[d.Currency] += d.AmountCents
		}
	}
	return out, nil
}

// --- Blocks ------------------------------------------------------------------

// BlockRepository is an in-memory block.Repository.
type BlockRepository struct {
	mu sync.RWMutex
	m  map[uuid.UUID]block.Block
}

// NewBlockRepository builds an empty in-memory block repository.
func NewBlockRepository() *BlockRepository {
	return &BlockRepository{m: map[uuid.UUID]block.Block{}}
}

func (r *BlockRepository) Create(_ context.Context, b *block.Block) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[b.ID] = *b
	return nil
}

func (r *BlockRepository) Delete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.m[id]; !ok {
		return shared.ErrNotFound
	}
	delete(r.m, id)
	return nil
}

func (r *BlockRepository) FindByID(_ context.Context, id uuid.UUID) (*block.Block, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if b, ok := r.m[id]; ok {
		c := b
		return &c, nil
	}
	return nil, shared.ErrNotFound
}

func (r *BlockRepository) ListByProperty(_ context.Context, propertyID uuid.UUID, page shared.Page) (shared.PageResult[*block.Block], error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []*block.Block
	for _, b := range r.m {
		if b.PropertyID == propertyID {
			c := b
			all = append(all, &c)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Dates.CheckIn.Before(all[j].Dates.CheckIn) })
	return paginate(all, page), nil
}

func (r *BlockRepository) HasOverlap(_ context.Context, propertyID uuid.UUID, dates booking.DateRange) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, b := range r.m {
		if b.PropertyID == propertyID && b.Dates.Overlaps(dates) {
			return true, nil
		}
	}
	return false, nil
}

func (r *BlockRepository) ListInRange(_ context.Context, propertyID uuid.UUID, from, to time.Time) ([]*block.Block, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*block.Block
	for _, b := range r.m {
		if b.PropertyID == propertyID && b.Dates.CheckIn.Before(to) && from.Before(b.Dates.CheckOut) {
			c := b
			out = append(out, &c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dates.CheckIn.Before(out[j].Dates.CheckIn) })
	return out, nil
}

func (r *BlockRepository) BlockedPropertyIDs(_ context.Context, from, to time.Time) ([]uuid.UUID, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := map[uuid.UUID]struct{}{}
	var ids []uuid.UUID
	for _, b := range r.m {
		if b.Dates.CheckIn.Before(to) && from.Before(b.Dates.CheckOut) {
			if _, ok := seen[b.PropertyID]; !ok {
				seen[b.PropertyID] = struct{}{}
				ids = append(ids, b.PropertyID)
			}
		}
	}
	return ids, nil
}

// --- Identity verifications --------------------------------------------------

// IdentityRepository is an in-memory identity.Repository.
type IdentityRepository struct {
	mu sync.RWMutex
	m  map[uuid.UUID]identity.Verification
}

// NewIdentityRepository builds an empty in-memory identity repository.
func NewIdentityRepository() *IdentityRepository {
	return &IdentityRepository{m: map[uuid.UUID]identity.Verification{}}
}

func (r *IdentityRepository) Create(_ context.Context, v *identity.Verification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[v.ID] = *v
	return nil
}

func (r *IdentityRepository) Update(_ context.Context, v *identity.Verification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.m[v.ID]; !ok {
		return shared.ErrNotFound
	}
	r.m[v.ID] = *v
	return nil
}

func (r *IdentityRepository) FindByID(_ context.Context, id uuid.UUID) (*identity.Verification, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if v, ok := r.m[id]; ok {
		c := v
		return &c, nil
	}
	return nil, shared.ErrNotFound
}

func (r *IdentityRepository) FindLatestByUser(_ context.Context, userID uuid.UUID) (*identity.Verification, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var latest *identity.Verification
	for _, v := range r.m {
		if v.UserID != userID {
			continue
		}
		if latest == nil || v.CreatedAt.After(latest.CreatedAt) {
			c := v
			latest = &c
		}
	}
	if latest == nil {
		return nil, shared.ErrNotFound
	}
	return latest, nil
}

func (r *IdentityRepository) ListByStatus(_ context.Context, status identity.Status, page shared.Page) (shared.PageResult[*identity.Verification], error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []*identity.Verification
	for _, v := range r.m {
		if v.Status == status {
			c := v
			all = append(all, &c)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	return paginate(all, page), nil
}

// --- Listing reports ---------------------------------------------------------

// ReportRepository is an in-memory report.Repository.
type ReportRepository struct {
	mu sync.RWMutex
	m  map[uuid.UUID]report.Report
}

// NewReportRepository builds an empty in-memory report repository.
func NewReportRepository() *ReportRepository {
	return &ReportRepository{m: map[uuid.UUID]report.Report{}}
}

func (r *ReportRepository) Create(_ context.Context, rep *report.Report) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[rep.ID] = *rep
	return nil
}

func (r *ReportRepository) Update(_ context.Context, rep *report.Report) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.m[rep.ID]; !ok {
		return shared.ErrNotFound
	}
	r.m[rep.ID] = *rep
	return nil
}

func (r *ReportRepository) FindByID(_ context.Context, id uuid.UUID) (*report.Report, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if rep, ok := r.m[id]; ok {
		c := rep
		return &c, nil
	}
	return nil, shared.ErrNotFound
}

func (r *ReportRepository) ListByStatus(_ context.Context, status report.Status, page shared.Page) (shared.PageResult[*report.Report], error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []*report.Report
	for _, rep := range r.m {
		if rep.Status == status {
			c := rep
			all = append(all, &c)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	return paginate(all, page), nil
}

func (r *ReportRepository) HasOpenFromReporter(_ context.Context, propertyID, reporterID uuid.UUID) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, rep := range r.m {
		if rep.PropertyID == propertyID && rep.ReporterID == reporterID && rep.Status == report.StatusOpen {
			return true, nil
		}
	}
	return false, nil
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

// haversineKm returns the great-circle distance in kilometres between two
// latitude/longitude points.
func haversineKm(lat1, lng1, lat2, lng2 float64) float64 {
	const earthRadiusKm = 6371.0
	dLat := (lat2 - lat1) * math.Pi / 180
	dLng := (lng2 - lng1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return earthRadiusKm * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
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
