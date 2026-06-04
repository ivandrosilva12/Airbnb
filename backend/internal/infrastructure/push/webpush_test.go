package push_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/pushtoken"
	"github.com/airhost/backend/internal/infrastructure/push"
	"github.com/google/uuid"
)

// A real VAPID key pair (publicly known, generated for tests only — NEVER use
// in production). Required because webpush-go validates the keys at marshal.
const (
	testVAPIDPublic  = "BLrPS5W6m6ld5d0XlIp9pWZh4Q1zVa6cFZjJp5b4FuvA9pTQwR-q6oZ5RmXLW1hZSv-iE9bF5tZKgkZHaUcZbN8"
	testVAPIDPrivate = "T2EZHmuiv0g8mrkv6r07e9p2C-AhO9w83vu7tFexkrA"
)

func TestNewWebPushSender_RequiresVAPIDMaterial(t *testing.T) {
	if _, err := push.NewWebPushSender(push.WebPushConfig{}); err == nil {
		t.Fatalf("expected error with empty config")
	}
	if _, err := push.NewWebPushSender(push.WebPushConfig{
		Public: testVAPIDPublic, Private: testVAPIDPrivate,
	}); err == nil {
		t.Fatalf("expected error with missing subject")
	}
	if _, err := push.NewWebPushSender(push.WebPushConfig{
		Public: testVAPIDPublic, Private: testVAPIDPrivate, Subject: "mailto:t@x",
	}); err != nil {
		t.Fatalf("unexpected error with valid config: %v", err)
	}
}

func TestWebPushSender_FlagsMalformedSubscriptionInvalid(t *testing.T) {
	s, err := push.NewWebPushSender(push.WebPushConfig{
		Public: testVAPIDPublic, Private: testVAPIDPrivate, Subject: "mailto:t@x",
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	// Endpoint is empty JSON / missing keys → must surface as Invalid so the
	// caller prunes the row instead of retrying forever.
	devices := []pushtoken.Token{
		{ID: uuid.New(), Platform: pushtoken.PlatformWeb, Token: "https://push/abc", Endpoint: "not-json"},
		{ID: uuid.New(), Platform: pushtoken.PlatformWeb, Token: "https://push/def", Endpoint: `{"p256dh":"","auth":""}`},
		{ID: uuid.New(), Platform: pushtoken.PlatformWeb, Token: "", Endpoint: ""},
	}
	res := s.Send(context.Background(), devices, port.PushPayload{Title: "t"})
	if len(res) != 3 {
		t.Fatalf("expected 3 results, got %d", len(res))
	}
	for i, r := range res {
		if !r.Invalid {
			t.Fatalf("result %d: expected Invalid=true, got %+v", i, r)
		}
	}
}

// TestWebPushSender_PrunesOnGone confirms the sender flags a subscription
// invalid when the Push Service returns 410 Gone (the canonical signal that
// the user revoked the subscription).
func TestWebPushSender_PrunesOnGone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer srv.Close()

	s, err := push.NewWebPushSender(push.WebPushConfig{
		Public: testVAPIDPublic, Private: testVAPIDPrivate, Subject: "mailto:t@x",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	// p256dh / auth are the canonical browser values from a real subscription
	// (89-char and 22-char base64-url respectively) so the library does not
	// reject the input before reaching the HTTP layer.
	dev := pushtoken.Token{
		ID:       uuid.New(),
		Platform: pushtoken.PlatformWeb,
		Token:    srv.URL + "/push/sub-1",
		Endpoint: `{"p256dh":"BNcRdreALRFXTkOOUHK1EtK2wtaz5Ry4YfYCA_0QTpQtUbVlUls0VJXg7A8u-Ts1XbjhazAkj7I99e8QcYP7DkM","auth":"k8JV6sjdbhAi1n3_LDBLvA"}`,
	}
	res := s.Send(context.Background(), []pushtoken.Token{dev}, port.PushPayload{Title: "t", Body: "b"})
	if len(res) != 1 || !res[0].Invalid {
		t.Fatalf("expected Gone to flag Invalid, got %+v", res)
	}
}
