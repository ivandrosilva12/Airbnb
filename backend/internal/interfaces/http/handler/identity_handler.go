package handler

import (
	"errors"

	identityapp "github.com/airhost/backend/internal/application/identity"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// IdentityHandler exposes KYC identity-verification endpoints (user-facing
// submit/status plus the administrator review queue).
type IdentityHandler struct {
	svc *identityapp.Service
}

// NewIdentityHandler builds an IdentityHandler.
func NewIdentityHandler(svc *identityapp.Service) *IdentityHandler {
	return &IdentityHandler{svc: svc}
}

type submitVerificationRequest struct {
	DocumentType string `json:"documentType" binding:"required"`
	DocumentRef  string `json:"documentRef" binding:"required"`
	LegalName    string `json:"legalName" binding:"required"`
}

// Submit files the authenticated user's identity-verification request.
func (h *IdentityHandler) Submit(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}
	var req submitVerificationRequest
	if !bindJSON(c, &req) {
		return
	}
	v, err := h.svc.Submit(c.Request.Context(), userID, identityapp.SubmitInput{
		DocumentType: req.DocumentType,
		DocumentRef:  req.DocumentRef,
		LegalName:    req.LegalName,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, dto.FromVerification(v))
}

// GetMine returns the authenticated user's latest verification. When the user
// has never submitted one it returns 200 with a null body so clients can render
// an "unverified" state without treating it as an error.
func (h *IdentityHandler) GetMine(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}
	v, err := h.svc.GetMine(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, shared.ErrNotFound) {
			response.OK(c, gin.H{"verification": nil})
			return
		}
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"verification": dto.FromVerification(v)})
}

// ListPending returns the administrator review queue.
func (h *IdentityHandler) ListPending(c *gin.Context) {
	res, err := h.svc.ListPending(c.Request.Context(), pageFromQuery(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.VerificationView, 0, len(res.Items))
	for _, v := range res.Items {
		items = append(items, dto.FromVerification(v))
	}
	response.OK(c, dto.PageView[dto.VerificationView]{Items: items, Total: res.Total})
}

// Approve marks a pending verification approved (administrator action).
func (h *IdentityHandler) Approve(c *gin.Context) {
	adminID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	v, err := h.svc.Approve(c.Request.Context(), adminID, id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromVerification(v))
}

type rejectVerificationRequest struct {
	Reason string `json:"reason" binding:"required"`
}

// Reject refuses a pending verification with a reason (administrator action).
func (h *IdentityHandler) Reject(c *gin.Context) {
	adminID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	var req rejectVerificationRequest
	if !bindJSON(c, &req) {
		return
	}
	v, err := h.svc.Reject(c.Request.Context(), adminID, id, req.Reason)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromVerification(v))
}
