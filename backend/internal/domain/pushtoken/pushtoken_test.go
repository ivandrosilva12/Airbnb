package pushtoken_test

import (
	"testing"

	"github.com/airhost/backend/internal/domain/pushtoken"
	"github.com/google/uuid"
)

func TestNew_ValidatesInputs(t *testing.T) {
	user := uuid.New()

	cases := []struct {
		name     string
		user     uuid.UUID
		platform pushtoken.Platform
		token    string
		wantErr  bool
	}{
		{"happy path", user, pushtoken.PlatformAndroid, "abc", false},
		{"missing user", uuid.Nil, pushtoken.PlatformAndroid, "abc", true},
		{"bad platform", user, pushtoken.Platform("blackberry"), "abc", true},
		{"empty token", user, pushtoken.PlatformIOS, "  ", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tok, err := pushtoken.New(tc.user, tc.platform, tc.token)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (token=%+v)", tok)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tok.UserID != tc.user || tok.Platform != tc.platform || tok.Token != "abc" {
				t.Fatalf("unexpected token: %+v", tok)
			}
			if tok.ID == uuid.Nil {
				t.Fatalf("expected generated id")
			}
		})
	}
}

func TestTouch_UpdatesLastSeen(t *testing.T) {
	tok, err := pushtoken.New(uuid.New(), pushtoken.PlatformIOS, "x")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	prev := tok.LastSeen
	tok.LastSeen = prev.Add(-1)
	tok.Touch()
	if !tok.LastSeen.After(prev.Add(-1)) {
		t.Fatalf("expected LastSeen to advance, got %v", tok.LastSeen)
	}
}

// TestWithEndpoint_RoundTripsWebSubscription verifies the JSON keys blob can be
// stashed on the aggregate and survives a trim. The Web Push sender reads it
// back via json.Unmarshal so the test mirrors the production shape.
func TestWithEndpoint_RoundTripsWebSubscription(t *testing.T) {
	tok, err := pushtoken.New(uuid.New(), pushtoken.PlatformWeb,
		"https://fcm.googleapis.com/fcm/send/abc")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	tok.WithEndpoint(`  {"p256dh":"AAA","auth":"BBB"}  `)
	if tok.Endpoint != `{"p256dh":"AAA","auth":"BBB"}` {
		t.Fatalf("expected endpoint to be trimmed, got %q", tok.Endpoint)
	}
}
