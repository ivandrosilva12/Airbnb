package alerting

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/config"
	"github.com/airhost/backend/internal/domain/shared"
)

func TestAlertmanagerSilencer_Create(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody wirePostSilence

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_ = json.NewEncoder(w).Encode(map[string]string{"silenceID": "abc-123"})
	}))
	defer srv.Close()

	s := NewSilencer(config.AlertingConfig{AlertmanagerURL: srv.URL})
	id, err := s.Create(context.Background(), port.NewSilence{
		Matchers:  []port.SilenceMatcher{{Name: "alertname", Value: "AirhostApiDown", IsEqual: true}},
		StartsAt:  time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
		EndsAt:    time.Date(2026, 5, 24, 14, 0, 0, 0, time.UTC),
		CreatedBy: "ops@airhost.dev",
		Comment:   "deploy",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "abc-123" {
		t.Fatalf("expected id abc-123, got %q", id)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v2/silences" {
		t.Fatalf("unexpected request %s %s", gotMethod, gotPath)
	}
	if len(gotBody.Matchers) != 1 || gotBody.Matchers[0].Name != "alertname" || !gotBody.Matchers[0].IsEqual {
		t.Fatalf("matcher not forwarded correctly: %+v", gotBody.Matchers)
	}
	if gotBody.StartsAt != "2026-05-24T12:00:00Z" || gotBody.EndsAt != "2026-05-24T14:00:00Z" {
		t.Fatalf("times not RFC3339 as expected: %q .. %q", gotBody.StartsAt, gotBody.EndsAt)
	}
}

func TestAlertmanagerSilencer_List(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/silences" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`[
			{"id":"s1","status":{"state":"active"},"createdBy":"ops","comment":"c",
			 "startsAt":"2026-05-24T12:00:00Z","endsAt":"2026-05-24T13:00:00Z",
			 "matchers":[{"name":"alertname","value":"X","isRegex":false,"isEqual":true}]}
		]`))
	}))
	defer srv.Close()

	s := NewSilencer(config.AlertingConfig{AlertmanagerURL: srv.URL})
	out, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 || out[0].ID != "s1" || out[0].Status != "active" {
		t.Fatalf("unexpected list result: %+v", out)
	}
	if len(out[0].Matchers) != 1 || out[0].Matchers[0].Name != "alertname" {
		t.Fatalf("matcher not parsed: %+v", out[0].Matchers)
	}
	if !out[0].StartsAt.Equal(time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("startsAt not parsed: %v", out[0].StartsAt)
	}
}

func TestAlertmanagerSilencer_DeleteNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v2/silence/missing" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	s := NewSilencer(config.AlertingConfig{AlertmanagerURL: srv.URL})
	err := s.Delete(context.Background(), "missing")
	if !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("expected not-found, got %v", err)
	}
}

func TestAlertmanagerSilencer_UpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := NewSilencer(config.AlertingConfig{AlertmanagerURL: srv.URL})
	_, err := s.List(context.Background())
	if err == nil {
		t.Fatal("expected an error for 500 response")
	}
	if errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("500 should not map to not-found: %v", err)
	}
}

func TestNewSilencer_DisabledWhenNoURL(t *testing.T) {
	s := NewSilencer(config.AlertingConfig{AlertmanagerURL: ""})
	if _, err := s.Create(context.Background(), port.NewSilence{}); err == nil {
		t.Fatal("disabled silencer should error on Create")
	}
	if _, err := s.List(context.Background()); err == nil {
		t.Fatal("disabled silencer should error on List")
	}
	if err := s.Delete(context.Background(), "x"); err == nil {
		t.Fatal("disabled silencer should error on Delete")
	}
}
