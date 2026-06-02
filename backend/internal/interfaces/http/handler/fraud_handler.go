package handler

import (
	"errors"
	"net/http"
	"strconv"

	fraudapp "github.com/airhost/backend/internal/application/fraud"
	"github.com/airhost/backend/internal/domain/fraud"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// FraudHandler exposes admin-only reads on the fraud assessment trail
// (S68). No write endpoints — assessments are produced by the booking
// pipeline as a side-effect of Create, not by direct admin action.
type FraudHandler struct {
	svc *fraudapp.Service
}

// NewFraudHandler builds a FraudHandler.
func NewFraudHandler(svc *fraudapp.Service) *FraudHandler {
	return &FraudHandler{svc: svc}
}

// AdminList returns assessments filtered by level. The default level
// floor is `low` (i.e. everything); admins typically pass `?level=high`
// to see the review queue.
//
//	GET /api/v1/admin/fraud/assessments?level=high&limit=50&offset=0
func (h *FraudHandler) AdminList(c *gin.Context) {
	filter := fraud.ListFilter{}
	if lv := c.Query("level"); lv != "" {
		l := fraud.Level(lv)
		if !l.Valid() {
			response.FailMessage(c, http.StatusBadRequest, "level must be low|medium|high")
			return
		}
		filter.MinLevel = l
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	page := shared.NewPage(limit, offset)
	res, err := h.svc.List(c.Request.Context(), filter, page)
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.FraudAssessmentView, 0, len(res.Items))
	for _, a := range res.Items {
		items = append(items, dto.FromFraudAssessment(a))
	}
	response.OK(c, gin.H{"items": items, "total": res.Total})
}

// AdminGetByBooking returns the assessment for a specific booking, or
// 404 when there is none. Useful when the admin booking-detail page
// wants to surface the score and signal list without scanning the
// global list.
//
//	GET /api/v1/admin/fraud/assessments/by-booking/:bookingId
func (h *FraudHandler) AdminGetByBooking(c *gin.Context) {
	bookingID, ok := parseUUID(c, c.Param("bookingId"), "bookingId")
	if !ok {
		return
	}
	a, err := h.svc.GetByBookingID(c.Request.Context(), bookingID)
	if err != nil {
		if errors.Is(err, shared.ErrNotFound) {
			response.FailMessage(c, http.StatusNotFound, "no assessment for that booking")
			return
		}
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromFraudAssessment(a))
}
