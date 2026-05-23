package handler

import (
	"net/http"

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
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailMessage(c, http.StatusBadRequest, err.Error())
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
