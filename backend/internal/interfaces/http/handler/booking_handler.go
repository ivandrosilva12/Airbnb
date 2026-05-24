package handler

import (
	"fmt"
	"net/http"
	"time"

	bookingapp "github.com/airhost/backend/internal/application/booking"
	"github.com/airhost/backend/internal/infrastructure/ical"
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
	if !bindJSON(c, &req) {
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

// Complete marks a stay as completed (host only, after check-out).
func (h *BookingHandler) Complete(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	b, err := h.svc.Complete(c.Request.Context(), actorID, id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromBooking(b))
}

// Availability returns the occupied date ranges for a property. Public read.
// Query: ?from=YYYY-MM-DD&to=YYYY-MM-DD (defaults to today .. +90 days).
func (h *BookingHandler) Availability(c *gin.Context) {
	propertyID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	from := parseDateOr(c.Query("from"), time.Now().UTC())
	to := parseDateOr(c.Query("to"), from.AddDate(0, 0, 90))

	ranges, err := h.svc.Availability(c.Request.Context(), propertyID, from, to)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"propertyId": propertyID, "booked": dto.FromBookedRanges(ranges)})
}

// CalendarICS streams the listing's busy ranges (bookings + host blocks) as an
// iCalendar feed, so external platforms (Airbnb, Google Calendar, …) can
// subscribe and avoid double-booking. Public, like availability.
func (h *BookingHandler) CalendarICS(c *gin.Context) {
	propertyID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	from := time.Now().UTC().Truncate(24 * time.Hour)
	to := from.AddDate(2, 0, 0) // two years of future availability
	ranges, err := h.svc.Availability(c.Request.Context(), propertyID, from, to)
	if err != nil {
		response.Fail(c, err)
		return
	}
	events := make([]ical.Event, 0, len(ranges))
	for _, r := range ranges {
		summary := "Booked"
		if r.Status == "blocked" {
			summary = "Blocked"
		}
		events = append(events, ical.Event{
			UID:     fmt.Sprintf("%s-%s@airhost", propertyID, r.CheckIn.Format("20060102")),
			Summary: summary,
			Start:   r.CheckIn,
			End:     r.CheckOut,
		})
	}
	body := ical.Render("AirHost — listing "+propertyID.String(), events)
	c.Header("Content-Disposition", `attachment; filename="airhost-`+propertyID.String()+`.ics"`)
	c.Data(http.StatusOK, "text/calendar; charset=utf-8", body)
}

func parseDateOr(raw string, fallback time.Time) time.Time {
	if raw == "" {
		return fallback
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return fallback
	}
	return t.UTC()
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
