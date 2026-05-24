// Package identity is the bounded context for KYC identity verification. A user
// submits a government identity document for review; a platform administrator
// approves or rejects it. Authentication itself stays with Keycloak — this
// context only records the trust decision so the platform can surface a
// "verified" badge and gate sensitive actions.
package identity

import (
	"strings"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Status is the lifecycle state of a verification request.
type Status string

const (
	// StatusPending is awaiting an administrator's decision.
	StatusPending Status = "pending"
	// StatusApproved means the identity was confirmed.
	StatusApproved Status = "approved"
	// StatusRejected means the submission was refused; the user may resubmit.
	StatusRejected Status = "rejected"
)

// DocumentType enumerates the accepted identity documents.
type DocumentType string

const (
	DocPassport      DocumentType = "passport"
	DocIDCard        DocumentType = "id_card"
	DocDriverLicense DocumentType = "driver_license"
)

// Valid reports whether the document type is one of the known values.
func (d DocumentType) Valid() bool {
	switch d {
	case DocPassport, DocIDCard, DocDriverLicense:
		return true
	default:
		return false
	}
}

// Verification is the aggregate root for a single identity-verification request.
//
// DocumentRef is a reference to the submitted document (in production this is an
// object-storage key for an uploaded scan held by the KYC provider, never raw
// PII such as a full document number). LegalName is the name as printed on the
// document, used by the reviewer to corroborate the submission.
type Verification struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	Status          Status
	DocumentType    DocumentType
	DocumentRef     string
	LegalName       string
	RejectionReason string
	ReviewerID      uuid.UUID // administrator who decided; Nil while pending
	ReviewedAt      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// NewVerification creates a pending verification, enforcing invariants.
func NewVerification(userID uuid.UUID, docType DocumentType, documentRef, legalName string) (*Verification, error) {
	documentRef = strings.TrimSpace(documentRef)
	legalName = strings.TrimSpace(legalName)

	if userID == uuid.Nil {
		return nil, shared.NewValidationError("verification requires a user")
	}
	if !docType.Valid() {
		return nil, shared.NewValidationError("unsupported document type")
	}
	if documentRef == "" {
		return nil, shared.NewValidationError("a document reference is required")
	}
	if legalName == "" {
		return nil, shared.NewValidationError("the legal name on the document is required")
	}
	if len(legalName) > 200 {
		return nil, shared.NewValidationError("legal name is too long")
	}

	now := time.Now().UTC()
	return &Verification{
		ID:           uuid.New(),
		UserID:       userID,
		Status:       StatusPending,
		DocumentType: docType,
		DocumentRef:  documentRef,
		LegalName:    legalName,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

// IsPending reports whether the request still awaits a decision.
func (v *Verification) IsPending() bool { return v.Status == StatusPending }

// Approve records an administrator's approval. Only a pending request can be
// decided.
func (v *Verification) Approve(reviewerID uuid.UUID) error {
	if !v.IsPending() {
		return shared.NewValidationError("only a pending verification can be approved")
	}
	v.Status = StatusApproved
	v.RejectionReason = ""
	v.stampReview(reviewerID)
	return nil
}

// Reject records an administrator's refusal with a reason. Only a pending
// request can be decided.
func (v *Verification) Reject(reviewerID uuid.UUID, reason string) error {
	if !v.IsPending() {
		return shared.NewValidationError("only a pending verification can be rejected")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return shared.NewValidationError("a rejection reason is required")
	}
	if len(reason) > 500 {
		return shared.NewValidationError("rejection reason is too long")
	}
	v.Status = StatusRejected
	v.RejectionReason = reason
	v.stampReview(reviewerID)
	return nil
}

func (v *Verification) stampReview(reviewerID uuid.UUID) {
	now := time.Now().UTC()
	v.ReviewerID = reviewerID
	v.ReviewedAt = &now
	v.UpdatedAt = now
}
