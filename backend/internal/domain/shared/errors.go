// Package shared contains the domain shared kernel: errors and value objects
// reused across multiple bounded contexts.
package shared

import "errors"

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
