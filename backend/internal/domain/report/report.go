// Package report is the bounded context for listing/abuse reports: a user flags
// a property listing for review, and a platform administrator resolves or
// dismisses the report from a moderation queue.
package report

import (
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Status is the lifecycle state of a report.
type Status string

const (
	// StatusOpen is awaiting moderation.
	StatusOpen Status = "open"
	// StatusResolved means the report was acted upon (e.g. the listing was
	// suspended or corrected).
	StatusResolved Status = "resolved"
	// StatusDismissed means the report required no action.
	StatusDismissed Status = "dismissed"
)

// Reason categorises why a listing was reported.
type Reason string

const (
	ReasonSpam          Reason = "spam"
	ReasonInappropriate Reason = "inappropriate"
	ReasonScam          Reason = "scam"
	ReasonInaccurate    Reason = "inaccurate"
	ReasonOther         Reason = "other"
)

// Valid reports whether the reason is one of the known values.
func (r Reason) Valid() bool {
	switch r {
	case ReasonSpam, ReasonInappropriate, ReasonScam, ReasonInaccurate, ReasonOther:
		return true
	default:
		return false
	}
}

// Report is the aggregate root for a single listing report.
type Report struct {
	ID         uuid.UUID
	PropertyID uuid.UUID
	ReporterID uuid.UUID
	Reason     Reason
	Note       string
	Status     Status
	Resolution string    // administrator note recorded on resolve/dismiss
	ResolverID uuid.UUID // administrator who decided; Nil while open
	ResolvedAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// NewReport creates an open report, enforcing invariants.
func NewReport(propertyID, reporterID uuid.UUID, reason Reason, note string) (*Report, error) {
	note = strings.TrimSpace(note)
	if propertyID == uuid.Nil {
		return nil, shared.NewValidationError("a report requires a listing")
	}
	if reporterID == uuid.Nil {
		return nil, shared.NewValidationError("a report requires a reporter")
	}
	if !reason.Valid() {
		return nil, shared.NewValidationError("unsupported report reason")
	}
	if len(note) > 1000 {
		return nil, shared.NewValidationError("note is too long")
	}
	now := time.Now().UTC()
	return &Report{
		ID:         uuid.New(),
		PropertyID: propertyID,
		ReporterID: reporterID,
		Reason:     reason,
		Note:       note,
		Status:     StatusOpen,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

// IsOpen reports whether the report still awaits moderation.
func (r *Report) IsOpen() bool { return r.Status == StatusOpen }

// Resolve marks an open report acted-upon (administrator action).
func (r *Report) Resolve(adminID uuid.UUID, resolution string) error {
	return r.decide(StatusResolved, adminID, resolution)
}

// Dismiss marks an open report as requiring no action (administrator action).
func (r *Report) Dismiss(adminID uuid.UUID, resolution string) error {
	return r.decide(StatusDismissed, adminID, resolution)
}

func (r *Report) decide(status Status, adminID uuid.UUID, resolution string) error {
	if !r.IsOpen() {
		return shared.NewValidationError("only an open report can be moderated")
	}
	resolution = strings.TrimSpace(resolution)
	if len(resolution) > 1000 {
		return shared.NewValidationError("resolution note is too long")
	}
	now := time.Now().UTC()
	r.Status = status
	r.Resolution = resolution
	r.ResolverID = adminID
	r.ResolvedAt = &now
	r.UpdatedAt = now
	return nil
}
