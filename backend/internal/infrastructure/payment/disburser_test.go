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
	ref, err := NewFakeDisburser().Disburse(context.Background(), uuid.New(), "acct_x", money, "idem")
	if err != nil {
		t.Fatalf("disburse: %v", err)
	}
	if ref == "" {
		t.Fatal("expected a non-empty fake reference")
	}
}

func TestFakeConnectGateway_EnabledAccount(t *testing.T) {
	g := NewFakeConnectGateway()
	acct, err := g.CreateAccount(context.Background(), "host@ex.dev")
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	if acct.ID == "" || !acct.PayoutsEnabled {
		t.Fatalf("fake account = %+v, want an id + payouts enabled", acct)
	}
	url, err := g.CreateOnboardingLink(context.Background(), acct.ID, "", "https://app/return")
	if err != nil || url != "https://app/return" {
		t.Fatalf("onboarding link = %q err=%v, want the return url", url, err)
	}
}

func TestStripeDisburser_CreatesTransferToDestination(t *testing.T) {
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

	d := NewStripeDisburser(config.StripeConfig{SecretKey: "sk_test_x", BaseURL: srv.URL})
	host := uuid.New()
	money, _ := shared.NewMoney(33000, "EUR")

	ref, err := d.Disburse(context.Background(), host, "acct_999", money, "disb-1")
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

func TestStripeDisburser_RequiresDestination(t *testing.T) {
	d := NewStripeDisburser(config.StripeConfig{SecretKey: "sk_test_x", BaseURL: "http://unused"})
	money, _ := shared.NewMoney(1000, "EUR")
	if _, err := d.Disburse(context.Background(), uuid.New(), "", money, "k"); err == nil {
		t.Fatal("expected an error when the destination account is missing")
	}
}

func TestStripeConnectGateway_OnboardingFlow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/accounts" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			form, _ := url.ParseQuery(string(body))
			if form.Get("type") != "express" || form.Get("country") != "PT" ||
				form.Get("capabilities[transfers][requested]") != "true" {
				t.Errorf("unexpected account form: %s", string(body))
			}
			_, _ = w.Write([]byte(`{"id":"acct_1","payouts_enabled":false}`))
		case r.URL.Path == "/v1/account_links" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"url":"https://connect.stripe.com/setup/abc"}`))
		case r.URL.Path == "/v1/accounts/acct_1" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"acct_1","payouts_enabled":true}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	g := NewStripeConnectGateway(config.StripeConfig{SecretKey: "sk_test_x", BaseURL: srv.URL, ConnectCountry: "PT"})
	ctx := context.Background()

	acct, err := g.CreateAccount(ctx, "host@ex.dev")
	if err != nil || acct.ID != "acct_1" || acct.PayoutsEnabled {
		t.Fatalf("create account = %+v err=%v, want acct_1 not-yet-enabled", acct, err)
	}
	link, err := g.CreateOnboardingLink(ctx, "acct_1", "https://app/refresh", "https://app/return")
	if err != nil || link != "https://connect.stripe.com/setup/abc" {
		t.Fatalf("onboarding link = %q err=%v", link, err)
	}
	got, err := g.GetAccount(ctx, "acct_1")
	if err != nil || !got.PayoutsEnabled {
		t.Fatalf("get account = %+v err=%v, want payouts enabled", got, err)
	}
}

func TestNewDisburser_SelectsRailFromConfig(t *testing.T) {
	// Fake rail unless provider=stripe AND a secret key is set.
	if _, ok := NewDisburser(config.PaymentConfig{Provider: "fake"}).(*FakeDisburser); !ok {
		t.Fatal("want fake disburser for provider=fake")
	}
	if _, ok := NewDisburser(config.PaymentConfig{Provider: "stripe"}).(*FakeDisburser); !ok {
		t.Fatal("want fake disburser when stripe has no secret key")
	}
	real := NewDisburser(config.PaymentConfig{Provider: "stripe", Stripe: config.StripeConfig{SecretKey: "sk_test_x"}})
	if _, ok := real.(*StripeDisburser); !ok {
		t.Fatalf("want Stripe disburser when configured, got %T", real)
	}
}

func TestNewConnectGateway_SelectsFromConfig(t *testing.T) {
	if _, ok := NewConnectGateway(config.PaymentConfig{Provider: "fake"}).(*FakeConnectGateway); !ok {
		t.Fatal("want fake connect gateway for provider=fake")
	}
	real := NewConnectGateway(config.PaymentConfig{Provider: "stripe", Stripe: config.StripeConfig{SecretKey: "sk_test_x"}})
	if _, ok := real.(*StripeConnectGateway); !ok {
		t.Fatalf("want Stripe connect gateway when configured, got %T", real)
	}
}
