package payment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/airhost/backend/internal/config"
	"github.com/airhost/backend/internal/domain/shared"
)

// GPayAngolaGateway is a port.PaymentGateway backed by the GPay Angola
// (gpayangola.com) REST API — the primary domestic processor for Angola.
//
// It authorizes by creating a payment with manual capture and returns the
// payment id as the gateway reference; capture and refund map to the matching
// endpoints. Authentication uses an API key sent as a bearer token.
type GPayAngolaGateway struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

// NewGPayAngolaGateway builds a GPayAngolaGateway from config.
func NewGPayAngolaGateway(cfg config.GPayAngolaConfig) *GPayAngolaGateway {
	base := cfg.BaseURL
	if base == "" {
		base = "https://api.gpayangola.com"
	}
	return &GPayAngolaGateway{
		apiKey:  cfg.APIKey,
		baseURL: strings.TrimRight(base, "/"),
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

// Authorize creates a manual-capture payment and returns its id.
func (g *GPayAngolaGateway) Authorize(ctx context.Context, amount shared.Money, idempotencyKey string) (string, error) {
	payload := map[string]any{
		"amount":      float64(amount.AmountCents()) / 100,
		"currency":    strings.ToUpper(amount.Currency()),
		"capture":     false, // authorize as a hold; capture later
		"referenceId": idempotencyKey,
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := g.do(ctx, http.MethodPost, "/payments", payload, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("gpayangola: empty payment id")
	}
	return out.ID, nil
}

// Capture captures a previously authorized payment in full.
func (g *GPayAngolaGateway) Capture(ctx context.Context, ref string) error {
	return g.do(ctx, http.MethodPost, "/payments/"+url.PathEscape(ref)+"/capture", map[string]any{}, nil)
}

// Refund refunds amountCents against the payment.
func (g *GPayAngolaGateway) Refund(ctx context.Context, ref string, amountCents int64) error {
	payload := map[string]any{}
	if amountCents > 0 {
		payload["amount"] = float64(amountCents) / 100
	}
	return g.do(ctx, http.MethodPost, "/payments/"+url.PathEscape(ref)+"/refund", payload, nil)
}

// do performs a JSON request against the GPay Angola API, decoding a successful
// body into out (when non-nil) and mapping API errors to Go errors.
func (g *GPayAngolaGateway) do(ctx context.Context, method, path string, payload any, out any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, g.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+g.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	res, err := g.http.Do(req)
	if err != nil {
		return fmt.Errorf("gpayangola: request failed: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	if res.StatusCode >= 400 {
		var e struct {
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		_ = json.Unmarshal(body, &e)
		switch {
		case e.Message != "":
			return fmt.Errorf("gpayangola: %s", e.Message)
		case e.Error != "":
			return fmt.Errorf("gpayangola: %s", e.Error)
		default:
			return fmt.Errorf("gpayangola: unexpected status %d", res.StatusCode)
		}
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("gpayangola: decode response: %w", err)
		}
	}
	return nil
}
