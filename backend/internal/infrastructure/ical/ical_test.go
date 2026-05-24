package ical_test

import (
	"strings"
	"testing"
	"time"

	"github.com/airhost/backend/internal/infrastructure/ical"
)

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestRenderThenParse_RoundTrips(t *testing.T) {
	events := []ical.Event{
		{UID: "a@airhost", Summary: "Booked", Start: day(2026, 6, 1), End: day(2026, 6, 5)},
		{UID: "b@airhost", Summary: "Blocked", Start: day(2026, 7, 10), End: day(2026, 7, 12)},
	}
	out := ical.Render("Test", events)
	if !strings.Contains(string(out), "BEGIN:VCALENDAR") || !strings.Contains(string(out), "DTSTART;VALUE=DATE:20260601") {
		t.Fatalf("rendered output missing expected lines:\n%s", out)
	}

	parsed, err := ical.Parse(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("parsed %d events, want 2", len(parsed))
	}
	if !parsed[0].Start.Equal(events[0].Start) || !parsed[0].End.Equal(events[0].End) {
		t.Fatalf("event 0 = %+v, want %+v", parsed[0], events[0])
	}
	if parsed[1].Summary != "Blocked" {
		t.Fatalf("event 1 summary = %q, want Blocked", parsed[1].Summary)
	}
}

func TestParse_HandlesExternalFeed(t *testing.T) {
	// Airbnb-style export: CRLF endings, VALUE=DATE all-day events, a folded line.
	feed := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Other//EN\r\n" +
		"BEGIN:VEVENT\r\nUID:ext-1\r\nDTSTART;VALUE=DATE:20260801\r\nDTEND;VALUE=DATE:20260804\r\n" +
		"SUMMARY:Reserved on Other\r\n Platform\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:ext-2\r\nDTSTART:20260901T140000Z\r\nDTEND:20260903T110000Z\r\nSUMMARY:Stay\r\nEND:VEVENT\r\n" +
		"END:VCALENDAR\r\n"

	events, err := ical.Parse([]byte(feed))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("parsed %d events, want 2", len(events))
	}
	if !events[0].Start.Equal(day(2026, 8, 1)) || !events[0].End.Equal(day(2026, 8, 4)) {
		t.Fatalf("event 0 dates = %s..%s", events[0].Start, events[0].End)
	}
	// Folded continuation line should be joined into the summary.
	if events[0].Summary != "Reserved on OtherPlatform" {
		t.Fatalf("folded summary = %q", events[0].Summary)
	}
	// Date-time values are truncated to the day.
	if !events[1].Start.Equal(day(2026, 9, 1)) || !events[1].End.Equal(day(2026, 9, 3)) {
		t.Fatalf("event 1 dates = %s..%s", events[1].Start, events[1].End)
	}
}
