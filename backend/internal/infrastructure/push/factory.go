package push

import (
	"log/slog"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/config"
	"github.com/airhost/backend/internal/domain/pushtoken"
)

// NewSender wires a PushSender from configuration. It constructs an FCM
// adapter when a service account JSON is supplied, an APNs adapter when the
// .p8 + team/key/bundle are supplied, and a native Web Push adapter when
// WEB_PUSH_ENABLED is true with VAPID material — anything else falls back to
// a LogSender so the platform always has *some* sender behind the port.
//
// Precedence: when both FCM (which can carry Firebase web tokens) AND the
// native Web Push sender are configured, the native sender wins for
// platform=web rows because it is the canonical W3C Web Push transport.
func NewSender(cfg config.PushConfig) port.PushSender {
	byPlatform := map[pushtoken.Platform]port.PushSender{}

	if cfg.FCMServiceAccountJSON != "" {
		fcm, err := NewFCMSender(FCMConfig{ServiceAccountJSON: []byte(cfg.FCMServiceAccountJSON)})
		if err != nil {
			slog.Warn("push: FCM disabled (bad config)", "error", err)
		} else {
			byPlatform[pushtoken.PlatformAndroid] = fcm
			// Firebase-issued web tokens still route through FCM. If a
			// native Web Push sender is also configured below, it overrides
			// this entry — that path serves the W3C Web Push protocol the
			// service worker subscribes to via pushManager.subscribe.
			byPlatform[pushtoken.PlatformWeb] = fcm
		}
	}

	if cfg.APNsKeyID != "" && cfg.APNsTeamID != "" && cfg.APNsBundleID != "" && cfg.APNsPrivateKeyPEM != "" {
		base := "https://api.push.apple.com"
		if cfg.APNsUseSandbox {
			base = "https://api.sandbox.push.apple.com"
		}
		apns, err := NewAPNsSender(APNsConfig{
			TeamID:        cfg.APNsTeamID,
			KeyID:         cfg.APNsKeyID,
			PrivateKeyPEM: []byte(cfg.APNsPrivateKeyPEM),
			BundleID:      cfg.APNsBundleID,
			BaseURL:       base,
		})
		if err != nil {
			slog.Warn("push: APNs disabled (bad config)", "error", err)
		} else {
			byPlatform[pushtoken.PlatformIOS] = apns
		}
	}

	if cfg.WebPushEnabled {
		web, err := NewWebPushSender(WebPushConfig{
			Public:  cfg.WebPushPublicKey,
			Private: cfg.WebPushPrivateKey,
			Subject: cfg.WebPushSubject,
		})
		if err != nil {
			slog.Warn("push: Web Push disabled (bad config)", "error", err)
		} else {
			byPlatform[pushtoken.PlatformWeb] = web
			slog.Info("push: Web Push enabled (W3C protocol)")
		}
	}

	if len(byPlatform) == 0 {
		slog.Info("push: no provider configured, using log-only sender")
	}
	return NewMultiplexSender(byPlatform, NewLogSender())
}
