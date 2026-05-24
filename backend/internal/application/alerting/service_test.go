package alerting

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/shared"
)

// fakeSilencer records the last created silence and serves canned responses.
type fakeSilencer struct {
	created   port.NewSilence
	createErr error
	listOut   []port.Silence
	listErr   error
	deletedID string
	deleteErr error
}

func (f *fakeSilencer) Create(_ context.Context, s port.NewSilence) (string, error) {
	f.created = s
	if f.createErr != nil {
		return "", f.createErr
	}
	return "sil-123", nil
}
func (f *fakeSilencer) List(context.Context) ([]port.Silence, error) {
	return f.listOut, f.listErr
}
func (f *fakeSilencer) Delete(_ context.Context, id string) error {
	f.deletedID = id
	return f.deleteErr
}

// fixedClock returns a service pinned to a known "now" for deterministic status.
func newAt(t time.Time, f port.AlertSilencer) *Service {
	s := NewService(f)
	s.now = func() time.Time { return t }
	return s
}

func TestCreateSilence_DefaultsAndStatus(t *testing.T) {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	f := &fakeSilencer{}
	s := newAt(now, f)

	out, err := s.CreateSilence(context.Background(), CreateSilenceInput{
		Matchers: []port.SilenceMatcher{{Name: "alertname", Value: "AirhostApiDown", IsEqual: true}},
		Duration: 2 * time.Hour,
		Comment:  "deploy window",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ID != "sil-123" {
		t.Fatalf("expected id sil-123, got %q", out.ID)
	}
	if !out.EndsAt.Equal(now.Add(2 * time.Hour)) {
		t.Fatalf("endsAt should default to now+duration, got %v", out.EndsAt)
	}
	if !out.StartsAt.Equal(now) {
		t.Fatalf("startsAt should default to now, got %v", out.StartsAt)
	}
	if out.Status != "active" {
		t.Fatalf("expected active status, got %q", out.Status)
	}
	if out.CreatedBy != "airhost-admin" {
		t.Fatalf("createdBy should default, got %q", out.CreatedBy)
	}
	// The forwarded silence carries the resolved window.
	if !f.created.EndsAt.Equal(now.Add(2 * time.Hour)) {
		t.Fatalf("forwarded endsAt wrong: %v", f.created.EndsAt)
	}
}

func TestCreateSilence_PendingWhenFutureStart(t *testing.T) {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	s := newAt(now, &fakeSilencer{})
	out, err := s.CreateSilence(context.Background(), CreateSilenceInput{
		Matchers: []port.SilenceMatcher{{Name: "severity", Value: "warning", IsEqual: true}},
		StartsAt: now.Add(time.Hour),
		EndsAt:   now.Add(2 * time.Hour),
		Comment:  "scheduled maintenance",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != "pending" {
		t.Fatalf("expected pending status, got %q", out.Status)
	}
}

func TestCreateSilence_Validation(t *testing.T) {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	cases := map[string]CreateSilenceInput{
		"no matchers": {
			Duration: time.Hour,
			Comment:  "x",
		},
		"matcher without name": {
			Matchers: []port.SilenceMatcher{{Value: "v"}},
			Duration: time.Hour,
			Comment:  "x",
		},
		"matcher without value (not regex)": {
			Matchers: []port.SilenceMatcher{{Name: "alertname"}},
			Duration: time.Hour,
			Comment:  "x",
		},
		"no comment": {
			Matchers: []port.SilenceMatcher{{Name: "alertname", Value: "v"}},
			Duration: time.Hour,
		},
		"no duration or endsAt": {
			Matchers: []port.SilenceMatcher{{Name: "alertname", Value: "v"}},
			Comment:  "x",
		},
		"endsAt before startsAt": {
			Matchers: []port.SilenceMatcher{{Name: "alertname", Value: "v"}},
			StartsAt: now.Add(time.Hour),
			EndsAt:   now,
			Comment:  "x",
		},
		"duration too long": {
			Matchers: []port.SilenceMatcher{{Name: "alertname", Value: "v"}},
			Duration: 31 * 24 * time.Hour,
			Comment:  "x",
		},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			s := newAt(now, &fakeSilencer{})
			_, err := s.CreateSilence(context.Background(), in)
			if !errors.Is(err, shared.ErrValidation) {
				t.Fatalf("expected validation error, got %v", err)
			}
		})
	}
}

func TestCreateSilence_RegexMatcherAllowsEmptyValue(t *testing.T) {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	s := newAt(now, &fakeSilencer{})
	_, err := s.CreateSilence(context.Background(), CreateSilenceInput{
		Matchers: []port.SilenceMatcher{{Name: "instance", Value: ".+", IsRegex: true, IsEqual: true}},
		Duration: time.Hour,
		Comment:  "regex ok",
	})
	if err != nil {
		t.Fatalf("regex matcher should be valid, got %v", err)
	}
}

func TestDeleteSilence(t *testing.T) {
	f := &fakeSilencer{}
	s := NewService(f)

	if err := s.DeleteSilence(context.Background(), "  "); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("blank id should be a validation error, got %v", err)
	}
	if err := s.DeleteSilence(context.Background(), "sil-9"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.deletedID != "sil-9" {
		t.Fatalf("expected delete to forward id, got %q", f.deletedID)
	}
}

func TestListSilences_Passthrough(t *testing.T) {
	f := &fakeSilencer{listOut: []port.Silence{{ID: "a"}, {ID: "b"}}}
	s := NewService(f)
	out, err := s.ListSilences(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 silences, got %d", len(out))
	}
}
