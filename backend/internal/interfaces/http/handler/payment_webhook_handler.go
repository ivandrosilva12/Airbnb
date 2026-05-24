package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"

	paymentapp "github.com/airhost/backend/internal/application/payment"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/infrastructure/observability"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// PaymentWebhookHandler receives asynchronous gateway webhooks and reconciles
// the local payment with the provider's authoritative state. Verifiers are
// keyed by provider name; a provider without a configured verifier yields 404.
type PaymentWebhookHandler struct {
	svc       *paymentapp.Service
	verifiers map[string]port.WebhookVerifier
	dedupe    port.WebhookDedupeStore
	metrics   *observability.Metrics
}

// NewPaymentWebhookHandler builds a PaymentWebhookHandler. dedupe may be nil to
// disable storage-level de-duplication (reconciliation stays idempotent anyway).
func NewPaymentWebhookHandler(svc *paymentapp.Service, verifiers map[string]port.WebhookVerifier, dedupe port.WebhookDedupeStore, metrics *observability.Metrics) *PaymentWebhookHandler {
	if verifiers == nil {
		verifiers = map[string]port.WebhookVerifier{}
	}
	return &PaymentWebhookHandler{svc: svc, verifiers: verifiers, dedupe: dedupe, metrics: metrics}
}

// observe bumps the webhook-events counter (nil-safe for tests without metrics).
func (h *PaymentWebhookHandler) observe(provider, outcome string) {
	if h.metrics != nil {
		h.metrics.WebhookEventsTotal.WithLabelValues(provider, outcome).Inc()
	}
}

// Handle verifies and reconciles a webhook for the provider in the path. It
// returns 200 for any authentic request (including no-ops) so the gateway stops
// retrying, and 400 when the signature/payload cannot be verified.
func (h *PaymentWebhookHandler) Handle(c *gin.Context) {
	provider := c.Param("provider")
	verifier, ok := h.verifiers[provider]
	if !ok {
		h.observe(provider, "unknown_provider")
		response.FailMessage(c, http.StatusNotFound, "unknown or unconfigured payment provider")
		return
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		h.observe(provider, "error")
		response.FailMessage(c, http.StatusBadRequest, "could not read request body")
		return
	}

	evt, actionable, err := verifier.Verify(c.Request.Header, body)
	if err != nil {
		// Signature/parse failures are client errors; do not leak detail.
		h.observe(provider, "rejected")
		slog.Warn("payment webhook: verification failed", "provider", provider, "error", err)
		response.FailMessage(c, http.StatusBadRequest, "invalid webhook signature or payload")
		return
	}
	if !actionable {
		h.observe(provider, "ignored")
		response.OK(c, gin.H{"status": "ignored"})
		return
	}

	// Storage-level de-duplication: skip a delivery already processed. The key is
	// the provider's delivery id when present, else a hash of the raw body so
	// byte-identical replays still collapse.
	eventID := evt.EventID
	if eventID == "" {
		sum := sha256.Sum256(body)
		eventID = hex.EncodeToString(sum[:])
	}
	if h.dedupe != nil {
		seen, err := h.dedupe.Seen(c.Request.Context(), provider, eventID)
		if err != nil {
			h.observe(provider, "error")
			response.Fail(c, err)
			return
		}
		if seen {
			h.observe(provider, "duplicate")
			response.OK(c, gin.H{"status": "duplicate"})
			return
		}
	}

	changed, err := h.svc.ReconcileGatewayEvent(c.Request.Context(), evt)
	if err != nil {
		h.observe(provider, "error")
		response.Fail(c, err)
		return
	}
	// Record only after a successful reconcile, so a failed attempt is retried
	// rather than silently skipped. The reconcile is idempotent, so the brief
	// window where a concurrent duplicate also reconciles is harmless.
	if h.dedupe != nil {
		if err := h.dedupe.Record(c.Request.Context(), provider, eventID); err != nil {
			slog.Warn("payment webhook: failed to record dedupe key", "provider", provider, "error", err)
		}
	}
	if changed {
		h.observe(provider, "reconciled")
	} else {
		h.observe(provider, "noop")
	}
	response.OK(c, gin.H{"status": "ok", "reconciled": changed})
}
