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

	// payment_intent.amount_capturable_updated maps to the async-auth completed
	// signal (WF-GAP-010): the 3DS challenge cleared and the hold is in place
	// but not yet captured. The reconciler moves a pending payment to
	// authorized and publishes PaymentAuthorized so the booking can confirm.
	abody := []byte(`{"type":"payment_intent.amount_capturable_updated","data":{"object":{"id":"pi_async_42"}}}`)
	h.Set("Stripe-Signature", stripeSignature("whsec_test", ts, abody))
	evt, ok, err = v.Verify(h, abody)
	if err != nil || !ok {
		t.Fatalf("verify amount_capturable_updated: ok=%v err=%v", ok, err)
	}
	if evt.Type != port.GatewayAuthorized || evt.Reference != "pi_async_42" {
		t.Fatalf("amount_capturable_updated event = %+v, want authorized pi_async_42", evt)
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

func TestStripeConnectWebhookVerifier_AccountUpdated(t *testing.T) {
	now := time.Unix(1700000000, 0)
	v := &stripeConnectWebhookVerifier{secret: "whsec_test", tolerance: 5 * time.Minute, now: func() time.Time { return now }}
	ts := now.Unix()
	body := []byte(`{"id":"evt_1","type":"account.updated","data":{"object":{"id":"acct_42","payouts_enabled":true}}}`)

	h := http.Header{}
	h.Set("Stripe-Signature", stripeSignature("whsec_test", ts, body))

	evt, ok, err := v.Verify(h, body)
	if err != nil || !ok {
		t.Fatalf("verify: ok=%v err=%v", ok, err)
	}
	if evt.EventID != "evt_1" || evt.AccountID != "acct_42" || !evt.PayoutsEnabled {
		t.Fatalf("event = %+v, want acct_42 payouts enabled", evt)
	}

	// A non-account event is authentic but not actionable.
	other := []byte(`{"id":"evt_2","type":"payout.paid","data":{"object":{"id":"po_1"}}}`)
	h.Set("Stripe-Signature", stripeSignature("whsec_test", ts, other))
	if _, ok, err := v.Verify(h, other); err != nil || ok {
		t.Fatalf("payout.paid: ok=%v err=%v, want not-actionable", ok, err)
	}

	// Tampered signature is rejected.
	h.Set("Stripe-Signature", "t="+strconv.FormatInt(ts, 10)+",v1=deadbeef")
	if _, _, err := v.Verify(h, body); err == nil {
		t.Fatal("expected signature mismatch error")
	}
}

func TestNewConnectWebhookVerifiers_SecretSelection(t *testing.T) {
	// No secret at all → disabled.
	if vs := NewConnectWebhookVerifiers(config.PaymentConfig{}); len(vs) != 0 {
		t.Fatalf("expected no verifiers without a secret, got %d", len(vs))
	}
	// Dedicated Connect secret enables it.
	if vs := NewConnectWebhookVerifiers(config.PaymentConfig{Stripe: config.StripeConfig{ConnectWebhookSecret: "whsec_connect"}}); vs[nameStripe] == nil {
		t.Fatal("expected a stripe connect verifier from the dedicated secret")
	}
	// Falls back to the payment webhook secret when no dedicated one is set.
	if vs := NewConnectWebhookVerifiers(config.PaymentConfig{Stripe: config.StripeConfig{WebhookSecret: "whsec"}}); vs[nameStripe] == nil {
		t.Fatal("expected a stripe connect verifier falling back to the payment secret")
	}
	// The dedicated secret takes precedence and is the one used to verify.
	vs := NewConnectWebhookVerifiers(config.PaymentConfig{Stripe: config.StripeConfig{WebhookSecret: "whsec_pay", ConnectWebhookSecret: "whsec_connect"}})
	v, ok := vs[nameStripe].(*stripeConnectWebhookVerifier)
	if !ok || v.secret != "whsec_connect" {
		t.Fatalf("expected the dedicated connect secret to be used, got %+v", vs[nameStripe])
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

	// "authorized" maps to the async-auth completed signal (WF-GAP-010): the
	// AppyPay/GPay redirect (or 3DS challenge) cleared and the gateway is
	// reporting the hold is now in place.
	abody := []byte(`{"event":"authorized","id":"gp_async_1","eventId":"evt_a1"}`)
	h.Set("X-Signature", hexHMAC("shh", abody))
	evt, ok, err = v.Verify(h, abody)
	if err != nil || !ok {
		t.Fatalf("verify authorized: ok=%v err=%v", ok, err)
	}
	if evt.Type != port.GatewayAuthorized || evt.Reference != "gp_async_1" || evt.EventID != "evt_a1" {
		t.Fatalf("authorized event = %+v, want authorized gp_async_1 evt_a1", evt)
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
