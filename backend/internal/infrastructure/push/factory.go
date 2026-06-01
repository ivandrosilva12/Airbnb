package push

import (
	"log/slog"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/config"
	"github.com/airhost/backend/internal/domain/pushtoken"
)

// NewSender wires a PushSender from configuration. It constructs an FCM
// adapter when a service account JSON is supplied and an APNs adapter when the
// .p8 + team/key/bundle are supplied; either side falls back to a LogSender so
// the platform always has *some* sender that satisfies the port. Web is wired
// through the FCM adapter (Firebase issues web push tokens) when FCM is
// configured, otherwise it logs.
func NewSender(cfg config.PushConfig) port.PushSender {
	byPlatform := map[pushtoken.Platform]port.PushSender{}

	if cfg.FCMServiceAccountJSON != "" {
		fcm, err := NewFCMSender(FCMConfig{ServiceAccountJSON: []byte(cfg.FCMServiceAccountJSON)})
		if err != nil {
			slog.Warn("push: FCM disabled (bad config)", "error", err)
		} else {
			byPlatform[pushtoken.PlatformAndroid] = fcm
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

	if len(byPlatform) == 0 {
		slog.Info("push: no provider configured, using log-only sender")
	}
	return NewMultiplexSender(byPlatform, NewLogSender())
}
