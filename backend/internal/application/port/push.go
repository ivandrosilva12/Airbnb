package port

import (
	"context"

	"github.com/airhost/backend/internal/domain/pushtoken"
)

// PushPayload is the message to deliver to one or more devices. Title and Body
// render in the OS notification tray; Data is application-defined metadata the
// client can use to deep-link (e.g. {"type":"booking","id":"…"}). Keys/values
// must be strings because FCM/APNs/web-push payloads only carry string data.
type PushPayload struct {
	Title string
	Body  string
	Data  map[string]string
}

// PushSendResult reports per-device delivery outcome. Token is the platform
// token sent to; Err is non-nil if the provider rejected the send. Invalid is
// true when the provider says the token is dead and should be deleted (e.g.
// FCM UNREGISTERED, APNs Unregistered/BadDeviceToken) so the caller can prune
// it locally.
type PushSendResult struct {
	Token   pushtoken.Token
	Err     error
	Invalid bool
}

// PushSender is the outbound port for delivering a push notification to one or
// more device tokens. Implementations route per-platform (FCM, APNs, Web Push,
// log) and return one result per device so the caller can prune dead tokens.
// Send must never panic; transient errors are reported per device.
type PushSender interface {
	Send(ctx context.Context, devices []pushtoken.Token, payload PushPayload) []PushSendResult
}
