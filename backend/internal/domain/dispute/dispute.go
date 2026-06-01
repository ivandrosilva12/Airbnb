// Package dispute is the bounded context for the Resolution Center. A Dispute
// is an after-stay claim attached to a Booking: a guest may ask for a partial
// refund (clean issue, broken amenity), a host may file a damage claim, or
// either side may raise an "other" issue for an admin to mediate. The
// aggregate models the lifecycle (open → under_review → resolved|rejected) and
// keeps a child list of Evidence that either side or the moderator can append.
//
// The actual money movement (partial refund, damage debit) is intentionally
// out of scope here — admin resolution records a text decision; S14 will plug
// the payment effects in once the dispute lifecycle is stable.
package dispute

import (
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Kind classifies what the opener is claiming.
type Kind string

const (
	KindRefund Kind = "refund" // guest asks for money back (cleanliness, misrepresented listing, …)
	KindDamage Kind = "damage" // host claims compensation for damage caused during the stay
	KindOther  Kind = "other"  // qualitative complaint without a money figure
)

// Valid reports whether the kind is one of the known values.
func (k Kind) Valid() bool {
	switch k {
	case KindRefund, KindDamage, KindOther:
		return true
	default:
		return false
	}
}

// Status is where the dispute sits in the moderation flow.
type Status string

const (
	StatusOpen        Status = "open"         // just filed by guest/host
	StatusUnderReview Status = "under_review" // a moderator has picked it up
	StatusResolved    Status = "resolved"     // moderator sided with the opener
	StatusRejected    Status = "rejected"     // moderator dismissed the claim
)

// IsTerminal reports whether the dispute is closed (no more state changes).
func (s Status) IsTerminal() bool {
	return s == StatusResolved || s == StatusRejected
}

// Evidence is a single piece of supporting material attached to a dispute.
// URL is optional (uploads happen out-of-band via the storage port; we keep
// only the public URL); Note carries free text from the contributor.
type Evidence struct {
	ID      uuid.UUID
	URL     string
	Note    string
	AddedBy uuid.UUID
	AddedAt time.Time
}

// Dispute is the aggregate root.
type Dispute struct {
	ID                   uuid.UUID
	BookingID            uuid.UUID
	OpenerID             uuid.UUID
	Kind                 Kind
	Reason               string
	RequestedAmountCents int64
	Currency             string
	Status               Status
	HostResponse         string
	Resolution           string
	AdminID              uuid.UUID
	Evidence             []Evidence
	OpenedAt             time.Time
	DecidedAt            *time.Time
	UpdatedAt            time.Time
}

// New constructs an Open dispute and enforces invariants. The application layer
// must additionally check that the booking exists, that opener is guest/host
// of the booking, that the booking is completed/cancelled, and that the
// post-stay window (e.g. 14 days) is still open.
func New(bookingID, openerID uuid.UUID, kind Kind, reason string, requestedAmountCents int64, currency string) (*Dispute, error) {
	if bookingID == uuid.Nil {
		return nil, shared.NewValidationError("booking is required")
	}
	if openerID == uuid.Nil {
		return nil, shared.NewValidationError("opener is required")
	}
	if !kind.Valid() {
		return nil, shared.NewValidationError("unknown dispute kind")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, shared.NewValidationError("reason is required")
	}
	if len(reason) > 2000 {
		return nil, shared.NewValidationError("reason is too long")
	}
	if kind == KindRefund || kind == KindDamage {
		if requestedAmountCents <= 0 {
			return nil, shared.NewValidationError("a positive amount is required for refund/damage disputes")
		}
		currency = strings.ToUpper(strings.TrimSpace(currency))
		if currency == "" {
			return nil, shared.NewValidationError("currency is required for refund/damage disputes")
		}
	} else {
		// "other" disputes have no money figure; normalise.
		requestedAmountCents = 0
		currency = strings.ToUpper(strings.TrimSpace(currency))
	}
	now := time.Now().UTC()
	return &Dispute{
		ID:                   uuid.New(),
		BookingID:            bookingID,
		OpenerID:             openerID,
		Kind:                 kind,
		Reason:               reason,
		RequestedAmountCents: requestedAmountCents,
		Currency:             currency,
		Status:               StatusOpen,
		OpenedAt:             now,
		UpdatedAt:            now,
	}, nil
}

// AddEvidence appends a piece of supporting material. Either side or the
// moderator may contribute. Adding evidence is allowed until the dispute is
// terminal so a moderator can still pin the decision rationale.
func (d *Dispute) AddEvidence(addedBy uuid.UUID, url, note string) (Evidence, error) {
	if d.Status.IsTerminal() {
		return Evidence{}, shared.NewValidationError("cannot add evidence to a closed dispute")
	}
	if addedBy == uuid.Nil {
		return Evidence{}, shared.NewValidationError("contributor is required")
	}
	url = strings.TrimSpace(url)
	note = strings.TrimSpace(note)
	if url == "" && note == "" {
		return Evidence{}, shared.NewValidationError("evidence requires a URL or a note")
	}
	ev := Evidence{
		ID:      uuid.New(),
		URL:     url,
		Note:    note,
		AddedBy: addedBy,
		AddedAt: time.Now().UTC(),
	}
	d.Evidence = append(d.Evidence, ev)
	d.touch()
	return ev, nil
}

// HostRespond records the host's side of the story. Allowed while the dispute
// is still open or under review; idempotent overwrites are accepted so the
// host can refine their statement.
func (d *Dispute) HostRespond(response string) error {
	if d.Status.IsTerminal() {
		return shared.NewValidationError("dispute is already closed")
	}
	response = strings.TrimSpace(response)
	if response == "" {
		return shared.NewValidationError("response is required")
	}
	if len(response) > 2000 {
		return shared.NewValidationError("response is too long")
	}
	d.HostResponse = response
	if d.Status == StatusOpen {
		d.Status = StatusUnderReview
	}
	d.touch()
	return nil
}

// AdminTakeOver marks the dispute as under_review when the moderator has
// picked it up but not yet decided. Idempotent.
func (d *Dispute) AdminTakeOver(adminID uuid.UUID) error {
	if d.Status.IsTerminal() {
		return shared.NewValidationError("dispute is already closed")
	}
	if adminID == uuid.Nil {
		return shared.NewValidationError("admin id is required")
	}
	d.AdminID = adminID
	if d.Status == StatusOpen {
		d.Status = StatusUnderReview
	}
	d.touch()
	return nil
}

// AdminResolve closes the dispute siding (at least partially) with the opener.
// The resolution string is the public decision text.
func (d *Dispute) AdminResolve(adminID uuid.UUID, resolution string) error {
	return d.decide(adminID, resolution, StatusResolved)
}

// AdminReject closes the dispute siding against the opener (no compensation /
// no further action).
func (d *Dispute) AdminReject(adminID uuid.UUID, resolution string) error {
	return d.decide(adminID, resolution, StatusRejected)
}

func (d *Dispute) decide(adminID uuid.UUID, resolution string, target Status) error {
	if d.Status.IsTerminal() {
		return shared.NewValidationError("dispute is already closed")
	}
	if adminID == uuid.Nil {
		return shared.NewValidationError("admin id is required")
	}
	resolution = strings.TrimSpace(resolution)
	if resolution == "" {
		return shared.NewValidationError("resolution text is required")
	}
	now := time.Now().UTC()
	d.AdminID = adminID
	d.Resolution = resolution
	d.Status = target
	d.DecidedAt = &now
	d.touch()
	return nil
}

func (d *Dispute) touch() { d.UpdatedAt = time.Now().UTC() }
