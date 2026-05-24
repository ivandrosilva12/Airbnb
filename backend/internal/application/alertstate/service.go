// Package alertstate keeps an in-memory view of the latest alert states pushed
// by Alertmanager (firing and resolved), so the internal UI can reflect the
// resolved state too — not just e-mail and Slack. It is a display cache, not a
// source of truth: it is rebuilt from Alertmanager's repeated notifications.
package alertstate

import (
	"sort"
	"sync"
	"time"
)

// retainResolved is how long a resolved alert remains visible before it is
// pruned from the view.
const retainResolved = 1 * time.Hour

// State is the latest known state of a single alert.
type State struct {
	Fingerprint string
	AlertName   string
	Severity    string
	Status      string // "firing" | "resolved"
	Summary     string
	Description string
	RunbookURL  string
	StartsAt    time.Time
	EndsAt      time.Time
	UpdatedAt   time.Time
}

// Alert is one alert inside an Alertmanager notification.
type Alert struct {
	Status      string
	Fingerprint string
	Labels      map[string]string
	Annotations map[string]string
	StartsAt    time.Time
	EndsAt      time.Time
}

// Notification is the (subset of the) Alertmanager webhook payload we consume.
type Notification struct {
	Status string
	Alerts []Alert
}

// Service stores the latest state per alert fingerprint.
type Service struct {
	mu  sync.Mutex
	by  map[string]State
	now func() time.Time
}

// NewService builds an empty alert-state cache.
func NewService() *Service {
	return &Service{by: map[string]State{}, now: time.Now}
}

// Ingest folds an Alertmanager notification into the cache, then prunes
// resolved alerts that have aged out. The per-alert status is authoritative;
// the envelope status is only a fallback.
func (s *Service) Ingest(n Notification) {
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, a := range n.Alerts {
		status := a.Status
		if status == "" {
			status = n.Status
		}
		fp := a.Fingerprint
		if fp == "" {
			fp = fingerprintFor(a.Labels)
		}
		s.by[fp] = State{
			Fingerprint: fp,
			AlertName:   a.Labels["alertname"],
			Severity:    a.Labels["severity"],
			Status:      status,
			Summary:     a.Annotations["summary"],
			Description: a.Annotations["description"],
			RunbookURL:  a.Annotations["runbook_url"],
			StartsAt:    a.StartsAt,
			EndsAt:      a.EndsAt,
			UpdatedAt:   now,
		}
	}
	s.pruneLocked(now)
}

// List returns the current alert states: firing alerts first (most recently
// updated first), then resolved alerts still within the retention window.
func (s *Service) List() []State {
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)

	out := make([]State, 0, len(s.by))
	for _, st := range s.by {
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool {
		fi, fj := out[i].Status == "firing", out[j].Status == "firing"
		if fi != fj {
			return fi // firing before resolved
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

// pruneLocked drops resolved alerts whose last update is older than the
// retention window. Callers must hold the mutex.
func (s *Service) pruneLocked(now time.Time) {
	for fp, st := range s.by {
		if st.Status == "resolved" && now.Sub(st.UpdatedAt) > retainResolved {
			delete(s.by, fp)
		}
	}
}

// fingerprintFor builds a stable key from a label set when Alertmanager does not
// supply a fingerprint (e.g. in tests).
func fingerprintFor(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b []byte
	for _, k := range keys {
		b = append(b, k...)
		b = append(b, '=')
		b = append(b, labels[k]...)
		b = append(b, ';')
	}
	return string(b)
}
