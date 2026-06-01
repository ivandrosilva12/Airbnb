package push_test

import (
	"context"
	"testing"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/pushtoken"
	"github.com/airhost/backend/internal/infrastructure/push"
	"github.com/google/uuid"
)

// recordingSender keeps the devices it received so the test can assert routing.
type recordingSender struct {
	name    string
	got     []pushtoken.Token
	invalid map[string]bool
}

func (s *recordingSender) Send(_ context.Context, devices []pushtoken.Token, _ port.PushPayload) []port.PushSendResult {
	s.got = append(s.got, devices...)
	out := make([]port.PushSendResult, 0, len(devices))
	for _, d := range devices {
		out = append(out, port.PushSendResult{Token: d, Invalid: s.invalid[d.Token]})
	}
	return out
}

func TestMultiplex_RoutesByPlatformAndPreservesOrder(t *testing.T) {
	ios := &recordingSender{name: "ios"}
	fcm := &recordingSender{name: "fcm", invalid: map[string]bool{"dead": true}}
	fb := &recordingSender{name: "fallback"}
	mux := push.NewMultiplexSender(map[pushtoken.Platform]port.PushSender{
		pushtoken.PlatformIOS:     ios,
		pushtoken.PlatformAndroid: fcm,
	}, fb)

	devices := []pushtoken.Token{
		{ID: uuid.New(), Platform: pushtoken.PlatformIOS, Token: "ios-1"},
		{ID: uuid.New(), Platform: pushtoken.PlatformAndroid, Token: "dead"},
		{ID: uuid.New(), Platform: pushtoken.PlatformWeb, Token: "web-1"}, // no entry → fallback
		{ID: uuid.New(), Platform: pushtoken.PlatformAndroid, Token: "live"},
	}
	res := mux.Send(context.Background(), devices, port.PushPayload{Title: "t", Body: "b"})

	if len(ios.got) != 1 || ios.got[0].Token != "ios-1" {
		t.Fatalf("ios sender got %#v", ios.got)
	}
	if len(fcm.got) != 2 {
		t.Fatalf("fcm sender expected 2 devices, got %#v", fcm.got)
	}
	if len(fb.got) != 1 || fb.got[0].Token != "web-1" {
		t.Fatalf("fallback sender got %#v", fb.got)
	}
	if len(res) != 4 {
		t.Fatalf("expected 4 results, got %d", len(res))
	}
	// Result order matches input order.
	for i, d := range devices {
		if res[i].Token.Token != d.Token {
			t.Fatalf("result %d: token mismatch %q vs %q", i, res[i].Token.Token, d.Token)
		}
	}
	// The dead token from FCM is reported as invalid.
	if !res[1].Invalid {
		t.Fatalf("expected dead token to be flagged invalid: %#v", res[1])
	}
}

func TestLogSender_AlwaysSucceedsWithoutFlaggingInvalid(t *testing.T) {
	s := push.NewLogSender()
	devices := []pushtoken.Token{{ID: uuid.New(), Platform: pushtoken.PlatformWeb, Token: "abcdefghijklmnop"}}
	res := s.Send(context.Background(), devices, port.PushPayload{Title: "t"})
	if len(res) != 1 || res[0].Invalid || res[0].Err != nil {
		t.Fatalf("unexpected log sender result: %#v", res)
	}
}
