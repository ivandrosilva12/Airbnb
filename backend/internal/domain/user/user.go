// Package user is the bounded context for application identities. Authentication
// is delegated to Keycloak; this aggregate stores the local profile linked to a
// Keycloak subject and the user's role within the platform.
package user

import (
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Role enumerates platform roles.
type Role string

const (
	RoleGuest Role = "guest"
	RoleHost  Role = "host"
	RoleAdmin Role = "admin"
)

// Valid reports whether the role is one of the known values.
func (r Role) Valid() bool {
	switch r {
	case RoleGuest, RoleHost, RoleAdmin:
		return true
	default:
		return false
	}
}

// User is the aggregate root for an application identity.
type User struct {
	ID            uuid.UUID
	KeycloakSub   string // Keycloak subject (sub claim) — the external identity link
	Email         string
	FullName      string
	Role          Role
	AvatarURL     string
	IsActive      bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// NewUser creates a User aggregate, enforcing invariants.
func NewUser(keycloakSub, email, fullName string, role Role) (*User, error) {
	keycloakSub = strings.TrimSpace(keycloakSub)
	email = strings.TrimSpace(strings.ToLower(email))
	fullName = strings.TrimSpace(fullName)

	if keycloakSub == "" {
		return nil, shared.NewValidationError("keycloak subject is required")
	}
	if !validEmail(email) {
		return nil, shared.NewValidationError("a valid email is required")
	}
	if fullName == "" {
		return nil, shared.NewValidationError("full name is required")
	}
	if !role.Valid() {
		role = RoleGuest
	}

	now := time.Now().UTC()
	return &User{
		ID:          uuid.New(),
		KeycloakSub: keycloakSub,
		Email:       email,
		FullName:    fullName,
		Role:        role,
		IsActive:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// PromoteToHost upgrades a guest to host so they can publish listings.
func (u *User) PromoteToHost() {
	if u.Role == RoleGuest {
		u.Role = RoleHost
		u.touch()
	}
}

// UpdateProfile changes mutable profile fields with validation.
func (u *User) UpdateProfile(fullName, avatarURL string) error {
	fullName = strings.TrimSpace(fullName)
	if fullName == "" {
		return shared.NewValidationError("full name is required")
	}
	u.FullName = fullName
	u.AvatarURL = strings.TrimSpace(avatarURL)
	u.touch()
	return nil
}

// Deactivate disables the account.
func (u *User) Deactivate() {
	u.IsActive = false
	u.touch()
}

// IsHost reports whether the user can act as a host.
func (u *User) IsHost() bool { return u.Role == RoleHost || u.Role == RoleAdmin }

func (u *User) touch() { u.UpdatedAt = time.Now().UTC() }

func validEmail(email string) bool {
	at := strings.IndexByte(email, '@')
	if at <= 0 || at == len(email)-1 {
		return false
	}
	return strings.IndexByte(email[at+1:], '.') >= 0
}
