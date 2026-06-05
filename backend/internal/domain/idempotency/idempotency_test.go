package idempotency

import (
	"errors"
	"testing"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

func TestNew_HappyPath(t *testing.T) {
	uid := uuid.New()
	hash := []byte{0x01, 0x02, 0x03}
	body := []byte(`{"ok":true}`)
	r, err := New("k-123", uid, "POST", "/api/v1/bookings", hash, 201, body, "application/json")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if r.Key != "k-123" || r.UserID != uid || r.Method != "POST" || r.Path != "/api/v1/bookings" {
		t.Fatalf("record fields: %+v", r)
	}
	if r.StatusCode != 201 || string(r.ResponseBody) != `{"ok":true}` || r.ResponseContentType != "application/json" {
		t.Fatalf("response fields: %+v", r)
	}
	if string(r.BodyHash) != string(hash) {
		t.Fatalf("body hash mismatch: %v", r.BodyHash)
	}
	if r.CreatedAt.IsZero() {
		t.Fatalf("CreatedAt should be set")
	}
}

func TestNew_Validation(t *testing.T) {
	uid := uuid.New()
	hash := []byte{0x01}
	body := []byte("x")
	cases := []struct {
		name      string
		key       string
		userID    uuid.UUID
		method    string
		path      string
		bodyHash  []byte
		status    int
		wantField string
	}{
		{"empty key", "", uid, "POST", "/p", hash, 200, "key"},
		{"whitespace key", "   ", uid, "POST", "/p", hash, 200, "key"},
		{"nil user", "k", uuid.Nil, "POST", "/p", hash, 200, "userID"},
		{"empty method", "k", uid, "", "/p", hash, 200, "method"},
		{"empty path", "k", uid, "POST", "", hash, 200, "path"},
		{"nil hash", "k", uid, "POST", "/p", nil, 200, "bodyHash"},
		{"zero status", "k", uid, "POST", "/p", hash, 0, "statusCode"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.key, tc.userID, tc.method, tc.path, tc.bodyHash, tc.status, body, "application/json")
			if err == nil {
				t.Fatalf("expected validation error")
			}
			if !errors.Is(err, shared.ErrValidation) {
				t.Fatalf("error not ErrValidation: %v", err)
			}
		})
	}
}
