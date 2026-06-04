package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/pushtoken"
	webpush "github.com/SherClockHolmes/webpush-go"
)

// WebPushConfig holds the VAPID material required to talk to a browser Push
// Service (Mozilla autopush, Google FCM web, Apple push, etc.). Subject is the
// mailto: or https: URL the Push Service uses to reach the application server
// about abuse; the VAPID spec requires it on every signed request.
type WebPushConfig struct {
	Public  string
	Private string
	Subject string
	// TTL bounds how long the Push Service will hold an undelivered message
	// before discarding it (in seconds). 12h matches what other tier-1
	// platforms ship — short enough that a stale browser does not get a
	// flood of redundant notifications on next launch, long enough to cover
	// a normal disconnect window.
	TTL int
	// HTTPClient overrides the HTTP client (tests).
	HTTPClient *http.Client
}

// WebPushSender delivers pushes via the W3C Web Push protocol, authenticated
// with VAPID. Tokens whose Endpoint blob fails to decode are reported as
// invalid so the caller can prune them; HTTP 404/410 from the Push Service
// also mean "this subscription is gone" and surface as Invalid.
type WebPushSender struct {
	options WebPushConfig
	client  *http.Client
}

// NewWebPushSender builds a WebPushSender. Returns an error when the VAPID
// material is missing — the factory wires a LogSender instead when this
// happens so the platform always has *some* sender behind the port.
func NewWebPushSender(cfg WebPushConfig) (*WebPushSender, error) {
	if cfg.Public == "" || cfg.Private == "" {
		return nil, errors.New("webpush: VAPID public/private keys are required")
	}
	if cfg.Subject == "" {
		return nil, errors.New("webpush: VAPID subject (mailto: or https:) is required")
	}
	if cfg.TTL <= 0 {
		cfg.TTL = int((12 * time.Hour).Seconds())
	}
	cli := cfg.HTTPClient
	if cli == nil {
		cli = &http.Client{Timeout: 10 * time.Second}
	}
	return &WebPushSender{options: cfg, client: cli}, nil
}

// webPushKeys mirrors the JSON sent by PushSubscription.toJSON().keys in
// every browser. We persist this blob in push_tokens.endpoint so the sender
// can rebuild the webpush.Subscription on each dispatch.
type webPushKeys struct {
	P256dh string `json:"p256dh"`
	Auth   string `json:"auth"`
}

// Send fans payload out one device at a time — the W3C Web Push protocol is
// a one-recipient-per-call protocol, and per-device errors must not abort
// the rest of the dispatch.
func (s *WebPushSender) Send(ctx context.Context, devices []pushtoken.Token, payload port.PushPayload) []port.PushSendResult {
	out := make([]port.PushSendResult, 0, len(devices))
	for _, d := range devices {
		invalid, err := s.sendOne(ctx, d, payload)
		out = append(out, port.PushSendResult{Token: d, Err: err, Invalid: invalid})
	}
	return out
}

func (s *WebPushSender) sendOne(ctx context.Context, dev pushtoken.Token, payload port.PushPayload) (invalid bool, err error) {
	if dev.Token == "" {
		return true, errors.New("webpush: empty subscription endpoint")
	}
	var keys webPushKeys
	if err := json.Unmarshal([]byte(dev.Endpoint), &keys); err != nil {
		return true, fmt.Errorf("webpush: decode subscription keys: %w", err)
	}
	if keys.P256dh == "" || keys.Auth == "" {
		return true, errors.New("webpush: subscription is missing p256dh/auth")
	}

	sub := &webpush.Subscription{
		Endpoint: dev.Token,
		Keys: webpush.Keys{
			P256dh: keys.P256dh,
			Auth:   keys.Auth,
		},
	}
	body, err := json.Marshal(map[string]any{
		"title": payload.Title,
		"body":  payload.Body,
		"data":  payload.Data,
	})
	if err != nil {
		return false, err
	}

	opts := &webpush.Options{
		HTTPClient:      s.client,
		Subscriber:      s.options.Subject,
		VAPIDPublicKey:  s.options.Public,
		VAPIDPrivateKey: s.options.Private,
		TTL:             s.options.TTL,
	}
	resp, err := webpush.SendNotificationWithContext(ctx, body, sub, opts)
	if err != nil {
		return false, fmt.Errorf("webpush: send: %w", err)
	}
	defer resp.Body.Close()
	// Drain the body so the connection can be reused; bound the read so a
	// rogue Push Service cannot pin memory.
	_, _ = io.CopyN(io.Discard, resp.Body, 4096)

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return false, nil
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		// Subscription was revoked by the user or the Push Service expired
		// it — prune the row locally so we stop dispatching to a dead URL.
		return true, fmt.Errorf("webpush: %s", strings.TrimSpace(resp.Status))
	default:
		return false, fmt.Errorf("webpush: %s", strings.TrimSpace(resp.Status))
	}
}
