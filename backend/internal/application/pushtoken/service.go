// Package pushtokenapp contains push-token use cases: registering a device on
// login, unregistering one on logout / token rotation, and the dispatch helper
// that the notification subscriber uses to fan a push out to every device a
// user owns. It also tracks per-user push-category opt-outs by reading the user
// aggregate.
package pushtokenapp

import (
	"context"
	"log/slog"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/pushtoken"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
)

// Category buckets a push so per-user opt-outs can be applied. The categories
// mirror the email opt-outs (bookings, messages) plus an "account" bucket that
// is never silenced (security-relevant notifications).
type Category string

const (
	CatBookings Category = "bookings"
	CatMessages Category = "messages"
	CatAccount  Category = "account"
)

// Service orchestrates push-token use cases.
type Service struct {
	tokens pushtoken.Repository
	users  user.Repository
	sender port.PushSender
}

// NewService wires the push-token application service. When sender is nil
// pushes are silently dropped (useful in tests that don't care about delivery).
func NewService(tokens pushtoken.Repository, users user.Repository, sender port.PushSender) *Service {
	return &Service{tokens: tokens, users: users, sender: sender}
}

// Register stores a device token, upserting on (platform, token) so the same
// device re-registering only refreshes LastSeen (and switches owner if the
// device was previously linked to a different user).
func (s *Service) Register(ctx context.Context, userID uuid.UUID, platform pushtoken.Platform, token string) (*pushtoken.Token, error) {
	t, err := pushtoken.New(userID, platform, token)
	if err != nil {
		return nil, err
	}
	if err := s.tokens.Save(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// Unregister removes the (platform, token) row so the device stops receiving
// pushes. Used on logout or when the OS reports the token as invalid.
func (s *Service) Unregister(ctx context.Context, platform pushtoken.Platform, token string) error {
	if !platform.Valid() {
		return shared.NewValidationError("unknown platform")
	}
	if token == "" {
		return shared.NewValidationError("token is required")
	}
	return s.tokens.DeleteByToken(ctx, platform, token)
}

// ListForUser returns every active device registration for a user (for the
// settings screen and tests).
func (s *Service) ListForUser(ctx context.Context, userID uuid.UUID) ([]*pushtoken.Token, error) {
	return s.tokens.ListByUser(ctx, userID)
}

// Push delivers a notification to every device a user owns, honouring the
// per-category opt-out on the user aggregate. Failed sends are logged. Tokens
// the provider reports as dead are pruned locally so they stop racking up
// failures on subsequent dispatches.
func (s *Service) Push(ctx context.Context, userID uuid.UUID, cat Category, title, body string, data map[string]string) error {
	if s.sender == nil {
		return nil
	}
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if !optedIn(u.PushPrefs, cat) {
		return nil
	}
	devices, err := s.tokens.ListByUser(ctx, userID)
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		return nil
	}
	owned := make([]pushtoken.Token, 0, len(devices))
	for _, d := range devices {
		owned = append(owned, *d)
	}
	results := s.sender.Send(ctx, owned, port.PushPayload{Title: title, Body: body, Data: data})
	for _, r := range results {
		if r.Invalid {
			if e := s.tokens.DeleteByToken(ctx, r.Token.Platform, r.Token.Token); e != nil {
				slog.Warn("push: prune dead token failed", "platform", r.Token.Platform, "error", e)
			}
			continue
		}
		if r.Err != nil {
			slog.Warn("push: provider rejected delivery", "platform", r.Token.Platform, "error", r.Err)
		}
	}
	return nil
}

// optedIn reports whether a category may be delivered to a user. Account is
// always allowed (security-relevant), other categories follow the toggles.
func optedIn(prefs user.PushPreferences, cat Category) bool {
	switch cat {
	case CatAccount:
		return true
	case CatMessages:
		return prefs.Messages
	default:
		return prefs.Bookings
	}
}

// NotifierAdapter exposes Push with the loose string-cat signature that the
// notificationapp.PushNotifier port expects. It lets pushtokenapp wire into the
// notification subscriber without notification importing this package (avoiding
// a dependency cycle via user.Repository).
type NotifierAdapter struct{ svc *Service }

// AsNotifier returns the service wrapped in a notificationapp.PushNotifier
// adapter so the composition root can attach it via notificationSvc.WithPush.
func (s *Service) AsNotifier() *NotifierAdapter { return &NotifierAdapter{svc: s} }

// Push implements notificationapp.PushNotifier.
func (a *NotifierAdapter) Push(ctx context.Context, userID uuid.UUID, cat string, title, body string, data map[string]string) error {
	return a.svc.Push(ctx, userID, Category(cat), title, body, data)
}
