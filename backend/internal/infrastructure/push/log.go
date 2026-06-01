// Package push provides PushSender implementations: a log-only sender used when
// no provider is configured, an HTTP v1 FCM sender for Android (and Web Push
// when the device is registered through Firebase), an APNs sender for iOS, and
// a multiplexer that routes each device token to the right transport.
package push

import (
	"context"
	"log/slog"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/pushtoken"
)

// LogSender writes every push to the application log without contacting any
// external provider. It is the safe default when no credentials are configured
// and is also useful in development to inspect payload shapes.
type LogSender struct{}

// NewLogSender builds a LogSender.
func NewLogSender() *LogSender { return &LogSender{} }

// Send logs every device and returns a non-Invalid, non-Err result so the
// caller does not prune the token.
func (s *LogSender) Send(_ context.Context, devices []pushtoken.Token, payload port.PushPayload) []port.PushSendResult {
	out := make([]port.PushSendResult, 0, len(devices))
	for _, d := range devices {
		slog.Info("push (log sender)", "platform", d.Platform, "token", maskToken(d.Token),
			"title", payload.Title, "body", payload.Body, "data", payload.Data)
		out = append(out, port.PushSendResult{Token: d})
	}
	return out
}

// maskToken trims a push token to its first/last few chars so logs do not leak
// the full token (it can be replayed against the provider in some setups).
func maskToken(t string) string {
	if len(t) <= 12 {
		return "***"
	}
	return t[:4] + "…" + t[len(t)-4:]
}
