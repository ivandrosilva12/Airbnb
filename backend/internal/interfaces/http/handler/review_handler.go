package handler

import (
	reviewapp "github.com/airhost/backend/internal/application/review"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// ReviewHandler exposes review endpoints.
type ReviewHandler struct {
	svc *reviewapp.Service
}

// NewReviewHandler builds a ReviewHandler.
func NewReviewHandler(svc *reviewapp.Service) *ReviewHandler { return &ReviewHandler{svc: svc} }

type createReviewRequest struct {
	BookingID string `json:"bookingId" binding:"required"`
	Rating    int    `json:"rating" binding:"required"`
	Comment   string `json:"comment"`
}

// Create publishes a review for a completed stay.
func (h *ReviewHandler) Create(c *gin.Context) {
	guestID, ok := requireUser(c)
	if !ok {
		return
	}
	var req createReviewRequest
	if !bindJSON(c, &req) {
		return
	}
	bookingID, ok := parseUUID(c, req.BookingID, "bookingId")
	if !ok {
		return
	}
	r, err := h.svc.Create(c.Request.Context(), reviewapp.CreateInput{
		GuestID:   guestID,
		BookingID: bookingID,
		Rating:    req.Rating,
		Comment:   req.Comment,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, dto.FromReview(r))
}

// CreateGuest publishes the host's review of the guest for a completed booking.
func (h *ReviewHandler) CreateGuest(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	var req createReviewRequest
	if !bindJSON(c, &req) {
		return
	}
	bookingID, ok := parseUUID(c, req.BookingID, "bookingId")
	if !ok {
		return
	}
	r, err := h.svc.CreateGuestReview(c.Request.Context(), reviewapp.GuestReviewInput{
		HostID:    hostID,
		BookingID: bookingID,
		Rating:    req.Rating,
		Comment:   req.Comment,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, dto.FromReview(r))
}

// MyGuestReviews returns reviews hosts have written about the authenticated user
// as a guest, plus the aggregate summary.
func (h *ReviewHandler) MyGuestReviews(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}
	res, err := h.svc.ListAboutGuest(c.Request.Context(), userID, pageFromQuery(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	summary, err := h.svc.SummaryForGuest(c.Request.Context(), userID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.ReviewView, 0, len(res.Items))
	for _, r := range res.Items {
		items = append(items, dto.FromReview(r))
	}
	response.OK(c, gin.H{
		"items":   items,
		"total":   res.Total,
		"summary": dto.FromReviewSummary(summary),
	})
}

// MyPendingReviews returns the authenticated guest's completed stays that still
// await a property review — the data behind the post-stay review prompt.
func (h *ReviewHandler) MyPendingReviews(c *gin.Context) {
	guestID, ok := requireUser(c)
	if !ok {
		return
	}
	pending, err := h.svc.PendingForGuest(c.Request.Context(), guestID, pageFromQuery(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.PendingReviewView, 0, len(pending))
	for _, p := range pending {
		items = append(items, dto.FromPendingReview(p))
	}
	response.OK(c, gin.H{"items": items})
}

// ListForProperty returns reviews for a property.
func (h *ReviewHandler) ListForProperty(c *gin.Context) {
	propertyID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	res, err := h.svc.ListByProperty(c.Request.Context(), propertyID, pageFromQuery(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.ReviewView, 0, len(res.Items))
	for _, r := range res.Items {
		items = append(items, dto.FromReview(r))
	}
	response.OK(c, dto.PageView[dto.ReviewView]{Items: items, Total: res.Total})
}

// Summary returns aggregate rating stats for a property.
func (h *ReviewHandler) Summary(c *gin.Context) {
	propertyID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	s, err := h.svc.Summary(c.Request.Context(), propertyID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromReviewSummary(s))
}
