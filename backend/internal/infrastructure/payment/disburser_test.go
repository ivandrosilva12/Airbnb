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
	"github.com/google/uuid"
)

func TestFakeDisburser_AlwaysSucceeds(t *testing.T) {
	money, _ := shared.NewMoney(33000, "EUR")
	ref, err := NewFakeDisburser().Disburse(context.Background(), uuid.New(), money, "idem")
	if err != nil {
		t.Fatalf("disburse: %v", err)
	}
	if ref == "" {
		t.Fatal("expected a non-empty fake reference")
	}
}

func TestStripeDisburser_CreatesTransfer(t *testing.T) {
	var gotAuth, gotIdem, gotPath, gotBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotIdem = r.Header.Get("Idempotency-Key")
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"tr_123","object":"transfer"}`))
	}))
	defer srv.Close()

	d := NewStripeDisburser(config.StripeConfig{SecretKey: "sk_test_x", BaseURL: srv.URL, ConnectAccount: "acct_999"})
	host := uuid.New()
	money, _ := shared.NewMoney(33000, "EUR")

	ref, err := d.Disburse(context.Background(), host, money, "disb-1")
	if err != nil {
		t.Fatalf("disburse: %v", err)
	}
	if ref != "tr_123" {
		t.Fatalf("ref = %q, want tr_123", ref)
	}
	if gotAuth != "Bearer sk_test_x" {
		t.Fatalf("auth header = %q", gotAuth)
	}
	if gotIdem != "disb-1" {
		t.Fatalf("idempotency header = %q, want disb-1", gotIdem)
	}
	if gotPath != "/v1/transfers" {
		t.Fatalf("path = %q, want /v1/transfers", gotPath)
	}
	form, _ := url.ParseQuery(gotBody)
	if form.Get("amount") != "33000" || form.Get("currency") != "eur" || form.Get("destination") != "acct_999" {
		t.Fatalf("unexpected transfer form: %s", gotBody)
	}
	if form.Get("transfer_group") != "host_"+host.String() {
		t.Fatalf("transfer_group = %q", form.Get("transfer_group"))
	}
}

func TestNewDisburser_SelectsRailFromConfig(t *testing.T) {
	// Fake rail unless provider=stripe AND both a key and a connected account are set.
	fake := NewDisburser(config.PaymentConfig{Provider: "stripe", Stripe: config.StripeConfig{SecretKey: "sk_test_x"}})
	if _, ok := fake.(*FakeDisburser); !ok {
		t.Fatalf("want fake disburser without a connect account, got %T", fake)
	}
	real := NewDisburser(config.PaymentConfig{Provider: "stripe", Stripe: config.StripeConfig{SecretKey: "sk_test_x", ConnectAccount: "acct_1"}})
	if _, ok := real.(*StripeDisburser); !ok {
		t.Fatalf("want Stripe disburser when fully configured, got %T", real)
	}
}
