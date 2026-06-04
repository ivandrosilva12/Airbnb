package payment

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/config"
)

// stripeReplayTolerance is the maximum age of a Stripe webhook timestamp; older
// (or far-future) signatures are rejected to prevent replay.
const stripeReplayTolerance = 5 * time.Minute

// NewWebhookVerifiers builds the set of provider webhook verifiers keyed by
// provider name, for whichever providers have a webhook secret configured.
// A provider without a secret is omitted (its webhook route returns 404),
// avoiding an unauthenticated reconciliation path.
func NewWebhookVerifiers(cfg config.PaymentConfig) map[string]port.WebhookVerifier {
	out := map[string]port.WebhookVerifier{}
	if cfg.Stripe.WebhookSecret != "" {
		out[nameStripe] = &stripeWebhookVerifier{secret: cfg.Stripe.WebhookSecret, tolerance: stripeReplayTolerance}
	}
	if cfg.AppyPay.WebhookSecret != "" {
		out[nameAppyPay] = &jsonWebhookVerifier{provider: nameAppyPay, secret: cfg.AppyPay.WebhookSecret}
	}
	if cfg.GPayAngola.WebhookSecret != "" {
		out[nameGPayAngola] = &jsonWebhookVerifier{provider: nameGPayAngola, secret: cfg.GPayAngola.WebhookSecret}
	}
	if len(out) == 0 {
		slog.Info("payment: no webhook secrets configured; payment webhook endpoint disabled")
	}
	return out
}

// hmacHexEqual reports whether the hex-encoded HMAC-SHA256 of payload under
// secret equals the provided signature, in constant time.
func hmacHexEqual(secret, signature string, payload []byte) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(strings.TrimSpace(signature)))
}

// --- Stripe ------------------------------------------------------------------

// stripeWebhookVerifier verifies the `Stripe-Signature` header (t=…,v1=…) as an
// HMAC-SHA256 of "<timestamp>.<body>" and maps Stripe event types. The signed
// timestamp is also checked against a tolerance window to prevent replay.
type stripeWebhookVerifier struct {
	secret    string
	tolerance time.Duration
	now       func() time.Time // injectable clock for tests
}

// verifyStripeSignature authenticates a Stripe webhook: it parses the
// `Stripe-Signature` header (t=…,v1=…), recomputes the HMAC-SHA256 of
// "<timestamp>.<body>" (accepting any v1 during a secret rotation), and rejects
// timestamps outside the tolerance window to prevent replay.
func verifyStripeSignature(secret string, tolerance time.Duration, now func() time.Time, header http.Header, body []byte) error {
	sig := header.Get("Stripe-Signature")
	if sig == "" {
		return fmt.Errorf("stripe webhook: missing signature")
	}
	var timestamp string
	var v1s []string
	for _, part := range strings.Split(sig, ",") {
		k, val, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			timestamp = val
		case "v1":
			v1s = append(v1s, val)
		}
	}
	if timestamp == "" || len(v1s) == 0 {
		return fmt.Errorf("stripe webhook: malformed signature")
	}
	signedPayload := append([]byte(timestamp+"."), body...)
	matched := false
	for _, v1 := range v1s {
		if hmacHexEqual(secret, v1, signedPayload) {
			matched = true
			break
		}
	}
	if !matched {
		return fmt.Errorf("stripe webhook: signature mismatch")
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("stripe webhook: bad timestamp")
	}
	if tolerance <= 0 {
		tolerance = stripeReplayTolerance
	}
	clock := now
	if clock == nil {
		clock = time.Now
	}
	if age := clock().Sub(time.Unix(ts, 0)); age > tolerance || age < -tolerance {
		return fmt.Errorf("stripe webhook: timestamp outside tolerance")
	}
	return nil
}

func (v *stripeWebhookVerifier) Verify(header http.Header, body []byte) (port.GatewayEvent, bool, error) {
	if err := verifyStripeSignature(v.secret, v.tolerance, v.now, header, body); err != nil {
		return port.GatewayEvent{}, false, err
	}

	var evt struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			Object struct {
				ID             string `json:"id"`
				PaymentIntent  string `json:"payment_intent"`
				AmountRefunded int64  `json:"amount_refunded"`
			} `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &evt); err != nil {
		return port.GatewayEvent{}, false, fmt.Errorf("stripe webhook: %w", err)
	}
	obj := evt.Data.Object
	base := port.GatewayEvent{Provider: nameStripe, EventID: evt.ID}
	switch evt.Type {
	case "payment_intent.succeeded":
		base.Reference, base.Type = obj.ID, port.GatewayCaptured
		return base, true, nil
	case "payment_intent.amount_capturable_updated":
		// In Stripe's manual-capture flow this fires once the 3DS challenge
		// (or any other async authentication) has cleared and the funds are
		// fully held but not yet captured — the "authorized" moment for
		// async-gateway integration.
		base.Reference, base.Type = obj.ID, port.GatewayAuthorized
		return base, true, nil
	case "charge.refunded":
		ref := obj.PaymentIntent
		if ref == "" {
			ref = obj.ID
		}
		base.Reference, base.Type, base.AmountCents = ref, port.GatewayRefunded, obj.AmountRefunded
		return base, true, nil
	case "payment_intent.payment_failed":
		base.Reference, base.Type, base.FailureReason = obj.ID, port.GatewayFailed, "payment failed"
		return base, true, nil
	default:
		base.Type = port.GatewayIgnored
		return base, false, nil
	}
}

// --- Stripe Connect (account.updated) ----------------------------------------

// NewConnectWebhookVerifiers builds the Connect webhook verifiers keyed by
// provider. Only Stripe is supported. Stripe delivers Connect events to a
// separate endpoint with its own signing secret (STRIPE_CONNECT_WEBHOOK_SECRET);
// when that is unset the payment webhook secret is reused for single-endpoint
// setups. Without either secret the Connect webhook route returns 404.
func NewConnectWebhookVerifiers(cfg config.PaymentConfig) map[string]port.ConnectWebhookVerifier {
	out := map[string]port.ConnectWebhookVerifier{}
	secret := cfg.Stripe.ConnectWebhookSecret
	if secret == "" {
		secret = cfg.Stripe.WebhookSecret
	}
	if secret != "" {
		out[nameStripe] = &stripeConnectWebhookVerifier{secret: secret, tolerance: stripeReplayTolerance}
	}
	if len(out) == 0 {
		slog.Info("payout: no Connect webhook secret configured; Connect webhook endpoint disabled")
	}
	return out
}

// stripeConnectWebhookVerifier authenticates a Stripe Connect webhook and maps
// account.updated events to a normalized ConnectAccountEvent.
type stripeConnectWebhookVerifier struct {
	secret    string
	tolerance time.Duration
	now       func() time.Time // injectable clock for tests
}

func (v *stripeConnectWebhookVerifier) Verify(header http.Header, body []byte) (port.ConnectAccountEvent, bool, error) {
	if err := verifyStripeSignature(v.secret, v.tolerance, v.now, header, body); err != nil {
		return port.ConnectAccountEvent{}, false, err
	}
	var evt struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			Object struct {
				ID             string `json:"id"`
				PayoutsEnabled bool   `json:"payouts_enabled"`
			} `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &evt); err != nil {
		return port.ConnectAccountEvent{}, false, fmt.Errorf("stripe connect webhook: %w", err)
	}
	if evt.Type != "account.updated" {
		return port.ConnectAccountEvent{EventID: evt.ID}, false, nil
	}
	return port.ConnectAccountEvent{
		EventID:        evt.ID,
		AccountID:      evt.Data.Object.ID,
		PayoutsEnabled: evt.Data.Object.PayoutsEnabled,
	}, true, nil
}

// --- AppyPay / GPay Angola ---------------------------------------------------

// jsonWebhookVerifier verifies a hex HMAC-SHA256 of the raw body in the
// `X-Signature` header and maps a simple JSON event shape shared by the
// Angolan processors: {"event":"captured|refunded|failed","id":"…",
// "amount":<major units>,"reason":"…"}.
type jsonWebhookVerifier struct {
	provider string
	secret   string
}

func (v *jsonWebhookVerifier) Verify(header http.Header, body []byte) (port.GatewayEvent, bool, error) {
	sig := header.Get("X-Signature")
	if sig == "" {
		return port.GatewayEvent{}, false, fmt.Errorf("%s webhook: missing signature", v.provider)
	}
	if !hmacHexEqual(v.secret, sig, body) {
		return port.GatewayEvent{}, false, fmt.Errorf("%s webhook: signature mismatch", v.provider)
	}

	var evt struct {
		Event   string  `json:"event"`
		ID      string  `json:"id"`
		EventID string  `json:"eventId"`
		Amount  float64 `json:"amount"`
		Reason  string  `json:"reason"`
	}
	if err := json.Unmarshal(body, &evt); err != nil {
		return port.GatewayEvent{}, false, fmt.Errorf("%s webhook: %w", v.provider, err)
	}
	base := port.GatewayEvent{Provider: v.provider, EventID: evt.EventID, Reference: evt.ID}
	switch strings.ToLower(evt.Event) {
	case "authorized", "authorize", "auth", "approved":
		// Async-auth completed (e.g. AppyPay redirect finished, 3DS cleared).
		// The reconciler moves a still-pending payment to authorized.
		base.Type = port.GatewayAuthorized
	case "captured", "capture", "succeeded", "paid":
		base.Type = port.GatewayCaptured
	case "refunded", "refund":
		base.Type = port.GatewayRefunded
		base.AmountCents = int64(math.Round(evt.Amount * 100))
	case "failed", "declined", "error":
		base.Type = port.GatewayFailed
		base.FailureReason = evt.Reason
	default:
		base.Type = port.GatewayIgnored
		return base, false, nil
	}
	return base, true, nil
}
