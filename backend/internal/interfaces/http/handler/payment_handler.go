package handler

import (
	"net/http"

	paymentapp "github.com/airhost/backend/internal/application/payment"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/infrastructure/pdf"
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

// GetDeposit returns the security-deposit hold for a booking the guest owns.
// 404 when the listing has no deposit configured (or the hold was never placed).
func (h *PaymentHandler) GetDeposit(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	bookingID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	d, err := h.svc.GetDepositForBooking(c.Request.Context(), actorID, bookingID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, dto.FromDeposit(d))
}

// Receipt streams a PDF receipt for a booking's payment to the owning guest.
func (h *PaymentHandler) Receipt(c *gin.Context) {
	actorID, ok := requireUser(c)
	if !ok {
		return
	}
	bookingID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	data, err := h.svc.Receipt(c.Request.Context(), actorID, bookingID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	body, err := pdf.RenderReceipt(pdf.Receipt{
		ReceiptNo:     data.ReceiptNo,
		IssuedAt:      data.IssuedAt.Format("2006-01-02 15:04 UTC"),
		Status:        string(data.Status),
		PropertyTitle: data.PropertyTitle,
		CheckIn:       data.CheckIn.Format("2006-01-02"),
		CheckOut:      data.CheckOut.Format("2006-01-02"),
		Nights:        data.Nights,
		Guests:        data.Guests,
		Subtotal:      data.Subtotal.String(),
		Discount:      moneyOrEmpty(data.Discount),
		CleaningFee:   data.CleaningFee.String(),
		ServiceFee:    data.ServiceFee.String(),
		Tax:           moneyOrEmpty(data.Tax),
		Total:         data.Total.String(),
	})
	if err != nil {
		response.FailMessage(c, http.StatusInternalServerError, "could not render receipt")
		return
	}
	c.Header("Content-Disposition", `attachment; filename="airhost-receipt-`+bookingID.String()+`.pdf"`)
	c.Data(http.StatusOK, "application/pdf", body)
}

// moneyOrEmpty renders a money value, or "" when it is zero (so optional receipt
// lines like discount/tax are omitted rather than shown as 0.00).
func moneyOrEmpty(m shared.Money) string {
	if m.AmountCents() == 0 {
		return ""
	}
	return m.String()
}
