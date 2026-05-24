package alertstate

import (
	"testing"
	"time"
)

func firing(name, sev string) Alert {
	return Alert{
		Status:      "firing",
		Fingerprint: name + ":" + sev,
		Labels:      map[string]string{"alertname": name, "severity": sev},
		Annotations: map[string]string{"summary": name + " summary", "runbook_url": "http://rb/" + name},
		StartsAt:    time.Now().UTC(),
	}
}

func TestIngestAndList_FiringThenResolved(t *testing.T) {
	s := NewService()
	s.Ingest(Notification{Status: "firing", Alerts: []Alert{firing("AirhostApiDown", "critical")}})

	list := s.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(list))
	}
	if list[0].Status != "firing" || list[0].AlertName != "AirhostApiDown" {
		t.Fatalf("unexpected state: %+v", list[0])
	}
	if list[0].RunbookURL != "http://rb/AirhostApiDown" {
		t.Fatalf("runbook not captured: %+v", list[0])
	}

	// The same alert resolves; the state flips rather than duplicating.
	resolved := firing("AirhostApiDown", "critical")
	resolved.Status = "resolved"
	resolved.EndsAt = time.Now().UTC()
	s.Ingest(Notification{Status: "resolved", Alerts: []Alert{resolved}})

	list = s.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 alert after resolve, got %d", len(list))
	}
	if list[0].Status != "resolved" {
		t.Fatalf("expected resolved, got %q", list[0].Status)
	}
}

func TestList_FiringSortedBeforeResolved(t *testing.T) {
	s := NewService()
	r := firing("AirhostHighErrorRate", "warning")
	r.Status = "resolved"
	s.Ingest(Notification{Alerts: []Alert{r}})
	s.Ingest(Notification{Alerts: []Alert{firing("AirhostApiDown", "critical")}})

	list := s.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 alerts, got %d", len(list))
	}
	if list[0].Status != "firing" {
		t.Fatalf("firing should sort first, got %q", list[0].Status)
	}
}

func TestPrune_ResolvedAgedOut(t *testing.T) {
	s := NewService()
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	r := firing("AirhostApiDown", "critical")
	r.Status = "resolved"
	s.Ingest(Notification{Alerts: []Alert{r}})
	if len(s.List()) != 1 {
		t.Fatal("resolved alert should be visible within retention")
	}

	// Advance beyond the retention window: the resolved alert is pruned.
	s.now = func() time.Time { return now.Add(retainResolved + time.Minute) }
	if len(s.List()) != 0 {
		t.Fatal("resolved alert should be pruned after retention")
	}
}

func TestIngest_FingerprintFallback(t *testing.T) {
	s := NewService()
	a := Alert{
		Status:      "firing",
		Labels:      map[string]string{"alertname": "X", "severity": "warning"},
		Annotations: map[string]string{},
	}
	s.Ingest(Notification{Alerts: []Alert{a}})
	// Re-ingesting the same labels (no fingerprint) must update, not duplicate.
	s.Ingest(Notification{Alerts: []Alert{a}})
	if len(s.List()) != 1 {
		t.Fatalf("expected dedupe by derived fingerprint, got %d", len(s.List()))
	}
}
