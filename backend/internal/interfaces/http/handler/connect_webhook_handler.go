package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"

	payoutapp "github.com/airhost/backend/internal/application/payout"
	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/infrastructure/observability"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/airhost/backend/internal/observability/logctx"
	"github.com/gin-gonic/gin"
)

// ConnectWebhookHandler receives Stripe Connect account webhooks and syncs the
// host's payout capability (account.updated → payouts_enabled). Verifiers are
// keyed by provider; an unconfigured provider yields 404.
type ConnectWebhookHandler struct {
	svc       *payoutapp.Service
	verifiers map[string]port.ConnectWebhookVerifier
	dedupe    port.WebhookDedupeStore
	metrics   *observability.Metrics
}

// NewConnectWebhookHandler builds a ConnectWebhookHandler. dedupe may be nil
// (the sync is idempotent regardless).
func NewConnectWebhookHandler(svc *payoutapp.Service, verifiers map[string]port.ConnectWebhookVerifier, dedupe port.WebhookDedupeStore, metrics *observability.Metrics) *ConnectWebhookHandler {
	if verifiers == nil {
		verifiers = map[string]port.ConnectWebhookVerifier{}
	}
	return &ConnectWebhookHandler{svc: svc, verifiers: verifiers, dedupe: dedupe, metrics: metrics}
}

func (h *ConnectWebhookHandler) observe(provider, outcome string) {
	if h.metrics != nil {
		h.metrics.WebhookEventsTotal.WithLabelValues(provider, outcome).Inc()
	}
}

// Handle verifies a Connect webhook and updates the owning host's payout state.
// Returns 200 for any authentic request (including no-ops) so the provider stops
// retrying, and 400 when the signature/payload cannot be verified.
func (h *ConnectWebhookHandler) Handle(c *gin.Context) {
	provider := "connect_" + c.Param("provider")
	verifier, ok := h.verifiers[c.Param("provider")]
	if !ok {
		h.observe(provider, "unknown_provider")
		response.FailMessage(c, http.StatusNotFound, "unknown or unconfigured connect provider")
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
		h.observe(provider, "rejected")
		logctx.LoggerFrom(c.Request.Context()).Warn("connect webhook: verification failed", "provider", provider, "error", err)
		response.FailMessage(c, http.StatusBadRequest, "invalid webhook signature or payload")
		return
	}
	if !actionable {
		h.observe(provider, "ignored")
		response.OK(c, gin.H{"status": "ignored"})
		return
	}

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

	changed, err := h.svc.SyncAccountFromWebhook(c.Request.Context(), evt.AccountID, evt.PayoutsEnabled)
	if err != nil {
		h.observe(provider, "error")
		response.Fail(c, err)
		return
	}
	if h.dedupe != nil {
		if err := h.dedupe.Record(c.Request.Context(), provider, eventID); err != nil {
			logctx.LoggerFrom(c.Request.Context()).Warn("connect webhook: failed to record dedupe key", "provider", provider, "error", err)
		}
	}
	if changed {
		h.observe(provider, "reconciled")
	} else {
		h.observe(provider, "noop")
	}
	response.OK(c, gin.H{"status": "ok", "updated": changed})
}
