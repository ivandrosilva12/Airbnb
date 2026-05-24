// Package ical renders and parses a pragmatic subset of iCalendar (RFC 5545)
// sufficient for accommodation availability sync: all-day VEVENTs with DATE
// values, as produced by Airbnb, Booking.com and Google Calendar exports.
package ical

import (
	"fmt"
	"strings"
	"time"
)

// Event is an all-day busy range. End is exclusive (the morning of checkout),
// matching the half-open convention used across the platform.
type Event struct {
	UID     string
	Summary string
	Start   time.Time
	End     time.Time
}

const dateLayout = "20060102"

// Render produces a VCALENDAR document (CRLF line endings) advertising the
// given busy events as all-day VEVENTs.
func Render(calendarName string, events []Event) []byte {
	var b strings.Builder
	w := func(s string) { b.WriteString(s + "\r\n") }

	w("BEGIN:VCALENDAR")
	w("VERSION:2.0")
	w("PRODID:-//AirHost//Calendar//EN")
	w("CALSCALE:GREGORIAN")
	w("METHOD:PUBLISH")
	w("X-WR-CALNAME:" + escapeText(calendarName))
	stamp := time.Now().UTC().Format("20060102T150405Z")
	for i, e := range events {
		uid := e.UID
		if uid == "" {
			uid = fmt.Sprintf("airhost-%d-%s@airhost", i, e.Start.Format(dateLayout))
		}
		w("BEGIN:VEVENT")
		w("UID:" + uid)
		w("DTSTAMP:" + stamp)
		w("DTSTART;VALUE=DATE:" + e.Start.UTC().Format(dateLayout))
		w("DTEND;VALUE=DATE:" + e.End.UTC().Format(dateLayout))
		w("SUMMARY:" + escapeText(e.Summary))
		w("END:VEVENT")
	}
	w("END:VCALENDAR")
	return []byte(b.String())
}

// Parse extracts all-day VEVENTs from an iCalendar document. It tolerates CRLF
// or LF endings and folded lines. Events missing DTEND default to one night.
func Parse(data []byte) ([]Event, error) {
	lines := unfold(string(data))
	var (
		events  []Event
		cur     *Event
		inEvent bool
	)
	for _, line := range lines {
		name, params, value := splitLine(line)
		upper := strings.ToUpper(name)
		switch {
		case upper == "BEGIN" && strings.EqualFold(value, "VEVENT"):
			inEvent = true
			cur = &Event{}
		case upper == "END" && strings.EqualFold(value, "VEVENT"):
			if cur != nil && !cur.Start.IsZero() {
				if cur.End.IsZero() {
					cur.End = cur.Start.AddDate(0, 0, 1)
				}
				events = append(events, *cur)
			}
			inEvent = false
			cur = nil
		case inEvent && cur != nil:
			switch upper {
			case "UID":
				cur.UID = value
			case "SUMMARY":
				cur.Summary = unescapeText(value)
			case "DTSTART":
				if t, ok := parseDate(value, params); ok {
					cur.Start = t
				}
			case "DTEND":
				if t, ok := parseDate(value, params); ok {
					cur.End = t
				}
			}
		}
	}
	return events, nil
}

// unfold joins RFC 5545 continuation lines (those starting with a space or tab)
// onto the preceding line and returns the logical lines.
func unfold(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	raw := strings.Split(s, "\n")
	var out []string
	for _, line := range raw {
		if line == "" {
			continue
		}
		if (line[0] == ' ' || line[0] == '\t') && len(out) > 0 {
			out[len(out)-1] += line[1:]
			continue
		}
		out = append(out, line)
	}
	return out
}

// splitLine separates a content line into its property name, parameter string
// and value (e.g. "DTSTART;VALUE=DATE:20240115").
func splitLine(line string) (name, params, value string) {
	colon := strings.IndexByte(line, ':')
	if colon < 0 {
		return line, "", ""
	}
	left := line[:colon]
	value = line[colon+1:]
	if semi := strings.IndexByte(left, ';'); semi >= 0 {
		return left[:semi], left[semi+1:], value
	}
	return left, "", value
}

// parseDate parses a DATE ("20240115") or DATE-TIME ("20240115T140000Z") value,
// truncated to day precision in UTC.
func parseDate(value, _ string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if idx := strings.IndexByte(value, 'T'); idx >= 0 {
		value = value[:idx]
	}
	if len(value) != 8 {
		return time.Time{}, false
	}
	t, err := time.Parse(dateLayout, value)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

func escapeText(s string) string {
	r := strings.NewReplacer("\\", "\\\\", ";", "\\;", ",", "\\,", "\n", "\\n")
	return r.Replace(s)
}

func unescapeText(s string) string {
	r := strings.NewReplacer("\\n", "\n", "\\,", ",", "\\;", ";", "\\\\", "\\")
	return r.Replace(s)
}
