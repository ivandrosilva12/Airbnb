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

func (v *stripeWebhookVerifier) Verify(header http.Header, body []byte) (port.GatewayEvent, bool, error) {
	sig := header.Get("Stripe-Signature")
	if sig == "" {
		return port.GatewayEvent{}, false, fmt.Errorf("stripe webhook: missing signature")
	}
	var timestamp, v1 string
	for _, part := range strings.Split(sig, ",") {
		k, val, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			timestamp = val
		case "v1":
			v1 = val
		}
	}
	if timestamp == "" || v1 == "" {
		return port.GatewayEvent{}, false, fmt.Errorf("stripe webhook: malformed signature")
	}
	signedPayload := append([]byte(timestamp+"."), body...)
	if !hmacHexEqual(v.secret, v1, signedPayload) {
		return port.GatewayEvent{}, false, fmt.Errorf("stripe webhook: signature mismatch")
	}
	// Anti-replay: reject timestamps outside the tolerance window.
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return port.GatewayEvent{}, false, fmt.Errorf("stripe webhook: bad timestamp")
	}
	tolerance := v.tolerance
	if tolerance <= 0 {
		tolerance = stripeReplayTolerance
	}
	clock := v.now
	if clock == nil {
		clock = time.Now
	}
	if age := clock().Sub(time.Unix(ts, 0)); age > tolerance || age < -tolerance {
		return port.GatewayEvent{}, false, fmt.Errorf("stripe webhook: timestamp outside tolerance")
	}

	var evt struct {
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
	switch evt.Type {
	case "payment_intent.succeeded", "payment_intent.amount_capturable_updated":
		return port.GatewayEvent{Provider: nameStripe, Reference: obj.ID, Type: port.GatewayCaptured}, true, nil
	case "charge.refunded":
		ref := obj.PaymentIntent
		if ref == "" {
			ref = obj.ID
		}
		return port.GatewayEvent{Provider: nameStripe, Reference: ref, Type: port.GatewayRefunded, AmountCents: obj.AmountRefunded}, true, nil
	case "payment_intent.payment_failed":
		return port.GatewayEvent{Provider: nameStripe, Reference: obj.ID, Type: port.GatewayFailed, FailureReason: "payment failed"}, true, nil
	default:
		return port.GatewayEvent{Provider: nameStripe, Type: port.GatewayIgnored}, false, nil
	}
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
		Event  string  `json:"event"`
		ID     string  `json:"id"`
		Amount float64 `json:"amount"`
		Reason string  `json:"reason"`
	}
	if err := json.Unmarshal(body, &evt); err != nil {
		return port.GatewayEvent{}, false, fmt.Errorf("%s webhook: %w", v.provider, err)
	}
	base := port.GatewayEvent{Provider: v.provider, Reference: evt.ID}
	switch strings.ToLower(evt.Event) {
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
