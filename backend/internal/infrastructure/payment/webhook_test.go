package payment

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/config"
)

func hexHMAC(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// stripeSignature builds a `Stripe-Signature` header value for the body at the
// given timestamp.
func stripeSignature(secret string, ts int64, body []byte) string {
	t := strconv.FormatInt(ts, 10)
	sig := hexHMAC(secret, append([]byte(t+"."), body...))
	return "t=" + t + ",v1=" + sig
}

func TestStripeWebhookVerifier(t *testing.T) {
	// A fixed clock so the signed timestamp sits inside the tolerance window.
	now := time.Unix(1700000000, 0)
	v := &stripeWebhookVerifier{secret: "whsec_test", tolerance: 5 * time.Minute, now: func() time.Time { return now }}
	ts := now.Unix()
	body := []byte(`{"type":"payment_intent.succeeded","data":{"object":{"id":"pi_42"}}}`)

	h := http.Header{}
	h.Set("Stripe-Signature", stripeSignature("whsec_test", ts, body))

	evt, ok, err := v.Verify(h, body)
	if err != nil || !ok {
		t.Fatalf("verify: ok=%v err=%v", ok, err)
	}
	if evt.Type != port.GatewayCaptured || evt.Reference != "pi_42" {
		t.Fatalf("event = %+v, want captured pi_42", evt)
	}

	// Tampered signature is rejected.
	h.Set("Stripe-Signature", "t="+strconv.FormatInt(ts, 10)+",v1=deadbeef")
	if _, _, err := v.Verify(h, body); err == nil {
		t.Fatal("expected signature mismatch error")
	}

	// A correctly signed but stale timestamp is rejected (anti-replay).
	old := now.Add(-time.Hour).Unix()
	h.Set("Stripe-Signature", stripeSignature("whsec_test", old, body))
	if _, _, err := v.Verify(h, body); err == nil {
		t.Fatal("expected stale-timestamp rejection")
	}

	// Multiple v1 signatures (as sent during a secret rotation) are accepted if
	// any one matches — here a bogus signature followed by the valid one.
	good := hexHMAC("whsec_test", append([]byte(strconv.FormatInt(ts, 10)+"."), body...))
	h.Set("Stripe-Signature", "t="+strconv.FormatInt(ts, 10)+",v1=deadbeef,v1="+good)
	if _, ok, err := v.Verify(h, body); err != nil || !ok {
		t.Fatalf("verify with rotated signatures: ok=%v err=%v", ok, err)
	}

	// A charge.refunded maps to a refund against the payment intent.
	rbody := []byte(`{"type":"charge.refunded","data":{"object":{"id":"ch_1","payment_intent":"pi_42","amount_refunded":1500}}}`)
	h.Set("Stripe-Signature", stripeSignature("whsec_test", ts, rbody))
	evt, ok, err = v.Verify(h, rbody)
	if err != nil || !ok {
		t.Fatalf("verify refund: ok=%v err=%v", ok, err)
	}
	if evt.Type != port.GatewayRefunded || evt.Reference != "pi_42" || evt.AmountCents != 1500 {
		t.Fatalf("refund event = %+v, want refunded pi_42 1500", evt)
	}
}

func TestJSONWebhookVerifier(t *testing.T) {
	v := &jsonWebhookVerifier{provider: nameGPayAngola, secret: "shh"}
	body := []byte(`{"event":"refunded","id":"gp_7","amount":12.50}`)

	h := http.Header{}
	h.Set("X-Signature", hexHMAC("shh", body))

	evt, ok, err := v.Verify(h, body)
	if err != nil || !ok {
		t.Fatalf("verify: ok=%v err=%v", ok, err)
	}
	if evt.Type != port.GatewayRefunded || evt.Reference != "gp_7" || evt.AmountCents != 1250 {
		t.Fatalf("event = %+v, want refunded gp_7 1250", evt)
	}

	// Wrong signature rejected.
	h.Set("X-Signature", "00")
	if _, _, err := v.Verify(h, body); err == nil {
		t.Fatal("expected signature mismatch error")
	}

	// Unknown event type is authentic but not actionable.
	ubody := []byte(`{"event":"pending","id":"gp_8"}`)
	h.Set("X-Signature", hexHMAC("shh", ubody))
	if _, ok, err := v.Verify(h, ubody); err != nil || ok {
		t.Fatalf("pending event: ok=%v err=%v, want not-actionable", ok, err)
	}
}

func TestNewWebhookVerifiers_OnlyConfigured(t *testing.T) {
	vs := NewWebhookVerifiers(config.PaymentConfig{
		Stripe:     config.StripeConfig{WebhookSecret: "whsec"},
		GPayAngola: config.GPayAngolaConfig{WebhookSecret: "k"},
	})
	if _, ok := vs[nameStripe]; !ok {
		t.Fatal("expected a stripe verifier")
	}
	if _, ok := vs[nameGPayAngola]; !ok {
		t.Fatal("expected a gpayangola verifier")
	}
	if _, ok := vs[nameAppyPay]; ok {
		t.Fatal("appypay has no secret; should be omitted")
	}
}
