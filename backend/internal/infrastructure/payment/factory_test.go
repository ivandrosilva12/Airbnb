package payment

import (
	"testing"

	"github.com/airhost/backend/internal/config"
)

func TestNewGateway_Selection(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.PaymentConfig
		want any
	}{
		{"default empty -> fake", config.PaymentConfig{}, &FakeGateway{}},
		{"fake -> fake", config.PaymentConfig{Provider: "fake"}, &FakeGateway{}},
		{"unknown -> fake", config.PaymentConfig{Provider: "wat"}, &FakeGateway{}},
		{"stripe without key -> fake", config.PaymentConfig{Provider: "stripe"}, &FakeGateway{}},
		{"stripe with key -> stripe", config.PaymentConfig{Provider: "Stripe", Stripe: config.StripeConfig{SecretKey: "sk"}}, &StripeGateway{}},
		{"appypay without token -> fake", config.PaymentConfig{Provider: "appypay"}, &FakeGateway{}},
		{"appypay with token -> appypay", config.PaymentConfig{Provider: "APPYPAY", AppyPay: config.AppyPayConfig{Token: "t"}}, &AppyPayGateway{}},
		{"gpayangola without key -> fake", config.PaymentConfig{Provider: "gpayangola"}, &FakeGateway{}},
		{"gpayangola with key -> gpayangola", config.PaymentConfig{Provider: "gpayangola", GPayAngola: config.GPayAngolaConfig{APIKey: "k"}}, &GPayAngolaGateway{}},
		{"auto -> routing", config.PaymentConfig{Provider: "auto"}, &RoutingGateway{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGateway(tc.cfg)
			switch tc.want.(type) {
			case *FakeGateway:
				if _, ok := g.(*FakeGateway); !ok {
					t.Fatalf("got %T, want *FakeGateway", g)
				}
			case *StripeGateway:
				if _, ok := g.(*StripeGateway); !ok {
					t.Fatalf("got %T, want *StripeGateway", g)
				}
			case *AppyPayGateway:
				if _, ok := g.(*AppyPayGateway); !ok {
					t.Fatalf("got %T, want *AppyPayGateway", g)
				}
			case *GPayAngolaGateway:
				if _, ok := g.(*GPayAngolaGateway); !ok {
					t.Fatalf("got %T, want *GPayAngolaGateway", g)
				}
			case *RoutingGateway:
				if _, ok := g.(*RoutingGateway); !ok {
					t.Fatalf("got %T, want *RoutingGateway", g)
				}
			}
		})
	}
}
