package handler

import (
	"net/http"
	"time"

	offerapp "github.com/airhost/backend/internal/application/offer"
	"github.com/airhost/backend/internal/domain/offer"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// OfferHandler exposes host-offer endpoints: a host sends a pre-approval or
// special offer, and the guest accepts or declines it.
type OfferHandler struct {
	svc *offerapp.Service
}

// NewOfferHandler builds an OfferHandler.
func NewOfferHandler(svc *offerapp.Service) *OfferHandler { return &OfferHandler{svc: svc} }

type createOfferRequest struct {
	PropertyID string `json:"propertyId" binding:"required"`
	GuestID    string `json:"guestId" binding:"required"`
	CheckIn    string `json:"checkIn" binding:"required"`
	CheckOut   string `json:"checkOut" binding:"required"`
	Guests     int    `json:"guests" binding:"required"`
	PriceCents int64  `json:"priceCents"` // 0 = pre-approval at listing price
	Message    string `json:"message"`
}

// Create sends an offer from the authenticated host to a guest.
func (h *OfferHandler) Create(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	var req createOfferRequest
	if !bindJSON(c, &req) {
		return
	}
	propertyID, ok := parseUUID(c, req.PropertyID, "propertyId")
	if !ok {
		return
	}
	guestID, ok := parseUUID(c, req.GuestID, "guestId")
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
	o, err := h.svc.Create(c.Request.Context(), offerapp.CreateInput{
		HostID:     hostID,
		PropertyID: propertyID,
		GuestID:    guestID,
		CheckIn:    checkIn,
		CheckOut:   checkOut,
		Guests:     req.Guests,
		PriceCents: req.PriceCents,
		Message:    req.Message,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, dto.FromOffer(o))
}

// Accept turns the authenticated guest's offer into a confirmed booking.
func (h *OfferHandler) Accept(c *gin.Context) {
	guestID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	b, err := h.svc.Accept(c.Request.Context(), guestID, id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, dto.FromBooking(b))
}

// Decline turns down the authenticated guest's offer.
func (h *OfferHandler) Decline(c *gin.Context) {
	guestID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.Decline(c.Request.Context(), guestID, id); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// Withdraw takes back an offer the authenticated host sent.
func (h *OfferHandler) Withdraw(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.Withdraw(c.Request.Context(), hostID, id); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// ListMine returns the offers addressed to the authenticated guest.
func (h *OfferHandler) ListMine(c *gin.Context) {
	guestID, ok := requireUser(c)
	if !ok {
		return
	}
	offers, err := h.svc.ListForGuest(c.Request.Context(), guestID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"items": offerViews(offers)})
}

// ListSent returns the offers the authenticated host has sent.
func (h *OfferHandler) ListSent(c *gin.Context) {
	hostID, ok := requireUser(c)
	if !ok {
		return
	}
	offers, err := h.svc.ListForHost(c.Request.Context(), hostID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"items": offerViews(offers)})
}

func offerViews(offers []*offer.Offer) []dto.OfferView {
	items := make([]dto.OfferView, 0, len(offers))
	for _, o := range offers {
		items = append(items, dto.FromOffer(o))
	}
	return items
}
