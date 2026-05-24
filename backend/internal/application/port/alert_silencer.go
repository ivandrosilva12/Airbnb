package port

import (
	"context"
	"time"
)

// SilenceMatcher selects which alerts a silence applies to. It mirrors an
// Alertmanager matcher: Name/Value with optional regex and negation. IsEqual
// defaults to true (an equality match); set it false for a "not-equal" matcher.
type SilenceMatcher struct {
	Name    string
	Value   string
	IsRegex bool
	IsEqual bool
}

// NewSilence is the input for creating a silence (a maintenance window that
// mutes matching alerts between StartsAt and EndsAt).
type NewSilence struct {
	Matchers  []SilenceMatcher
	StartsAt  time.Time
	EndsAt    time.Time
	CreatedBy string
	Comment   string
}

// Silence is an existing silence as reported by the upstream alert manager.
type Silence struct {
	ID        string
	Matchers  []SilenceMatcher
	StartsAt  time.Time
	EndsAt    time.Time
	CreatedBy string
	Comment   string
	Status    string // "active", "pending", or "expired"
}

// AlertSilencer is an outbound port for managing alert silences in the upstream
// alert manager (Alertmanager). The application depends on it; infrastructure
// provides the implementation (an Alertmanager REST client, or a disabled
// no-op when no manager is configured).
type AlertSilencer interface {
	// Create registers a silence and returns its id.
	Create(ctx context.Context, s NewSilence) (string, error)
	// List returns the current silences (active, pending and recently expired).
	List(ctx context.Context) ([]Silence, error)
	// Delete expires (removes) the silence with the given id.
	Delete(ctx context.Context, id string) error
}
