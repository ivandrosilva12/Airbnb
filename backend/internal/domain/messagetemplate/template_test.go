package messagetemplate

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

func TestNewTrimsAndValidates(t *testing.T) {
	hostID := uuid.New()
	tpl, err := New(hostID, "  Check-in  ", "  Lockbox code is 1234. Welcome!  ")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if tpl.Label != "Check-in" {
		t.Fatalf("label = %q, want trimmed", tpl.Label)
	}
	if tpl.Body != "Lockbox code is 1234. Welcome!" {
		t.Fatalf("body = %q, want trimmed", tpl.Body)
	}
	if !tpl.IsOwnedBy(hostID) {
		t.Fatalf("IsOwnedBy host = false, want true")
	}
}

func TestNewRejectsInvalidFields(t *testing.T) {
	hostID := uuid.New()
	cases := []struct {
		name        string
		label, body string
		host        uuid.UUID
	}{
		{"empty label", "   ", "body", hostID},
		{"empty body", "label", "  ", hostID},
		{"label too long", strings.Repeat("a", MaxLabelLength+1), "body", hostID},
		{"body too long", "label", strings.Repeat("a", MaxBodyLength+1), hostID},
		{"empty host", "label", "body", uuid.Nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.host, tc.label, tc.body); !errors.Is(err, shared.ErrValidation) {
				t.Fatalf("got %v, want ErrValidation", err)
			}
		})
	}
}

func TestUpdateRevalidatesAndTouches(t *testing.T) {
	tpl, err := New(uuid.New(), "old", "old body")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	first := tpl.UpdatedAt
	time.Sleep(1 * time.Millisecond)
	if err := tpl.Update("new", "new body"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if tpl.Label != "new" || tpl.Body != "new body" {
		t.Fatalf("fields not updated: %+v", tpl)
	}
	if !tpl.UpdatedAt.After(first) {
		t.Fatalf("UpdatedAt should advance on mutation")
	}
	// Re-validation still applies on update.
	if err := tpl.Update("", "x"); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("blank label update: got %v, want ErrValidation", err)
	}
}
