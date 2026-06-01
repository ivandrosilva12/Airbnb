// Package pushtoken is the bounded context for device registrations used to
// deliver push notifications. A Token is one (user, device) pair: when the same
// physical device re-registers (on app start, after a token rotation) the
// repository upserts on (platform, token) so the per-device row is unique.
package pushtoken

import (
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Platform identifies the push transport that a token belongs to. Each platform
// is dispatched by its own infrastructure adapter (FCM, APNs, Web Push).
type Platform string

const (
	PlatformIOS     Platform = "ios"
	PlatformAndroid Platform = "android"
	PlatformWeb     Platform = "web"
)

// Valid reports whether the platform is one of the known values.
func (p Platform) Valid() bool {
	switch p {
	case PlatformIOS, PlatformAndroid, PlatformWeb:
		return true
	default:
		return false
	}
}

// Token is the aggregate root for one device registration.
type Token struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Platform  Platform
	Token     string
	LastSeen  time.Time
	CreatedAt time.Time
}

// New builds a Token, enforcing invariants.
func New(userID uuid.UUID, platform Platform, token string) (*Token, error) {
	if userID == uuid.Nil {
		return nil, shared.NewValidationError("push token requires a user")
	}
	if !platform.Valid() {
		return nil, shared.NewValidationError("push token requires a known platform")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, shared.NewValidationError("push token is required")
	}
	now := time.Now().UTC()
	return &Token{
		ID:        uuid.New(),
		UserID:    userID,
		Platform:  platform,
		Token:     token,
		LastSeen:  now,
		CreatedAt: now,
	}, nil
}

// Touch refreshes LastSeen — used when the same (platform, token) re-registers
// so the repository can prune stale rows by age.
func (t *Token) Touch() { t.LastSeen = time.Now().UTC() }
