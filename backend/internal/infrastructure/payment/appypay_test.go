package payment

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/airhost/backend/internal/config"
	"github.com/airhost/backend/internal/domain/shared"
)

func TestAppyPayGateway_AuthorizeCaptureRefund(t *testing.T) {
	ctx := context.Background()

	var gotAuth, gotContentType string
	var authBody map[string]any
	var captureHit, refundHit bool
	var refundBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.URL.Path == "/charges" && r.Method == http.MethodPost:
			_ = json.Unmarshal(body, &authBody)
			_, _ = w.Write([]byte(`{"id":"chg_99","status":"authorized"}`))
		case r.URL.Path == "/charges/chg_99/capture":
			captureHit = true
			_, _ = w.Write([]byte(`{"id":"chg_99","status":"captured"}`))
		case r.URL.Path == "/charges/chg_99/refunds":
			refundHit = true
			_ = json.Unmarshal(body, &refundBody)
			_, _ = w.Write([]byte(`{"id":"rf_1","status":"refunded"}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	g := NewAppyPayGateway(config.AppyPayConfig{Token: "tok_abc", BaseURL: srv.URL})
	money, _ := shared.NewMoney(50000, "AOA") // 500.00 AOA

	ref, err := g.Authorize(ctx, money, "merchant-tx-1")
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if ref != "chg_99" {
		t.Fatalf("ref = %q, want chg_99", ref)
	}
	if gotAuth != "Bearer tok_abc" {
		t.Fatalf("auth header = %q", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("content-type = %q", gotContentType)
	}
	if authBody["amount"].(float64) != 500 || authBody["currency"].(string) != "AOA" ||
		authBody["captureMode"].(string) != "manual" || authBody["merchantTransactionId"].(string) != "merchant-tx-1" {
		t.Fatalf("unexpected authorize payload: %v", authBody)
	}

	if err := g.Capture(ctx, ref); err != nil {
		t.Fatalf("capture: %v", err)
	}
	if !captureHit {
		t.Fatal("capture endpoint not called")
	}

	if err := g.Refund(ctx, ref, 25000); err != nil {
		t.Fatalf("refund: %v", err)
	}
	if !refundHit {
		t.Fatal("refund endpoint not called")
	}
	if refundBody["amount"].(float64) != 250 {
		t.Fatalf("unexpected refund payload: %v", refundBody)
	}
}

func TestAppyPayGateway_ErrorMapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"insufficient funds"}`))
	}))
	defer srv.Close()

	g := NewAppyPayGateway(config.AppyPayConfig{Token: "tok", BaseURL: srv.URL})
	money, _ := shared.NewMoney(1000, "AOA")
	_, err := g.Authorize(context.Background(), money, "k")
	if err == nil || err.Error() != "appypay: insufficient funds" {
		t.Fatalf("error = %v, want mapped message", err)
	}
}
