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

// AppyPayGateway is a port.PaymentGateway backed by the AppyPay REST API (an
// Angolan/PALOP processor: Multicaixa Express, references, card).
//
// It authorizes by creating a charge with manual capture and returns the
// charge id as the gateway reference; capture and refund map to the matching
// endpoints. Authentication uses a pre-minted OAuth2 (client-credentials)
// bearer token supplied via configuration.
type AppyPayGateway struct {
	token   string
	baseURL string
	http    *http.Client
}

// NewAppyPayGateway builds an AppyPayGateway from config.
func NewAppyPayGateway(cfg config.AppyPayConfig) *AppyPayGateway {
	base := cfg.BaseURL
	if base == "" {
		base = "https://api.appypay.co.ao/v2.0"
	}
	return &AppyPayGateway{
		token:   cfg.Token,
		baseURL: strings.TrimRight(base, "/"),
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

// Authorize creates a manual-capture charge and returns its id.
func (g *AppyPayGateway) Authorize(ctx context.Context, amount shared.Money, idempotencyKey string) (string, error) {
	payload := map[string]any{
		"amount":                float64(amount.AmountCents()) / 100,
		"currency":              strings.ToUpper(amount.Currency()),
		"captureMode":           "manual",
		"merchantTransactionId": idempotencyKey,
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := g.do(ctx, http.MethodPost, "/charges", payload, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("appypay: empty charge id")
	}
	return out.ID, nil
}

// Capture captures a previously authorized charge in full.
func (g *AppyPayGateway) Capture(ctx context.Context, ref string) error {
	return g.do(ctx, http.MethodPost, "/charges/"+url.PathEscape(ref)+"/capture", map[string]any{}, nil)
}

// Refund refunds amountCents against the charge.
func (g *AppyPayGateway) Refund(ctx context.Context, ref string, amountCents int64) error {
	payload := map[string]any{}
	if amountCents > 0 {
		payload["amount"] = float64(amountCents) / 100
	}
	return g.do(ctx, http.MethodPost, "/charges/"+url.PathEscape(ref)+"/refunds", payload, nil)
}

// do performs a JSON request against the AppyPay API, decoding a successful body
// into out (when non-nil) and mapping API errors to Go errors.
func (g *AppyPayGateway) do(ctx context.Context, method, path string, payload any, out any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, g.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	res, err := g.http.Do(req)
	if err != nil {
		return fmt.Errorf("appypay: request failed: %w", err)
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
			return fmt.Errorf("appypay: %s", e.Message)
		case e.Error != "":
			return fmt.Errorf("appypay: %s", e.Error)
		default:
			return fmt.Errorf("appypay: unexpected status %d", res.StatusCode)
		}
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("appypay: decode response: %w", err)
		}
	}
	return nil
}
