// Package shared contains the domain shared kernel: errors and value objects
// reused across multiple bounded contexts.
package shared

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Sentinel domain errors. Application and interface layers translate these
// into transport-specific responses (e.g. HTTP status codes).
var (
	// ErrNotFound indicates an aggregate could not be located.
	ErrNotFound = errors.New("resource not found")
	// ErrConflict indicates a uniqueness or state conflict.
	ErrConflict = errors.New("resource conflict")
	// ErrValidation indicates invariant or input validation failure.
	ErrValidation = errors.New("validation failed")
	// ErrForbidden indicates the actor may not perform the operation.
	ErrForbidden = errors.New("operation forbidden")
	// ErrKYCStepUpRequired indicates the request is blocked because the
	// actor's identity is not verified AND the request exceeds a policy
	// threshold (e.g. a high-value booking). It satisfies errors.Is for
	// both itself and ErrValidation so it maps to 422 by default — the
	// transport layer can detect this specific sentinel to surface a
	// distinct error code and trigger a verification prompt.
	ErrKYCStepUpRequired = errors.New("identity verification required")
	// ErrPayoutOnboardingIncomplete signals that a booking cannot be
	// confirmed because the host has not yet finished their payout
	// (Stripe Connect) onboarding. It satisfies errors.Is for ErrConflict
	// so the transport layer maps it to HTTP 409 by default — it is the
	// host's fault, not a guest input validation failure. Surfaced
	// concretely via PayoutOnboardingIncompleteError so the client can
	// render a host-id-specific nudge.
	ErrPayoutOnboardingIncomplete = errors.New("host payout onboarding incomplete")
)

// ValidationError carries a human-readable message while still matching
// ErrValidation through errors.Is.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

func (e *ValidationError) Is(target error) bool { return target == ErrValidation }

// NewValidationError builds a ValidationError.
func NewValidationError(msg string) error { return &ValidationError{Message: msg} }

// KYCStepUpError carries the threshold that triggered a step-up requirement.
// It satisfies errors.Is for both ErrKYCStepUpRequired and ErrValidation, so
// callers that don't care about the typed variant still see a validation
// failure. The transport layer (response.Fail) checks for it via errors.As
// to populate a structured details payload the client can render.
type KYCStepUpError struct {
	// ThresholdCents is the policy threshold the booking total reached or
	// exceeded, in the same currency as Currency.
	ThresholdCents int64
	// Currency is the ISO-4217 code of ThresholdCents (e.g. "EUR").
	Currency string
}

func (e *KYCStepUpError) Error() string {
	return fmt.Sprintf("identity verification required for bookings of %d %s or more",
		e.ThresholdCents/100, e.Currency)
}

func (e *KYCStepUpError) Is(target error) bool {
	return target == ErrKYCStepUpRequired || target == ErrValidation
}

// NewKYCStepUpError builds a KYCStepUpError.
func NewKYCStepUpError(thresholdCents int64, currency string) error {
	return &KYCStepUpError{ThresholdCents: thresholdCents, Currency: currency}
}

// PayoutOnboardingIncompleteError indicates a booking cannot transition to
// confirmed because the listing's host has not finished payout onboarding —
// the platform would capture funds with no rail to disburse them. It
// satisfies errors.Is for ErrPayoutOnboardingIncomplete and ErrConflict so
// the transport layer maps it to HTTP 409 (it is the host's situation, not
// a guest input validation problem).
//
// OnboardingURL is optional — when set, it is the host-facing dashboard or
// hosted onboarding link the client can surface so the host can resume the
// flow. Today the booking gate doesn't synthesise it (the gate decision and
// the link-creation call live in different services), but the field is part
// of the typed error so we can wire it through later without breaking the
// HTTP contract.
type PayoutOnboardingIncompleteError struct {
	// HostID is the listing owner whose onboarding is incomplete. Surfaced
	// in the HTTP details payload so admin tooling can deep-link.
	HostID uuid.UUID
	// OnboardingURL, when non-empty, is a host-facing link to resume
	// onboarding. Optional.
	OnboardingURL string
}

func (e *PayoutOnboardingIncompleteError) Error() string {
	return fmt.Sprintf("host %s has not completed payout onboarding", e.HostID)
}

func (e *PayoutOnboardingIncompleteError) Is(target error) bool {
	return target == ErrPayoutOnboardingIncomplete || target == ErrConflict
}

// NewPayoutOnboardingIncompleteError builds a PayoutOnboardingIncompleteError
// with no onboarding URL set (the most common construction site — the
// booking gate doesn't have one handy).
func NewPayoutOnboardingIncompleteError(hostID uuid.UUID) error {
	return &PayoutOnboardingIncompleteError{HostID: hostID}
}
