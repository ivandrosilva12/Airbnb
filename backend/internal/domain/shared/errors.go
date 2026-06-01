// Package shared contains the domain shared kernel: errors and value objects
// reused across multiple bounded contexts.
package shared

import (
	"errors"
	"fmt"
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
