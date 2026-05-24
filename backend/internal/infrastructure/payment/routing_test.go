package payment

import (
	"context"
	"errors"
	"testing"

	"github.com/airhost/backend/internal/domain/shared"
)

// stubGateway is a configurable port.PaymentGateway for routing tests. It
// records the calls it received and can be made to fail authorization.
type stubGateway struct {
	ref        string
	authErr    error
	authorized []string // idempotency keys seen
	captured   []string // refs seen
	refunded   []string // refs seen
}

func (s *stubGateway) Authorize(_ context.Context, _ shared.Money, idem string) (string, error) {
	if s.authErr != nil {
		return "", s.authErr
	}
	s.authorized = append(s.authorized, idem)
	return s.ref, nil
}

func (s *stubGateway) Capture(_ context.Context, ref string) error {
	s.captured = append(s.captured, ref)
	return nil
}

func (s *stubGateway) Refund(_ context.Context, ref string, _ int64) error {
	s.refunded = append(s.refunded, ref)
	return nil
}

func aoa(cents int64) shared.Money { m, _ := shared.NewMoney(cents, "AOA"); return m }
func usd(cents int64) shared.Money { m, _ := shared.NewMoney(cents, "USD"); return m }

func TestRouting_DomesticUsesPrimaryThenPinsCaptureRefund(t *testing.T) {
	ctx := context.Background()
	gpay := &stubGateway{ref: "gp_1"}
	appy := &stubGateway{ref: "ap_1"}
	stripe := &stubGateway{ref: "pi_1"}

	r := newRoutingBuilder("AOA").
		addDomestic(nameGPayAngola, gpay).
		addDomestic(nameAppyPay, appy).
		addForeign(nameStripe, stripe).
		build()

	ref, err := r.Authorize(ctx, aoa(50000), "idem-1")
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if ref != "gpayangola:gp_1" {
		t.Fatalf("ref = %q, want gpayangola:gp_1", ref)
	}
	if len(gpay.authorized) != 1 || len(appy.authorized) != 0 || len(stripe.authorized) != 0 {
		t.Fatalf("primary should be the only one authorized: gpay=%v appy=%v stripe=%v",
			gpay.authorized, appy.authorized, stripe.authorized)
	}

	// Capture/refund must go back to GPay Angola with the untagged native ref.
	if err := r.Capture(ctx, ref); err != nil {
		t.Fatalf("capture: %v", err)
	}
	if err := r.Refund(ctx, ref, 1000); err != nil {
		t.Fatalf("refund: %v", err)
	}
	if len(gpay.captured) != 1 || gpay.captured[0] != "gp_1" {
		t.Fatalf("gpay captured = %v, want [gp_1]", gpay.captured)
	}
	if len(gpay.refunded) != 1 || gpay.refunded[0] != "gp_1" {
		t.Fatalf("gpay refunded = %v, want [gp_1]", gpay.refunded)
	}
	if len(appy.captured) != 0 {
		t.Fatalf("appypay should not have been touched: %v", appy.captured)
	}
}

func TestRouting_DomesticFailsOverToAppyPay(t *testing.T) {
	ctx := context.Background()
	gpay := &stubGateway{authErr: errors.New("gpay down")}
	appy := &stubGateway{ref: "ap_2"}

	r := newRoutingBuilder("AOA").
		addDomestic(nameGPayAngola, gpay).
		addDomestic(nameAppyPay, appy).
		build()

	ref, err := r.Authorize(ctx, aoa(20000), "idem-2")
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if ref != "appypay:ap_2" {
		t.Fatalf("ref = %q, want appypay:ap_2 (failover)", ref)
	}
	if err := r.Capture(ctx, ref); err != nil {
		t.Fatalf("capture: %v", err)
	}
	if len(appy.captured) != 1 || appy.captured[0] != "ap_2" {
		t.Fatalf("appypay captured = %v, want [ap_2]", appy.captured)
	}
}

func TestRouting_ForeignCurrencyUsesStripe(t *testing.T) {
	ctx := context.Background()
	gpay := &stubGateway{ref: "gp_3"}
	stripe := &stubGateway{ref: "pi_3"}

	r := newRoutingBuilder("AOA").
		addDomestic(nameGPayAngola, gpay).
		addForeign(nameStripe, stripe).
		build()

	ref, err := r.Authorize(ctx, usd(9900), "idem-3")
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if ref != "stripe:pi_3" {
		t.Fatalf("ref = %q, want stripe:pi_3", ref)
	}
	if len(gpay.authorized) != 0 {
		t.Fatalf("domestic gateway should not be used for USD: %v", gpay.authorized)
	}
}

func TestRouting_AllDomesticFail(t *testing.T) {
	ctx := context.Background()
	gpay := &stubGateway{authErr: errors.New("gpay down")}
	appy := &stubGateway{authErr: errors.New("appy down")}

	r := newRoutingBuilder("AOA").
		addDomestic(nameGPayAngola, gpay).
		addDomestic(nameAppyPay, appy).
		build()

	if _, err := r.Authorize(ctx, aoa(1000), "idem-4"); err == nil {
		t.Fatal("expected an error when all domestic gateways fail")
	}
}

func TestRouting_ResolveRejectsUnknownAndUntagged(t *testing.T) {
	r := newRoutingBuilder("AOA").addDomestic(nameGPayAngola, &stubGateway{}).build()
	if err := r.Capture(context.Background(), "no-tag-here"); err == nil {
		t.Fatal("expected error for untagged reference")
	}
	if err := r.Capture(context.Background(), "stripe:pi_x"); err == nil {
		t.Fatal("expected error for unknown gateway tag")
	}
}
