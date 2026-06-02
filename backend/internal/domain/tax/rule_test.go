package tax

import (
	"errors"
	"testing"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
)

// TestNewRule_PercentHappyPath confirms a well-formed VAT-style rule
// is constructed without complaint and rejects the wrong-knob errors.
func TestNewRule_PercentHappyPath(t *testing.T) {
	r, err := NewRule("Portugal VAT", KindPercent, "PT", "", "EUR", 2300, 0, 0, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("NewRule: %v", err)
	}
	if r.RatePctBips != 2300 || r.Country != "PT" {
		t.Errorf("unexpected fields: %+v", r)
	}
	// Wrong knob: flatCents on a percent rule.
	if _, err := NewRule("bad", KindPercent, "PT", "", "EUR", 2300, 100, 0, time.Time{}, time.Time{}); !errors.Is(err, shared.ErrValidation) {
		t.Errorf("expected validation error for flatCents on percent rule, got %v", err)
	}
	if _, err := NewRule("bad", KindPercent, "PT", "", "EUR", 0, 0, 0, time.Time{}, time.Time{}); !errors.Is(err, shared.ErrValidation) {
		t.Errorf("expected validation error for missing bips, got %v", err)
	}
}

// TestNewRule_PerNightPerGuest_HappyPathAndGuards covers the Lisbon
// tourist-tax shape (per night per guest, capped) and rejects bad knobs.
func TestNewRule_PerNightPerGuest_HappyPathAndGuards(t *testing.T) {
	r, err := NewRule("Lisbon tourist tax", KindPerNightPerGuest, "PT", "Lisbon", "EUR", 0, 200, 7, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("NewRule: %v", err)
	}
	if r.FlatAmountCents != 200 || r.MaxNights != 7 || r.City != "Lisbon" {
		t.Errorf("unexpected fields: %+v", r)
	}
	if _, err := NewRule("bad", KindPerNightPerGuest, "PT", "", "EUR", 100, 200, 7, time.Time{}, time.Time{}); !errors.Is(err, shared.ErrValidation) {
		t.Errorf("expected validation error for rateBips on per-night rule, got %v", err)
	}
	if _, err := NewRule("bad", KindPerNightPerGuest, "PT", "", "EUR", 0, 0, 7, time.Time{}, time.Time{}); !errors.Is(err, shared.ErrValidation) {
		t.Errorf("expected validation error for missing flat amount, got %v", err)
	}
}

// TestNewRule_RejectsBadCurrency_AndOpenEndedWindow proves the
// currency-format and effective-window guards trigger.
func TestNewRule_RejectsBadCurrency_AndOpenEndedWindow(t *testing.T) {
	if _, err := NewRule("bad", KindPercent, "PT", "", "EU", 2300, 0, 0, time.Time{}, time.Time{}); !errors.Is(err, shared.ErrValidation) {
		t.Errorf("expected validation error for 2-letter currency, got %v", err)
	}
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	until := from // until == from is rejected (must be after)
	if _, err := NewRule("bad", KindPercent, "PT", "", "EUR", 2300, 0, 0, from, until); !errors.Is(err, shared.ErrValidation) {
		t.Errorf("expected validation error for non-positive effective window, got %v", err)
	}
}

// TestRule_Matches_CountryAndCity_AreCaseInsensitive guards the
// case-folding behaviour of the matcher (a host might enter "PT" or
// "pt"; a UI might POST "Lisbon" or "lisbon" — every variant
// resolves to the same rule).
func TestRule_Matches_CountryAndCity_AreCaseInsensitive(t *testing.T) {
	r, _ := NewRule("X", KindPerStay, "PT", "Lisbon", "EUR", 0, 100, 0, time.Time{}, time.Time{})
	at := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if !r.Matches("pt", "lisbon", at) {
		t.Errorf("case-insensitive match failed: country and city should match")
	}
	if r.Matches("ES", "Lisbon", at) {
		t.Errorf("country mismatch should not match")
	}
	if r.Matches("PT", "Porto", at) {
		t.Errorf("city mismatch should not match")
	}
}

// TestRule_Matches_EmptyCity_MeansNationalScope confirms a rule with
// City == "" matches every city in the country (used for national
// VAT, for instance).
func TestRule_Matches_EmptyCity_MeansNationalScope(t *testing.T) {
	r, _ := NewRule("VAT", KindPercent, "PT", "", "EUR", 2300, 0, 0, time.Time{}, time.Time{})
	at := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if !r.Matches("PT", "Porto", at) {
		t.Errorf("national rule should match any city")
	}
	if !r.Matches("PT", "", at) {
		t.Errorf("national rule should match an unspecified city")
	}
}

// TestRule_Matches_EffectiveWindow_IsHalfOpen exercises the [from,
// until) semantics — a stay AT the until boundary does NOT match.
func TestRule_Matches_EffectiveWindow_IsHalfOpen(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	r, _ := NewRule("X", KindPerStay, "PT", "", "EUR", 0, 100, 0, from, until)
	if r.Matches("PT", "", from.Add(-time.Hour)) {
		t.Errorf("before from should not match")
	}
	if !r.Matches("PT", "", from) {
		t.Errorf("at from should match (inclusive)")
	}
	if r.Matches("PT", "", until) {
		t.Errorf("at until should not match (exclusive)")
	}
}

// TestCalculate_AllThreeKinds_SumsCorrectly is the canonical scenario:
// a Lisbon stay subject to national VAT (percent), tourist tax (per
// night per guest, capped), and a fixed convention fee. It exercises
// all three Kinds in one quote and confirms the total and the
// deterministic (name-sorted) line order.
func TestCalculate_AllThreeKinds_SumsCorrectly(t *testing.T) {
	vat, _ := NewRule("Z VAT", KindPercent, "PT", "", "EUR", 2300, 0, 0, time.Time{}, time.Time{})
	tourist, _ := NewRule("M Tourist tax", KindPerNightPerGuest, "PT", "Lisbon", "EUR", 0, 200, 7, time.Time{}, time.Time{})
	convention, _ := NewRule("A Convention fee", KindPerStay, "PT", "Lisbon", "EUR", 0, 500, 0, time.Time{}, time.Time{})

	stay := StayInput{
		Country: "PT", City: "Lisbon",
		CheckIn: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		Nights: 3, Guests: 2,
		SubtotalCents: 30000, // 300 EUR
		Currency:      "EUR",
	}
	q, err := Calculate(stay, []*Rule{vat, tourist, convention})
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	// Expected:
	//   VAT 23% of 30000 = 6900
	//   Tourist 200 * 3 nights * 2 guests = 1200
	//   Convention = 500
	//   Total = 8600
	if q.TotalCents != 8600 {
		t.Fatalf("total = %d, want 8600 (got lines: %+v)", q.TotalCents, q.Lines)
	}
	if len(q.Lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(q.Lines))
	}
	// Names are A < M < Z — confirm sorted output.
	if q.Lines[0].Name != "A Convention fee" || q.Lines[1].Name != "M Tourist tax" || q.Lines[2].Name != "Z VAT" {
		t.Errorf("lines not name-sorted: %v", []string{q.Lines[0].Name, q.Lines[1].Name, q.Lines[2].Name})
	}
}

// TestCalculate_PerNightPerGuest_NightsCap clamps long stays at
// MaxNights, matching Lisbon's "first 7 nights only" rule.
func TestCalculate_PerNightPerGuest_NightsCap(t *testing.T) {
	tourist, _ := NewRule("T", KindPerNightPerGuest, "PT", "Lisbon", "EUR", 0, 200, 7, time.Time{}, time.Time{})
	stay := StayInput{
		Country: "PT", City: "Lisbon",
		CheckIn: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		Nights: 14, Guests: 2, // 14 nights → capped at 7
		Currency: "EUR",
	}
	q, _ := Calculate(stay, []*Rule{tourist})
	// 200 * 7 (capped) * 2 = 2800
	if q.TotalCents != 2800 {
		t.Errorf("total = %d, want 2800 (cap should clamp)", q.TotalCents)
	}
}

// TestCalculate_SkipsCurrencyMismatch keeps a EUR rule out of an AOA
// booking's quote — the comment in Calculate is the contract; this
// test makes the contract executable.
func TestCalculate_SkipsCurrencyMismatch(t *testing.T) {
	eur, _ := NewRule("EUR VAT", KindPercent, "PT", "", "EUR", 2300, 0, 0, time.Time{}, time.Time{})
	aoa, _ := NewRule("AOA tax", KindPercent, "PT", "", "AOA", 1400, 0, 0, time.Time{}, time.Time{})
	stay := StayInput{
		Country: "PT",
		CheckIn: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		Nights: 1, Guests: 1, SubtotalCents: 10000, Currency: "AOA",
	}
	q, _ := Calculate(stay, []*Rule{eur, aoa})
	if len(q.Lines) != 1 || q.Lines[0].Name != "AOA tax" {
		t.Fatalf("expected only the AOA rule, got %+v", q.Lines)
	}
}

// TestCalculate_RejectsNegativeStayInputs proves the negative-guard
// fires — defensive but cheap, and a regression here would let a
// caller request "1 night with -2 guests" and silently get a negative
// tax that decreased the total.
func TestCalculate_RejectsNegativeStayInputs(t *testing.T) {
	r, _ := NewRule("X", KindPercent, "PT", "", "EUR", 2300, 0, 0, time.Time{}, time.Time{})
	stay := StayInput{Country: "PT", Nights: -1, Currency: "EUR"}
	if _, err := Calculate(stay, []*Rule{r}); !errors.Is(err, shared.ErrValidation) {
		t.Errorf("expected validation error for negative nights, got %v", err)
	}
}
