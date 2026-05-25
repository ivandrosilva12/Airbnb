package payment

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/airhost/backend/internal/config"
	"github.com/airhost/backend/internal/domain/shared"
)

// StripeGateway is a port.PaymentGateway backed by the Stripe REST API.
//
// The booking flow authorizes server-side with only an amount, so the adapter
// creates a PaymentIntent with capture_method=manual (a hold) and returns its
// id as the gateway reference. Capture and refund map to the corresponding
// Stripe endpoints. In a full integration the client would confirm the intent
// with a collected payment method before capture; that step is out of scope of
// this server-to-server port.
type StripeGateway struct {
	secretKey string
	baseURL   string
	http      *http.Client
}

// NewStripeGateway builds a StripeGateway from config.
func NewStripeGateway(cfg config.StripeConfig) *StripeGateway {
	base := cfg.BaseURL
	if base == "" {
		base = "https://api.stripe.com"
	}
	return &StripeGateway{
		secretKey: cfg.SecretKey,
		baseURL:   strings.TrimRight(base, "/"),
		http:      &http.Client{Timeout: 15 * time.Second},
	}
}

// Authorize creates a manual-capture PaymentIntent and returns its id.
func (g *StripeGateway) Authorize(ctx context.Context, amount shared.Money, idempotencyKey string) (string, error) {
	form := url.Values{}
	form.Set("amount", strconv.FormatInt(amount.AmountCents(), 10))
	form.Set("currency", strings.ToLower(amount.Currency()))
	form.Set("capture_method", "manual")
	form.Add("payment_method_types[]", "card")

	var out struct {
		ID string `json:"id"`
	}
	if err := g.do(ctx, "/v1/payment_intents", form, idempotencyKey, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("stripe: empty payment intent id")
	}
	return out.ID, nil
}

// Capture captures a previously authorized PaymentIntent in full.
func (g *StripeGateway) Capture(ctx context.Context, ref string) error {
	return g.do(ctx, "/v1/payment_intents/"+url.PathEscape(ref)+"/capture", url.Values{}, "", nil)
}

// Refund refunds amountCents against the PaymentIntent.
func (g *StripeGateway) Refund(ctx context.Context, ref string, amountCents int64) error {
	form := url.Values{}
	form.Set("payment_intent", ref)
	if amountCents > 0 {
		form.Set("amount", strconv.FormatInt(amountCents, 10))
	}
	return g.do(ctx, "/v1/refunds", form, "", nil)
}

// do performs a form-encoded POST against the Stripe API, decoding a successful
// JSON body into out (when non-nil) and mapping API errors to Go errors.
func (g *StripeGateway) do(ctx context.Context, path string, form url.Values, idempotencyKey string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	return g.send(req, out)
}

// get performs a GET against the Stripe API, decoding the JSON body into out.
func (g *StripeGateway) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.baseURL+path, nil)
	if err != nil {
		return err
	}
	return g.send(req, out)
}

// send authenticates the request, executes it, and maps Stripe API errors.
func (g *StripeGateway) send(req *http.Request, out any) error {
	req.Header.Set("Authorization", "Bearer "+g.secretKey)

	res, err := g.http.Do(req)
	if err != nil {
		return fmt.Errorf("stripe: request failed: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	if res.StatusCode >= 400 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &e)
		if e.Error.Message != "" {
			return fmt.Errorf("stripe: %s", e.Error.Message)
		}
		return fmt.Errorf("stripe: unexpected status %d", res.StatusCode)
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("stripe: decode response: %w", err)
		}
	}
	return nil
}
