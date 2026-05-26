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

// TargetType is what a report is filed against.
type TargetType string

const (
	// TargetListing is a property listing.
	TargetListing TargetType = "listing"
	// TargetReview is a posted review.
	TargetReview TargetType = "review"
)

// Valid reports whether the target type is one of the known values.
func (t TargetType) Valid() bool {
	return t == TargetListing || t == TargetReview
}

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

// Report is the aggregate root for a single moderation report. A report always
// carries the relevant listing in PropertyID (for a review report this is the
// listing the review is about), so the moderation queue can show the listing
// title regardless of target type. TargetType/TargetID identify what was
// actually reported.
type Report struct {
	ID         uuid.UUID
	TargetType TargetType
	TargetID   uuid.UUID
	PropertyID uuid.UUID // the listing involved (the target itself for listing reports)
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

// NewReport creates an open report against a property listing.
func NewReport(propertyID, reporterID uuid.UUID, reason Reason, note string) (*Report, error) {
	if propertyID == uuid.Nil {
		return nil, shared.NewValidationError("a report requires a listing")
	}
	return newReport(TargetListing, propertyID, propertyID, reporterID, reason, note)
}

// NewReviewReport creates an open report against a review. propertyID is the
// listing the review belongs to, recorded so moderators see which listing it
// concerns.
func NewReviewReport(reviewID, propertyID, reporterID uuid.UUID, reason Reason, note string) (*Report, error) {
	if reviewID == uuid.Nil {
		return nil, shared.NewValidationError("a report requires a review")
	}
	return newReport(TargetReview, reviewID, propertyID, reporterID, reason, note)
}

func newReport(targetType TargetType, targetID, propertyID, reporterID uuid.UUID, reason Reason, note string) (*Report, error) {
	note = strings.TrimSpace(note)
	if !targetType.Valid() {
		return nil, shared.NewValidationError("unsupported report target")
	}
	if targetID == uuid.Nil {
		return nil, shared.NewValidationError("a report requires a target")
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
		TargetType: targetType,
		TargetID:   targetID,
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
