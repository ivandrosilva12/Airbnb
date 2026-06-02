package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	bookingapp "github.com/airhost/backend/internal/application/booking"
	fraudapp "github.com/airhost/backend/internal/application/fraud"
	houserulesapp "github.com/airhost/backend/internal/application/houserules"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/infrastructure/ical"
	"github.com/airhost/backend/internal/infrastructure/observability"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/airhost/backend/internal/observability/logctx"
	"github.com/gin-gonic/gin"
)

// BookingHandler exposes reservation endpoints.
type BookingHandler struct {
	svc        *bookingapp.Service
	metrics    *observability.Metrics
	houseRules *houserulesapp.Service // nil = acceptance-recording disabled
	fraud      *fraudapp.Service      // nil = fraud scoring disabled (S68)
}

// NewBookingHandler builds a BookingHandler.
func NewBookingHandler(svc *bookingapp.Service, m *observability.Metrics) *BookingHandler {
	return &BookingHandler{svc: svc, metrics: m}
}

// WithHouseRules wires the houserules service so the handler can
// persist the per-booking acceptance row right after Create succeeds
// (S47). Optional: a deployment without house rules wired skips the
// post-commit recording without affecting the booking response.
func (h *BookingHandler) WithHouseRules(svc *houserulesapp.Service) *BookingHandler {
	h.houseRules = svc
	return h
}

// WithFraud wires the fraud assessor so each successful booking is
// scored right after Create commits (S68). Best-effort: a failure
// here MUST NOT undo the booking and MUST NOT block the response —
// the guest legitimately reserved, and the assessor's job is
// forensic, not gating. nil leaves the BookingHandler unaffected
// (e.g. tests that don't care about the fraud trail).
func (h *BookingHandler) WithFraud(svc *fraudapp.Service) *BookingHandler {
	h.fraud = svc
	return h
}

type createBookingRequest struct {
	PropertyID string `json:"propertyId" binding:"required"`
	CheckIn    string `json:"checkIn" binding:"required"`
	CheckOut   string `json:"checkOut" binding:"required"`
	Guests     int    `json:"guests" binding:"required"`
	CouponCode string `json:"couponCode"`
	// SplitShares, when present and non-empty, enables split payment. The
	// organizer (caller) must be one of the share emails and the share
	// amounts must sum exactly to the booking total. Restricted to
	// instant-book listings.
	SplitShares []splitShareRequest `json:"splitShares"`
	// AcceptedHouseRulesVersion is the version of the listing's house
	// rules the guest just acknowledged (S47). Required when the listing
	// has an active rule set; the booking service enforces the match —
	// a stale or missing number returns 422 with a re-prompt-the-guest
	// signal to the client.
	AcceptedHouseRulesVersion int `json:"acceptedHouseRulesVersion"`
}

type splitShareRequest struct {
	Email       string `json:"email" binding:"required"`
	AmountCents int64  `json:"amountCents" binding:"required"`
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
	shares := make([]bookingapp.SplitShareInput, 0, len(req.SplitShares))
	for _, s := range req.SplitShares {
		shares = append(shares, bookingapp.SplitShareInput{Email: s.Email, AmountCents: s.AmountCents})
	}
	b, err := h.svc.Create(c.Request.Context(), bookingapp.CreateInput{
		GuestID:                   guestID,
		PropertyID:                propertyID,
		CheckIn:                   checkIn,
		CheckOut:                  checkOut,
		Guests:                    req.Guests,
		CouponCode:                req.CouponCode,
		SplitShares:               shares,
		AcceptedHouseRulesVersion: req.AcceptedHouseRulesVersion,
	})
	if err != nil {
		// S29 — track high-value bookings that hit the KYC step-up gate, so
		// ops can correlate spikes with newly-onboarded high-value listings
		// or with KYC backlog.
		if errors.Is(err, shared.ErrKYCStepUpRequired) {
			h.metrics.KYCStepUpRequired.Inc()
		}
		response.Fail(c, err)
		return
	}
	h.metrics.BookingsCreated.Inc()

	// Record the per-booking house-rules acceptance proof (S47). Best
	// effort: a failure here MUST NOT undo the booking — the guest
	// legitimately acknowledged the version that was current at the
	// time, and that is the legal commitment. The missing proof row is
	// then a follow-up for ops (we log loudly so it's observable).
	if h.houseRules != nil && req.AcceptedHouseRulesVersion > 0 {
		if err := h.houseRules.RecordAcceptance(c.Request.Context(), b.ID, guestID, b.PropertyID, req.AcceptedHouseRulesVersion); err != nil {
			logctx.LoggerFrom(c.Request.Context()).Error(
				"house-rules acceptance recording failed",
				"bookingId", b.ID, "version", req.AcceptedHouseRulesVersion, "err", err,
			)
		}
	}

	// Score the fresh booking against the fraud heuristics (S68).
	// Best-effort post-commit: the assessor scans the guest's history
	// and emits a per-booking assessment that admins can browse via
	// GET /admin/fraud/assessments. Errors are logged loudly (a
	// missing assessment row is observable as a gap in the trail)
	// but never bubble out to the guest — fraud detection must not
	// gate the booking response.
	if h.fraud != nil {
		if _, err := h.fraud.Assess(c.Request.Context(), b.ID); err != nil {
			logctx.LoggerFrom(c.Request.Context()).Error(
				"fraud assessment failed",
				"bookingId", b.ID, "err", err,
			)
		}
	}

	response.Created(c, dto.FromBooking(b))
}

type modifyBookingRequest struct {
	CheckIn  string `json:"checkIn" binding:"required"`
	CheckOut string `json:"checkOut" binding:"required"`
	Guests   int    `json:"guests" binding:"required"`
}

// Modify changes the dates and/or guest count of a pending booking (guest only).
func (h *BookingHandler) Modify(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	var req modifyBookingRequest
	if !bindJSON(c, &req) {
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
	b, err := h.svc.Modify(c.Request.Context(), bookingapp.ModifyInput{
		ActorID:   actorID,
		BookingID: id,
		CheckIn:   checkIn,
		CheckOut:  checkOut,
		Guests:    req.Guests,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromBooking(b))
}

type previewCouponRequest struct {
	PropertyID string `json:"propertyId" binding:"required"`
	CheckIn    string `json:"checkIn" binding:"required"`
	CheckOut   string `json:"checkOut" binding:"required"`
	Code       string `json:"code" binding:"required"`
}

// Quote returns the full price breakdown a Create call would assemble for the
// same inputs, WITHOUT persisting anything (S59). Used by the booking-flow UI
// to show subtotal/fees/jurisdiction-tax/total before the guest commits. The
// caller need not be authenticated — pricing is public per-property data.
//
// Query params: checkIn, checkOut (YYYY-MM-DD); guests (int); couponCode (opt).
func (h *BookingHandler) Quote(c *gin.Context) {
	propertyID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	checkIn, err := time.Parse("2006-01-02", c.Query("checkIn"))
	if err != nil {
		response.FailMessage(c, http.StatusBadRequest, "checkIn must be YYYY-MM-DD")
		return
	}
	checkOut, err := time.Parse("2006-01-02", c.Query("checkOut"))
	if err != nil {
		response.FailMessage(c, http.StatusBadRequest, "checkOut must be YYYY-MM-DD")
		return
	}
	guests, _ := strconv.Atoi(c.Query("guests"))
	if guests < 1 {
		response.FailMessage(c, http.StatusBadRequest, "guests must be >= 1")
		return
	}
	pricing, err := h.svc.Quote(c.Request.Context(), bookingapp.QuoteInput{
		PropertyID: propertyID,
		CheckIn:    checkIn,
		CheckOut:   checkOut,
		Guests:     guests,
		CouponCode: c.Query("couponCode"),
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromPricing(pricing))
}

// PreviewCoupon returns the discount a promo code would yield for a stay, so the
// guest can see it before reserving.
func (h *BookingHandler) PreviewCoupon(c *gin.Context) {
	if _, ok := requireUser(c); !ok {
		return
	}
	var req previewCouponRequest
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
	preview, err := h.svc.PreviewCoupon(c.Request.Context(), propertyID, req.Code, checkIn, checkOut)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromCouponPreview(preview))
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

// Arrival returns the listing's check-in instructions and wifi credentials
// to the booking's guest, gated by the reveal window (≤ 48h before check-in
// through check-out). Outside that window the response is 403 even when the
// caller owns the booking; missing arrival info returns 404.
func (h *BookingHandler) Arrival(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	info, err := h.svc.ArrivalForBooking(c.Request.Context(), actorID, id, time.Now().UTC())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromArrivalInfo(info))
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
