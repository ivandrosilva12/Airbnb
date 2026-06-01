package property

import (
	"strings"
	"testing"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// mustProperty builds a minimally-valid draft Property for tests in this
// package. Only the fields the test cares about need to be touched after.
func mustProperty(t *testing.T) *Property {
	t.Helper()
	price, _ := shared.NewMoney(5000, "EUR")
	cleaning, _ := shared.NewMoney(0, "EUR")
	addr := Address{City: "Lisboa", Country: "PT", Latitude: 38.7, Longitude: -9.1}
	p, err := NewProperty(uuid.New(), "Test listing", "", TypeApartment, addr, price, cleaning, 2, 1, 1, 1, nil)
	if err != nil {
		t.Fatalf("new property: %v", err)
	}
	return p
}

// arrivalAnchor pins the visibility-window tests to a deterministic moment so
// the table cases don't drift with the wall clock. The dates also dodge any
// DST surprises (the math is purely h-from-checkIn).
var arrivalAnchor = time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

func TestArrivalVisibleAt_WindowBoundaries(t *testing.T) {
	checkIn := arrivalAnchor.AddDate(0, 0, 3)               // +3d
	checkOut := arrivalAnchor.AddDate(0, 0, 5)              // +5d
	revealOpens := checkIn.Add(-ArrivalRevealWindow)        // 48h before check-in

	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"way before reveal", arrivalAnchor, false},
		{"49h before check-in", checkIn.Add(-49 * time.Hour), false},
		{"exactly at reveal opens", revealOpens, true},
		{"1h after reveal opens", revealOpens.Add(time.Hour), true},
		{"at check-in moment", checkIn, true},
		{"mid-stay", checkIn.Add(24 * time.Hour), true},
		{"just before check-out", checkOut.Add(-time.Minute), true},
		{"at check-out", checkOut, false},
		{"after check-out", checkOut.Add(2 * time.Hour), false},
	}
	for _, c := range cases {
		if got := ArrivalVisibleAt(c.now, checkIn, checkOut); got != c.want {
			t.Errorf("%s: ArrivalVisibleAt = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestArrivalVisibleAt_ZeroDatesHide(t *testing.T) {
	if ArrivalVisibleAt(arrivalAnchor, time.Time{}, arrivalAnchor.AddDate(0, 0, 1)) {
		t.Error("zero check-in must hide arrival info")
	}
	if ArrivalVisibleAt(arrivalAnchor, arrivalAnchor, time.Time{}) {
		t.Error("zero check-out must hide arrival info")
	}
}

func TestSetArrivalInfo_TrimsAndValidates(t *testing.T) {
	p := mustProperty(t)

	// Trims whitespace + normalises unknown method to the empty marker.
	if err := p.SetArrivalInfo(ArrivalInfo{
		CheckInMethod: "weird",
		Instructions:  "  Code is 1234.  ",
		WifiSSID:      " AirhostNet  ",
		WifiPassword:  "  hunter2  ",
	}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if p.Arrival.CheckInMethod != CheckInMethodUnknown {
		t.Fatalf("method = %q, want unknown (normalised)", p.Arrival.CheckInMethod)
	}
	if p.Arrival.Instructions != "Code is 1234." {
		t.Fatalf("instructions = %q, want trimmed", p.Arrival.Instructions)
	}
	if p.Arrival.WifiSSID != "AirhostNet" || p.Arrival.WifiPassword != "hunter2" {
		t.Fatalf("wifi creds not trimmed: %q / %q", p.Arrival.WifiSSID, p.Arrival.WifiPassword)
	}
	if !p.Arrival.IsConfigured() {
		t.Fatal("IsConfigured should be true after a non-empty set")
	}

	// Rejects oversized instructions.
	huge := ArrivalInfo{Instructions: strings.Repeat("x", 2001)}
	if err := p.SetArrivalInfo(huge); err == nil {
		t.Fatal("instructions > 2000 chars must be rejected")
	}
	// Rejects oversized SSID.
	if err := p.SetArrivalInfo(ArrivalInfo{WifiSSID: strings.Repeat("y", 101)}); err == nil {
		t.Fatal("SSID > 100 chars must be rejected")
	}
	// Rejects oversized password.
	if err := p.SetArrivalInfo(ArrivalInfo{WifiPassword: strings.Repeat("z", 201)}); err == nil {
		t.Fatal("password > 200 chars must be rejected")
	}

	// Clears back to empty.
	if err := p.SetArrivalInfo(ArrivalInfo{}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if p.Arrival.IsConfigured() {
		t.Fatal("IsConfigured should be false after clearing")
	}
}

func TestSetArrivalInfo_AcceptsKnownMethods(t *testing.T) {
	p := mustProperty(t)
	methods := []CheckInMethod{
		CheckInMethodSelfLockbox, CheckInMethodSmartLock,
		CheckInMethodKeyExchange, CheckInMethodHostGreeting,
	}
	for _, m := range methods {
		if err := p.SetArrivalInfo(ArrivalInfo{CheckInMethod: m, Instructions: "ok"}); err != nil {
			t.Fatalf("method %q: %v", m, err)
		}
		if p.Arrival.CheckInMethod != m {
			t.Fatalf("stored %q, want %q", p.Arrival.CheckInMethod, m)
		}
	}
}
