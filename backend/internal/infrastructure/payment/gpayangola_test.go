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

func TestGPayAngolaGateway_AuthorizeCaptureRefund(t *testing.T) {
	ctx := context.Background()

	var gotAuth string
	var authBody, refundBody map[string]any
	var captureHit, refundHit bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.URL.Path == "/payments" && r.Method == http.MethodPost:
			_ = json.Unmarshal(body, &authBody)
			_, _ = w.Write([]byte(`{"id":"gp_77","status":"authorized"}`))
		case r.URL.Path == "/payments/gp_77/capture":
			captureHit = true
			_, _ = w.Write([]byte(`{"id":"gp_77","status":"captured"}`))
		case r.URL.Path == "/payments/gp_77/refund":
			refundHit = true
			_ = json.Unmarshal(body, &refundBody)
			_, _ = w.Write([]byte(`{"id":"gp_77","status":"refunded"}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	g := NewGPayAngolaGateway(config.GPayAngolaConfig{APIKey: "key_x", BaseURL: srv.URL})
	money, _ := shared.NewMoney(75000, "AOA") // 750.00 AOA

	ref, err := g.Authorize(ctx, money, "ref-1")
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if ref != "gp_77" {
		t.Fatalf("ref = %q, want gp_77", ref)
	}
	if gotAuth != "Bearer key_x" {
		t.Fatalf("auth header = %q", gotAuth)
	}
	if authBody["amount"].(float64) != 750 || authBody["currency"].(string) != "AOA" ||
		authBody["capture"].(bool) != false || authBody["referenceId"].(string) != "ref-1" {
		t.Fatalf("unexpected authorize payload: %v", authBody)
	}

	if err := g.Capture(ctx, ref); err != nil {
		t.Fatalf("capture: %v", err)
	}
	if !captureHit {
		t.Fatal("capture endpoint not called")
	}

	if err := g.Refund(ctx, ref, 30000); err != nil {
		t.Fatalf("refund: %v", err)
	}
	if !refundHit {
		t.Fatal("refund endpoint not called")
	}
	if refundBody["amount"].(float64) != 300 {
		t.Fatalf("unexpected refund payload: %v", refundBody)
	}
}

func TestGPayAngolaGateway_ErrorMapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"gateway timeout"}`))
	}))
	defer srv.Close()

	g := NewGPayAngolaGateway(config.GPayAngolaConfig{APIKey: "k", BaseURL: srv.URL})
	money, _ := shared.NewMoney(1000, "AOA")
	_, err := g.Authorize(context.Background(), money, "k")
	if err == nil || err.Error() != "gpayangola: gateway timeout" {
		t.Fatalf("error = %v, want mapped message", err)
	}
}
