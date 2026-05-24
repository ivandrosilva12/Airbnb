package handler

import (
	"io"
	"log/slog"
	"net/http"

	paymentapp "github.com/airhost/backend/internal/application/payment"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// PaymentWebhookHandler receives asynchronous gateway webhooks and reconciles
// the local payment with the provider's authoritative state. Verifiers are
// keyed by provider name; a provider without a configured verifier yields 404.
type PaymentWebhookHandler struct {
	svc       *paymentapp.Service
	verifiers map[string]port.WebhookVerifier
}

// NewPaymentWebhookHandler builds a PaymentWebhookHandler.
func NewPaymentWebhookHandler(svc *paymentapp.Service, verifiers map[string]port.WebhookVerifier) *PaymentWebhookHandler {
	if verifiers == nil {
		verifiers = map[string]port.WebhookVerifier{}
	}
	return &PaymentWebhookHandler{svc: svc, verifiers: verifiers}
}

// Handle verifies and reconciles a webhook for the provider in the path. It
// returns 200 for any authentic request (including no-ops) so the gateway stops
// retrying, and 400 when the signature/payload cannot be verified.
func (h *PaymentWebhookHandler) Handle(c *gin.Context) {
	provider := c.Param("provider")
	verifier, ok := h.verifiers[provider]
	if !ok {
		response.FailMessage(c, http.StatusNotFound, "unknown or unconfigured payment provider")
		return
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		response.FailMessage(c, http.StatusBadRequest, "could not read request body")
		return
	}

	evt, actionable, err := verifier.Verify(c.Request.Header, body)
	if err != nil {
		// Signature/parse failures are client errors; do not leak detail.
		slog.Warn("payment webhook: verification failed", "provider", provider, "error", err)
		response.FailMessage(c, http.StatusBadRequest, "invalid webhook signature or payload")
		return
	}
	if !actionable {
		response.OK(c, gin.H{"status": "ignored"})
		return
	}

	changed, err := h.svc.ReconcileGatewayEvent(c.Request.Context(), evt)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"status": "ok", "reconciled": changed})
}
