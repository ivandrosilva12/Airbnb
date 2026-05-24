package payment

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/shared"
)

// taggedGateway names an underlying gateway so the routing layer can tag the
// references it returns and route later capture/refund calls back to the same
// provider.
type taggedGateway struct {
	name string
	gw   port.PaymentGateway
}

// RoutingGateway is a composite port.PaymentGateway that chooses an underlying
// gateway per transaction by currency, with ordered failover.
//
//   - Domestic currency (e.g. AOA → Angola): try the domestic chain in order —
//     GPay Angola first, then AppyPay — so a failed primary authorization falls
//     back to the secondary.
//   - Any other currency (payments from abroad): use the foreign chain (Stripe).
//
// Authorize tags the returned reference with the winning gateway's name
// ("<name>:<ref>"); Capture and Refund parse that tag and delegate to the same
// gateway, since a hold can only be settled by the processor that created it.
// The tag is part of the reference persisted with the payment, so routing
// survives restarts.
type RoutingGateway struct {
	domesticCurrency string
	domestic         []taggedGateway
	foreign          []taggedGateway
	byName           map[string]port.PaymentGateway
}

// routingBuilder accumulates the chains before constructing the gateway.
type routingBuilder struct {
	domesticCurrency string
	domestic         []taggedGateway
	foreign          []taggedGateway
}

func newRoutingBuilder(domesticCurrency string) *routingBuilder {
	if strings.TrimSpace(domesticCurrency) == "" {
		domesticCurrency = "AOA"
	}
	return &routingBuilder{domesticCurrency: strings.ToUpper(domesticCurrency)}
}

func (b *routingBuilder) addDomestic(name string, gw port.PaymentGateway) *routingBuilder {
	if gw != nil {
		b.domestic = append(b.domestic, taggedGateway{name: name, gw: gw})
	}
	return b
}

func (b *routingBuilder) addForeign(name string, gw port.PaymentGateway) *routingBuilder {
	if gw != nil {
		b.foreign = append(b.foreign, taggedGateway{name: name, gw: gw})
	}
	return b
}

func (b *routingBuilder) build() *RoutingGateway {
	r := &RoutingGateway{
		domesticCurrency: b.domesticCurrency,
		domestic:         b.domestic,
		foreign:          b.foreign,
		byName:           map[string]port.PaymentGateway{},
	}
	for _, t := range append(append([]taggedGateway{}, b.domestic...), b.foreign...) {
		r.byName[t.name] = t.gw
	}
	return r
}

// Authorize routes by currency and fails over along the selected chain.
func (r *RoutingGateway) Authorize(ctx context.Context, amount shared.Money, idempotencyKey string) (string, error) {
	chain := r.foreign
	if strings.EqualFold(amount.Currency(), r.domesticCurrency) {
		chain = r.domestic
	}
	if len(chain) == 0 {
		return "", fmt.Errorf("payment: no gateway configured for currency %s", amount.Currency())
	}
	var lastErr error
	for _, t := range chain {
		ref, err := t.gw.Authorize(ctx, amount, idempotencyKey)
		if err == nil {
			return t.name + ":" + ref, nil
		}
		lastErr = err
		slog.Warn("payment: authorize failed, trying next gateway", "gateway", t.name, "error", err)
	}
	return "", fmt.Errorf("payment: all gateways failed for currency %s: %w", amount.Currency(), lastErr)
}

// Capture routes to the gateway that authorized the (tagged) reference.
func (r *RoutingGateway) Capture(ctx context.Context, ref string) error {
	gw, raw, err := r.resolve(ref)
	if err != nil {
		return err
	}
	return gw.Capture(ctx, raw)
}

// Refund routes to the gateway that authorized the (tagged) reference.
func (r *RoutingGateway) Refund(ctx context.Context, ref string, amountCents int64) error {
	gw, raw, err := r.resolve(ref)
	if err != nil {
		return err
	}
	return gw.Refund(ctx, raw, amountCents)
}

// resolve splits a tagged reference into its gateway and the provider-native
// reference.
func (r *RoutingGateway) resolve(ref string) (port.PaymentGateway, string, error) {
	name, raw, ok := strings.Cut(ref, ":")
	if !ok {
		return nil, "", fmt.Errorf("payment: untagged gateway reference %q", ref)
	}
	gw, ok := r.byName[name]
	if !ok {
		return nil, "", fmt.Errorf("payment: unknown gateway %q for reference", name)
	}
	return gw, raw, nil
}

var _ port.PaymentGateway = (*RoutingGateway)(nil)
