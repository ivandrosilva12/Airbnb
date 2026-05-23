package handler

import (
	paymentapp "github.com/airhost/backend/internal/application/payment"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// PaymentHandler exposes payment read endpoints.
type PaymentHandler struct {
	svc *paymentapp.Service
}

// NewPaymentHandler builds a PaymentHandler.
func NewPaymentHandler(svc *paymentapp.Service) *PaymentHandler { return &PaymentHandler{svc: svc} }

// ListMine returns the authenticated guest's payments.
func (h *PaymentHandler) ListMine(c *gin.Context) {
	guestID, ok := requireUser(c)
	if !ok {
		return
	}
	res, err := h.svc.ListForGuest(c.Request.Context(), guestID, pageFromQuery(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.PaymentView, 0, len(res.Items))
	for _, p := range res.Items {
		items = append(items, dto.FromPayment(p))
	}
	response.OK(c, dto.PageView[dto.PaymentView]{Items: items, Total: res.Total})
}

// GetForBooking returns the payment for a booking the guest owns.
func (h *PaymentHandler) GetForBooking(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	bookingID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	p, err := h.svc.GetForBooking(c.Request.Context(), actorID, bookingID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromPayment(p))
}
