// Package alerting provides infrastructure adapters for the alerting stack —
// currently an Alertmanager-backed implementation of the AlertSilencer port.
package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/config"
	"github.com/airhost/backend/internal/domain/shared"
)

// AlertmanagerSilencer manages silences through the Alertmanager v2 REST API.
type AlertmanagerSilencer struct {
	baseURL string
	http    *http.Client
}

// NewSilencer builds an AlertSilencer from config. When no Alertmanager URL is
// configured it returns a disabled silencer whose operations fail cleanly,
// mirroring how the payment layer falls back when a provider is unconfigured.
func NewSilencer(cfg config.AlertingConfig) port.AlertSilencer {
	base := strings.TrimRight(cfg.AlertmanagerURL, "/")
	if base == "" {
		return disabledSilencer{}
	}
	return &AlertmanagerSilencer{
		baseURL: base,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// --- wire types (Alertmanager v2 API) ---------------------------------------

type wireMatcher struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsRegex bool   `json:"isRegex"`
	IsEqual bool   `json:"isEqual"`
}

type wirePostSilence struct {
	Matchers  []wireMatcher `json:"matchers"`
	StartsAt  string        `json:"startsAt"`
	EndsAt    string        `json:"endsAt"`
	CreatedBy string        `json:"createdBy"`
	Comment   string        `json:"comment"`
}

type wireGettableSilence struct {
	ID        string        `json:"id"`
	Matchers  []wireMatcher `json:"matchers"`
	StartsAt  string        `json:"startsAt"`
	EndsAt    string        `json:"endsAt"`
	CreatedBy string        `json:"createdBy"`
	Comment   string        `json:"comment"`
	Status    struct {
		State string `json:"state"`
	} `json:"status"`
}

// Create posts a new silence and returns its id.
func (a *AlertmanagerSilencer) Create(ctx context.Context, s port.NewSilence) (string, error) {
	body := wirePostSilence{
		Matchers:  toWireMatchers(s.Matchers),
		StartsAt:  s.StartsAt.UTC().Format(time.RFC3339),
		EndsAt:    s.EndsAt.UTC().Format(time.RFC3339),
		CreatedBy: s.CreatedBy,
		Comment:   s.Comment,
	}
	var out struct {
		SilenceID string `json:"silenceID"`
	}
	if err := a.do(ctx, http.MethodPost, "/api/v2/silences", body, &out); err != nil {
		return "", err
	}
	if out.SilenceID == "" {
		return "", fmt.Errorf("alertmanager: empty silence id in response")
	}
	return out.SilenceID, nil
}

// List returns the current silences.
func (a *AlertmanagerSilencer) List(ctx context.Context) ([]port.Silence, error) {
	var raw []wireGettableSilence
	if err := a.do(ctx, http.MethodGet, "/api/v2/silences", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]port.Silence, 0, len(raw))
	for _, w := range raw {
		out = append(out, port.Silence{
			ID:        w.ID,
			Matchers:  fromWireMatchers(w.Matchers),
			StartsAt:  parseTime(w.StartsAt),
			EndsAt:    parseTime(w.EndsAt),
			CreatedBy: w.CreatedBy,
			Comment:   w.Comment,
			Status:    w.Status.State,
		})
	}
	return out, nil
}

// Delete expires the silence with the given id.
func (a *AlertmanagerSilencer) Delete(ctx context.Context, id string) error {
	return a.do(ctx, http.MethodDelete, "/api/v2/silence/"+id, nil, nil)
}

// do performs a JSON request against the Alertmanager API. A 404 maps to the
// shared not-found sentinel; other non-2xx responses become plain errors that
// the interface layer reports as an upstream (502) failure.
func (a *AlertmanagerSilencer) do(ctx context.Context, method, path string, payload, out any) error {
	var reader io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, reader)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	res, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("alertmanager: request failed: %w", err)
	}
	defer res.Body.Close()
	respBody, _ := io.ReadAll(res.Body)

	if res.StatusCode == http.StatusNotFound {
		return fmt.Errorf("alertmanager: silence not found: %w", shared.ErrNotFound)
	}
	if res.StatusCode >= 400 {
		msg := strings.TrimSpace(string(respBody))
		if msg == "" {
			msg = http.StatusText(res.StatusCode)
		}
		return fmt.Errorf("alertmanager: unexpected status %d: %s", res.StatusCode, msg)
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("alertmanager: decode response: %w", err)
		}
	}
	return nil
}

func toWireMatchers(in []port.SilenceMatcher) []wireMatcher {
	out := make([]wireMatcher, 0, len(in))
	for _, m := range in {
		out = append(out, wireMatcher{Name: m.Name, Value: m.Value, IsRegex: m.IsRegex, IsEqual: m.IsEqual})
	}
	return out
}

func fromWireMatchers(in []wireMatcher) []port.SilenceMatcher {
	out := make([]port.SilenceMatcher, 0, len(in))
	for _, m := range in {
		out = append(out, port.SilenceMatcher{Name: m.Name, Value: m.Value, IsRegex: m.IsRegex, IsEqual: m.IsEqual})
	}
	return out
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// disabledSilencer is used when no Alertmanager URL is configured; every
// operation reports the feature as unavailable rather than panicking.
type disabledSilencer struct{}

func (disabledSilencer) Create(context.Context, port.NewSilence) (string, error) {
	return "", errAlertingDisabled
}
func (disabledSilencer) List(context.Context) ([]port.Silence, error) {
	return nil, errAlertingDisabled
}
func (disabledSilencer) Delete(context.Context, string) error { return errAlertingDisabled }

var errAlertingDisabled = fmt.Errorf("alerting: no Alertmanager configured (set ALERTMANAGER_URL)")
