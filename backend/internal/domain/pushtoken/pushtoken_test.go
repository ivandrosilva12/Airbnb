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
