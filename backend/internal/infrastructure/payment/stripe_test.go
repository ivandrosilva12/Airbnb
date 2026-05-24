package payment

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/airhost/backend/internal/config"
	"github.com/airhost/backend/internal/domain/shared"
)

func TestStripeGateway_AuthorizeCaptureRefund(t *testing.T) {
	ctx := context.Background()

	var gotAuth, gotIdem, gotPath, gotBody string
	var captureHit, refundHit bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.URL.Path == "/v1/payment_intents" && r.Method == http.MethodPost:
			gotIdem = r.Header.Get("Idempotency-Key")
			gotPath = r.URL.Path
			gotBody = string(body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"pi_123","status":"requires_capture"}`))
		case r.URL.Path == "/v1/payment_intents/pi_123/capture":
			captureHit = true
			_, _ = w.Write([]byte(`{"id":"pi_123","status":"succeeded"}`))
		case r.URL.Path == "/v1/refunds":
			refundHit = true
			gotBody = string(body)
			_, _ = w.Write([]byte(`{"id":"re_1","status":"succeeded"}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	g := NewStripeGateway(config.StripeConfig{SecretKey: "sk_test_x", BaseURL: srv.URL})
	money, _ := shared.NewMoney(12345, "EUR")

	ref, err := g.Authorize(ctx, money, "idem-key-1")
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if ref != "pi_123" {
		t.Fatalf("ref = %q, want pi_123", ref)
	}
	if gotAuth != "Bearer sk_test_x" {
		t.Fatalf("auth header = %q", gotAuth)
	}
	if gotIdem != "idem-key-1" {
		t.Fatalf("idempotency header = %q", gotIdem)
	}
	if gotPath != "/v1/payment_intents" {
		t.Fatalf("path = %q", gotPath)
	}
	form, _ := url.ParseQuery(gotBody)
	if form.Get("amount") != "12345" || form.Get("currency") != "eur" || form.Get("capture_method") != "manual" {
		t.Fatalf("unexpected authorize form: %s", gotBody)
	}

	if err := g.Capture(ctx, ref); err != nil {
		t.Fatalf("capture: %v", err)
	}
	if !captureHit {
		t.Fatal("capture endpoint not called")
	}

	if err := g.Refund(ctx, ref, 5000); err != nil {
		t.Fatalf("refund: %v", err)
	}
	if !refundHit {
		t.Fatal("refund endpoint not called")
	}
	form, _ = url.ParseQuery(gotBody)
	if form.Get("payment_intent") != "pi_123" || form.Get("amount") != "5000" {
		t.Fatalf("unexpected refund form: %s", gotBody)
	}
}

func TestStripeGateway_ErrorMapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Your card was declined."}}`))
	}))
	defer srv.Close()

	g := NewStripeGateway(config.StripeConfig{SecretKey: "sk_test_x", BaseURL: srv.URL})
	money, _ := shared.NewMoney(1000, "USD")
	_, err := g.Authorize(context.Background(), money, "k")
	if err == nil || err.Error() != "stripe: Your card was declined." {
		t.Fatalf("error = %v, want mapped decline message", err)
	}
}
