package property_test

import (
	"testing"

	"github.com/airhost/backend/internal/domain/property"
)

// TestCanonicalizeAmenityAccessibility (S161) — the six accessibility codes
// added for EAA / ADA compliance must canonicalize to themselves whether the
// caller submits kebab-case or snake_case. Without this guarantee the filter
// silently drops the value and a guest with a mobility impairment cannot
// surface accessible listings.
func TestCanonicalizeAmenityAccessibility(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"wheelchair-accessible", "wheelchair-accessible"},
		{"wheelchair_accessible", "wheelchair-accessible"},
		{"step-free-entry", "step-free-entry"},
		{"step_free_entry", "step-free-entry"},
		{"wide-doorways", "wide-doorways"},
		{"wide_doorways", "wide-doorways"},
		{"roll-in-shower", "roll-in-shower"},
		{"roll_in_shower", "roll-in-shower"},
		{"accessible-parking", "accessible-parking"},
		{"accessible_parking", "accessible-parking"},
		{"accessible-bathroom", "accessible-bathroom"},
		{"accessible_bathroom", "accessible-bathroom"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := property.CanonicalizeAmenity(tc.in); got != tc.want {
				t.Errorf("CanonicalizeAmenity(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestCanonicalAmenitiesIncludesAccessibility (S161) — the canonical slice
// itself must contain each accessibility code so /amenities exposes them to
// the web + mobile filter UIs.
func TestCanonicalAmenitiesIncludesAccessibility(t *testing.T) {
	want := []string{
		"wheelchair-accessible",
		"step-free-entry",
		"wide-doorways",
		"roll-in-shower",
		"accessible-parking",
		"accessible-bathroom",
	}
	have := make(map[string]struct{}, len(property.CanonicalAmenities))
	for _, a := range property.CanonicalAmenities {
		have[a] = struct{}{}
	}
	for _, code := range want {
		if _, ok := have[code]; !ok {
			t.Errorf("CanonicalAmenities is missing accessibility code %q", code)
		}
	}
}

// TestNormalizeAmenitiesAccessibility (S161) — NormalizeAmenities is the
// path every write endpoint actually goes through, so check it accepts the
// new codes and dedupes mixed-case input the way it does for the rest.
func TestNormalizeAmenitiesAccessibility(t *testing.T) {
	in := []string{
		"wheelchair_accessible",
		"WHEELCHAIR-ACCESSIBLE",
		"step free entry",
		"roll-in-shower",
		"unknown-code",
	}
	got := property.NormalizeAmenities(in)
	want := []string{
		"wheelchair-accessible",
		"step-free-entry",
		"roll-in-shower",
	}
	if len(got) != len(want) {
		t.Fatalf("NormalizeAmenities(%v) = %v, want %v", in, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("NormalizeAmenities[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
