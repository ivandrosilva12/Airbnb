package handler

import (
	"net/http"
	"time"

	bookingapp "github.com/airhost/backend/internal/application/booking"
	"github.com/airhost/backend/internal/infrastructure/observability"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// BookingHandler exposes reservation endpoints.
type BookingHandler struct {
	svc     *bookingapp.Service
	metrics *observability.Metrics
}

// NewBookingHandler builds a BookingHandler.
func NewBookingHandler(svc *bookingapp.Service, m *observability.Metrics) *BookingHandler {
	return &BookingHandler{svc: svc, metrics: m}
}

type createBookingRequest struct {
	PropertyID string `json:"propertyId" binding:"required"`
	CheckIn    string `json:"checkIn" binding:"required"`
	CheckOut   string `json:"checkOut" binding:"required"`
	Guests     int    `json:"guests" binding:"required"`
}

// Create makes a reservation for the authenticated guest.
func (h *BookingHandler) Create(c *gin.Context) {
	guestID, ok := requireUser(c)
	if !ok {
		return
	}
	var req createBookingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	propertyID, ok := parseUUID(c, req.PropertyID, "propertyId")
	if !ok {
		return
	}
	checkIn, err := time.Parse("2006-01-02", req.CheckIn)
	if err != nil {
		response.FailMessage(c, http.StatusBadRequest, "checkIn must be YYYY-MM-DD")
		return
	}
	checkOut, err := time.Parse("2006-01-02", req.CheckOut)
	if err != nil {
		response.FailMessage(c, http.StatusBadRequest, "checkOut must be YYYY-MM-DD")
		return
	}
	b, err := h.svc.Create(c.Request.Context(), bookingapp.CreateInput{
		GuestID:    guestID,
		PropertyID: propertyID,
		CheckIn:    checkIn,
		CheckOut:   checkOut,
		Guests:     req.Guests,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	h.metrics.BookingsCreated.Inc()
	response.Created(c, dto.FromBooking(b))
}

// Get returns a single reservation the actor participates in.
func (h *BookingHandler) Get(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	b, err := h.svc.GetByID(c.Request.Context(), actorID, id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromBooking(b))
}

// ListMine returns the authenticated guest's reservations.
func (h *BookingHandler) ListMine(c *gin.Context) {
	guestID, ok := requireUser(c)
	if !ok {
		return
	}
	res, err := h.svc.ListForGuest(c.Request.Context(), guestID, pageFromQuery(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.BookingView, 0, len(res.Items))
	for _, b := range res.Items {
		items = append(items, dto.FromBooking(b))
	}
	response.OK(c, dto.PageView[dto.BookingView]{Items: items, Total: res.Total})
}

// ListForProperty returns reservations for a property the actor hosts.
func (h *BookingHandler) ListForProperty(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	propertyID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	res, err := h.svc.ListForProperty(c.Request.Context(), actorID, propertyID, pageFromQuery(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.BookingView, 0, len(res.Items))
	for _, b := range res.Items {
		items = append(items, dto.FromBooking(b))
	}
	response.OK(c, dto.PageView[dto.BookingView]{Items: items, Total: res.Total})
}

// Confirm confirms a pending booking (host only).
func (h *BookingHandler) Confirm(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	b, err := h.svc.Confirm(c.Request.Context(), actorID, id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromBooking(b))
}

// Cancel cancels a booking (guest or host).
func (h *BookingHandler) Cancel(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	b, err := h.svc.Cancel(c.Request.Context(), actorID, id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromBooking(b))
}
