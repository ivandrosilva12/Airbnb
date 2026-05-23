package property

import "strings"

// CanonicalAmenities is the shared, canonical set of amenity codes. The web
// create form and the search filter both use this list (via GET /amenities) so
// stored data and filters always agree.
var CanonicalAmenities = []string{
	"wifi",
	"kitchen",
	"parking",
	"pool",
	"air-conditioning",
	"heating",
	"washer",
	"dryer",
	"tv",
	"workspace",
	"hot-tub",
	"gym",
	"pets-allowed",
	"elevator",
	"breakfast",
	"smoke-alarm",
}

var amenitySet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(CanonicalAmenities))
	for _, a := range CanonicalAmenities {
		m[a] = struct{}{}
	}
	return m
}()

// CanonicalizeAmenity normalises a free-form amenity to a canonical code, or ""
// if it is not recognised.
func CanonicalizeAmenity(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	if _, ok := amenitySet[s]; ok {
		return s
	}
	return ""
}

// NormalizeAmenities maps inputs to canonical codes, dropping unknown values and
// duplicates while preserving order.
func NormalizeAmenities(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, a := range in {
		c := CanonicalizeAmenity(a)
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}
