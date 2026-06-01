package push

import (
	"context"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/pushtoken"
)

// MultiplexSender dispatches each device to a per-platform sender so iOS goes
// to APNs and Android/Web goes to FCM/Web Push. Platforms without a registered
// sender fall back to Fallback (typically a LogSender), keeping the contract
// "every token gets a result" intact.
type MultiplexSender struct {
	byPlatform map[pushtoken.Platform]port.PushSender
	fallback   port.PushSender
}

// NewMultiplexSender builds a MultiplexSender. fallback is used for platforms
// without an explicit entry; nil collapses to a LogSender.
func NewMultiplexSender(byPlatform map[pushtoken.Platform]port.PushSender, fallback port.PushSender) *MultiplexSender {
	if fallback == nil {
		fallback = NewLogSender()
	}
	if byPlatform == nil {
		byPlatform = map[pushtoken.Platform]port.PushSender{}
	}
	return &MultiplexSender{byPlatform: byPlatform, fallback: fallback}
}

// Send groups devices by platform, dispatches each subset, and re-assembles
// the per-device results in input order.
func (m *MultiplexSender) Send(ctx context.Context, devices []pushtoken.Token, payload port.PushPayload) []port.PushSendResult {
	if len(devices) == 0 {
		return nil
	}
	groups := map[port.PushSender][]pushtoken.Token{}
	chosen := make([]port.PushSender, len(devices))
	for i, d := range devices {
		s, ok := m.byPlatform[d.Platform]
		if !ok {
			s = m.fallback
		}
		chosen[i] = s
		groups[s] = append(groups[s], d)
	}
	// Per-sender result lookup keyed on (platform, token).
	type key struct{ platform, token string }
	lookup := map[key]port.PushSendResult{}
	for sender, group := range groups {
		for _, r := range sender.Send(ctx, group, payload) {
			lookup[key{string(r.Token.Platform), r.Token.Token}] = r
		}
	}
	out := make([]port.PushSendResult, len(devices))
	for i, d := range devices {
		if r, ok := lookup[key{string(d.Platform), d.Token}]; ok {
			out[i] = r
		} else {
			out[i] = port.PushSendResult{Token: d}
		}
	}
	return out
}
