// Package splitpayment is the bounded context for splitting a booking's cost
// across multiple travellers. A SplitPayment is created alongside a booking;
// each Share represents one traveller's portion of the total. The booking
// confirms only when every share is paid.
//
// Constraint for this slice: a SplitPayment is only valid against an
// instant-book listing, so the split completion can confirm the booking
// without requiring a host decision afterwards. Each share is currently
// authorised in trust mode (the payer signs off; no real gateway capture)
// because the per-share gateway integration is a follow-up.
package splitpayment

import (
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Status tracks the SplitPayment lifecycle.
type Status string

const (
	StatusPending   Status = "pending"   // some shares still unpaid
	StatusCompleted Status = "completed" // every share is paid; booking can confirm
	StatusCancelled Status = "cancelled" // organizer (or the booking flow) called it off
)

// ShareStatus tracks one traveller's portion.
type ShareStatus string

const (
	SharePending  ShareStatus = "pending"  // invited, payer hasn't authorised yet
	SharePaid     ShareStatus = "paid"     // payer signed off — gateway hold placed, ref stored
	ShareFailed   ShareStatus = "failed"   // gateway refused the per-share authorize; payer can retry
	ShareRefunded ShareStatus = "refunded" // booking cancelled — the previously-authorized hold has been released
)

// Share is the value object representing one traveller's portion.
//
// GatewayRef is the per-share transaction reference returned by the payment
// gateway when the share's hold was authorized (WF-GAP-001). It is nil while
// the share is still pending (the per-share authorize has not been issued
// yet) and on failed/refunded shares whose gateway side has been released.
//
// FailureReason carries the gateway's rejection text when a per-share
// authorize fails — surfaced for operator triage; not shown to payers.
type Share struct {
	ID             uuid.UUID
	SplitPaymentID uuid.UUID
	// PayerEmail is the invited address (case-folded). The payer must
	// authenticate as a user whose email matches before authorising.
	PayerEmail string
	// PayerUserID is set once the share is authorised, recording who
	// actually paid. nil while the share is still pending.
	PayerUserID   *uuid.UUID
	AmountCents   int64
	Status        ShareStatus
	GatewayRef    *string // S88 — set after a successful per-share gateway authorize
	FailureReason *string // S88 — set when the gateway refuses authorize/refund
	PaidAt        *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// SplitPayment is the aggregate root: the per-booking split plan and its
// shares.
type SplitPayment struct {
	ID          uuid.UUID
	BookingID   uuid.UUID
	OrganizerID uuid.UUID
	Currency    string
	TotalCents  int64
	Status      Status
	Shares      []Share
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CompletedAt *time.Time
	CancelledAt *time.Time
}

// ShareInput is the data needed to seed one share at creation time.
type ShareInput struct {
	Email       string
	AmountCents int64
}

// New builds a SplitPayment with validated invariants:
//   - At least two shares (a one-payer split is just a regular booking)
//   - Every share AmountCents > 0
//   - Sum of shares == totalCents exactly (no rounding slop — the caller
//     must distribute any remainder to one share)
//   - The organizer's own email MUST appear in the shares (an organizer
//     cannot push the entire cost onto invitees)
//   - Currency non-empty
func New(bookingID, organizerID uuid.UUID, organizerEmail, currency string, totalCents int64, shares []ShareInput) (*SplitPayment, error) {
	if bookingID == uuid.Nil {
		return nil, shared.NewValidationError("bookingID is required")
	}
	if organizerID == uuid.Nil {
		return nil, shared.NewValidationError("organizerID is required")
	}
	currency = strings.TrimSpace(strings.ToUpper(currency))
	if currency == "" {
		return nil, shared.NewValidationError("currency is required")
	}
	if totalCents <= 0 {
		return nil, shared.NewValidationError("totalCents must be positive")
	}
	if len(shares) < 2 {
		return nil, shared.NewValidationError("a split needs at least two shares")
	}
	organizerEmail = strings.TrimSpace(strings.ToLower(organizerEmail))
	if organizerEmail == "" {
		return nil, shared.NewValidationError("organizer email is required")
	}

	var sum int64
	emails := make(map[string]struct{}, len(shares))
	out := make([]Share, 0, len(shares))
	now := time.Now().UTC()
	splitID := uuid.New()
	organizerIncluded := false
	for _, s := range shares {
		email := strings.TrimSpace(strings.ToLower(s.Email))
		if email == "" {
			return nil, shared.NewValidationError("each share needs a payer email")
		}
		if _, dup := emails[email]; dup {
			return nil, shared.NewValidationError("each payer email must appear at most once: " + email)
		}
		emails[email] = struct{}{}
		if s.AmountCents <= 0 {
			return nil, shared.NewValidationError("each share amount must be positive")
		}
		sum += s.AmountCents
		if email == organizerEmail {
			organizerIncluded = true
		}
		out = append(out, Share{
			ID:             uuid.New(),
			SplitPaymentID: splitID,
			PayerEmail:     email,
			AmountCents:    s.AmountCents,
			Status:         SharePending,
			CreatedAt:      now,
			UpdatedAt:      now,
		})
	}
	if sum != totalCents {
		return nil, shared.NewValidationError("share amounts must sum exactly to the booking total")
	}
	if !organizerIncluded {
		return nil, shared.NewValidationError("the organizer must be one of the payers")
	}
	return &SplitPayment{
		ID:          splitID,
		BookingID:   bookingID,
		OrganizerID: organizerID,
		Currency:    currency,
		TotalCents:  totalCents,
		Status:      StatusPending,
		Shares:      out,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// MarkSharePaid authorises one share. The payerEmail (case-folded) must match
// the share's invited address; payerUserID records the authenticated user
// who actually paid; gatewayRef is the per-share transaction reference
// returned by the payment gateway (empty in trust-mode tests, recorded
// otherwise so the cancel flow can release exactly that hold).
//
// A share previously in ShareFailed may be retried back to SharePaid — the
// payer's earlier card refusal is not terminal. SharePaid → SharePaid is a
// conflict (already authorized). Returns ErrNotFound if the shareID is
// unknown, ErrConflict on a non-pending split or already-paid share, and
// ErrForbidden if the email does not match the share.
func (sp *SplitPayment) MarkSharePaid(shareID, payerUserID uuid.UUID, payerEmail, gatewayRef string) error {
	if sp.Status != StatusPending {
		return shared.ErrConflict
	}
	payerEmail = strings.TrimSpace(strings.ToLower(payerEmail))
	for i := range sp.Shares {
		if sp.Shares[i].ID != shareID {
			continue
		}
		if sp.Shares[i].Status == SharePaid || sp.Shares[i].Status == ShareRefunded {
			return shared.ErrConflict
		}
		if sp.Shares[i].PayerEmail != payerEmail {
			return shared.ErrForbidden
		}
		now := time.Now().UTC()
		sp.Shares[i].Status = SharePaid
		sp.Shares[i].PaidAt = &now
		sp.Shares[i].PayerUserID = &payerUserID
		sp.Shares[i].FailureReason = nil
		if gatewayRef != "" {
			ref := gatewayRef
			sp.Shares[i].GatewayRef = &ref
		}
		sp.Shares[i].UpdatedAt = now
		sp.UpdatedAt = now
		return nil
	}
	return shared.ErrNotFound
}

// MarkShareFailed records a per-share authorize failure. The payerEmail must
// match the invited address (same authorisation rule as MarkSharePaid). The
// share moves to ShareFailed and the reason is kept for operator triage. A
// failed share does NOT progress the split toward completion; the payer can
// re-attempt later, flipping the share back to SharePaid on success.
func (sp *SplitPayment) MarkShareFailed(shareID, payerUserID uuid.UUID, payerEmail, reason string) error {
	if sp.Status != StatusPending {
		return shared.ErrConflict
	}
	payerEmail = strings.TrimSpace(strings.ToLower(payerEmail))
	for i := range sp.Shares {
		if sp.Shares[i].ID != shareID {
			continue
		}
		if sp.Shares[i].Status == SharePaid || sp.Shares[i].Status == ShareRefunded {
			return shared.ErrConflict
		}
		if sp.Shares[i].PayerEmail != payerEmail {
			return shared.ErrForbidden
		}
		now := time.Now().UTC()
		sp.Shares[i].Status = ShareFailed
		sp.Shares[i].PayerUserID = &payerUserID
		r := reason
		sp.Shares[i].FailureReason = &r
		sp.Shares[i].UpdatedAt = now
		sp.UpdatedAt = now
		return nil
	}
	return shared.ErrNotFound
}

// MarkShareRefunded transitions a previously-paid share to ShareRefunded
// after the gateway-side hold has been released (booking cancelled). A
// share that is not paid is skipped (returns ErrConflict) — there is
// nothing to refund.
func (sp *SplitPayment) MarkShareRefunded(shareID uuid.UUID) error {
	for i := range sp.Shares {
		if sp.Shares[i].ID != shareID {
			continue
		}
		if sp.Shares[i].Status != SharePaid {
			return shared.ErrConflict
		}
		now := time.Now().UTC()
		sp.Shares[i].Status = ShareRefunded
		sp.Shares[i].UpdatedAt = now
		sp.UpdatedAt = now
		return nil
	}
	return shared.ErrNotFound
}

// AllPaid reports whether every share has been authorised. A SplitPayment in
// any state other than StatusPending returns false (a completed/cancelled
// split is no longer driving toward "all paid").
func (sp *SplitPayment) AllPaid() bool {
	if sp.Status != StatusPending {
		return false
	}
	for _, s := range sp.Shares {
		if s.Status != SharePaid {
			return false
		}
	}
	return true
}

// MarkCompleted transitions a pending split with all shares paid to
// completed. Returns ErrConflict if the split isn't pending, ErrValidation
// if not every share is paid yet.
func (sp *SplitPayment) MarkCompleted() error {
	if sp.Status != StatusPending {
		return shared.ErrConflict
	}
	for _, s := range sp.Shares {
		if s.Status != SharePaid {
			return shared.NewValidationError("not every share is paid yet")
		}
	}
	now := time.Now().UTC()
	sp.Status = StatusCompleted
	sp.CompletedAt = &now
	sp.UpdatedAt = now
	return nil
}

// Cancel transitions a pending split to cancelled. Returns ErrConflict if the
// split is already terminal (completed or cancelled).
func (sp *SplitPayment) Cancel() error {
	if sp.Status != StatusPending {
		return shared.ErrConflict
	}
	now := time.Now().UTC()
	sp.Status = StatusCancelled
	sp.CancelledAt = &now
	sp.UpdatedAt = now
	return nil
}

// HasPayer reports whether the given email is one of the share recipients.
// Used by the application layer to authorise reads and writes by a payer
// other than the organizer.
func (sp *SplitPayment) HasPayer(email string) bool {
	email = strings.TrimSpace(strings.ToLower(email))
	for _, s := range sp.Shares {
		if s.PayerEmail == email {
			return true
		}
	}
	return false
}

// FindShare returns the share with the given id, or nil. Read-only — callers
// must not mutate the returned share; use MarkSharePaid for changes.
func (sp *SplitPayment) FindShare(shareID uuid.UUID) *Share {
	for i := range sp.Shares {
		if sp.Shares[i].ID == shareID {
			return &sp.Shares[i]
		}
	}
	return nil
}
