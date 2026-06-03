package handler

import (
	"net/http"
	"strconv"
	"time"

	experiencebookingapp "github.com/airhost/backend/internal/application/experiencebooking"
	"github.com/airhost/backend/internal/domain/experiencebooking"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// ExperienceBookingHandler exposes the ExperienceBooking BC endpoints
// (S80). Routes:
//
//	POST   /api/v1/experiences/:id/bookings        — guest creates
//	GET    /api/v1/experience-bookings/me           — guest list
//	GET    /api/v1/experience-bookings/host         — host list
//	GET    /api/v1/experience-bookings/:id          — guest or host read
//	POST   /api/v1/experience-bookings/:id/cancel   — guest or host
//	POST   /api/v1/experience-bookings/:id/confirm  — host only
//	POST   /api/v1/experience-bookings/:id/complete — host only
type ExperienceBookingHandler struct {
	svc *experiencebookingapp.Service
}

// NewExperienceBookingHandler builds the handler.
func NewExperienceBookingHandler(svc *experiencebookingapp.Service) *ExperienceBookingHandler {
	return &ExperienceBookingHandler{svc: svc}
}

type createExperienceBookingRequest struct {
	StartAt time.Time `json:"startAt" binding:"required"`
	Guests  int       `json:"guests" binding:"required"`
}

// Create — POST /experiences/:id/bookings
func (h *ExperienceBookingHandler) Create(c *gin.Context) {
	guestID, ok := requireUser(c)
	if !ok {
		return
	}
	expID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	var req createExperienceBookingRequest
	if !bindJSON(c, &req) {
		return
	}
	b, err := h.svc.Create(c.Request.Context(), experiencebookingapp.CreateInput{
		ExperienceID: expID, GuestID: guestID,
		StartAt: req.StartAt, Guests: req.Guests,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, dto.FromExperienceBooking(b))
}

// Get — GET /experience-bookings/:id
// Visible to the booking's guest or the experience's host.
func (h *ExperienceBookingHandler) Get(c *gin.Context) {
	actor, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	b, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if b.GuestID != actor && b.HostID != actor {
		response.FailMessage(c, http.StatusForbidden, "forbidden")
		return
	}
	response.OK(c, dto.FromExperienceBooking(b))
}

// ListMine — GET /experience-bookings/me
func (h *ExperienceBookingHandler) ListMine(c *gin.Context) {
	guestID, ok := requireUser(c)
	if !ok {
		return
	}
	res, err := h.svc.ListMine(c.Request.Context(), guestID, expBkPage(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	writeExpBkList(c, res)
}

// HostList — GET /experience-bookings/host
func (h *ExperienceBookingHandler) HostList(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	res, err := h.svc.ListForHost(c.Request.Context(), hostID, expBkPage(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	writeExpBkList(c, res)
}

// Cancel — POST /experience-bookings/:id/cancel
func (h *ExperienceBookingHandler) Cancel(c *gin.Context) {
	actor, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	b, err := h.svc.Cancel(c.Request.Context(), actor, id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromExperienceBooking(b))
}

// Confirm — POST /experience-bookings/:id/confirm
func (h *ExperienceBookingHandler) Confirm(c *gin.Context) {
	actor, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	b, err := h.svc.Confirm(c.Request.Context(), actor, id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromExperienceBooking(b))
}

// Complete — POST /experience-bookings/:id/complete
func (h *ExperienceBookingHandler) Complete(c *gin.Context) {
	actor, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	b, err := h.svc.Complete(c.Request.Context(), actor, id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromExperienceBooking(b))
}

// expBkPage applies the list defaults: limit=50 / offset=0.
func expBkPage(c *gin.Context) shared.Page {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	return shared.NewPage(limit, offset)
}

func writeExpBkList(c *gin.Context, res shared.PageResult[*experiencebooking.Booking]) {
	items := make([]dto.ExperienceBookingView, 0, len(res.Items))
	for _, b := range res.Items {
		items = append(items, dto.FromExperienceBooking(b))
	}
	response.OK(c, gin.H{"items": items, "total": res.Total})
}
