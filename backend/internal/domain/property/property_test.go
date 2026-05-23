package property

import "testing"

func TestCancellationPolicy_RefundFraction(t *testing.T) {
	cases := []struct {
		policy CancellationPolicy
		days   int
		want   float64
	}{
		{PolicyFlexible, 1, 1.0},
		{PolicyFlexible, 0, 0.0},
		{PolicyModerate, 5, 1.0},
		{PolicyModerate, 4, 0.5},
		{PolicyModerate, 0, 0.0},
		{PolicyStrict, 7, 1.0},
		{PolicyStrict, 6, 0.5},
		{PolicyStrict, 2, 0.5},
		{PolicyStrict, 1, 0.0},
	}
	for _, c := range cases {
		if got := c.policy.RefundFraction(c.days); got != c.want {
			t.Errorf("%s @ %dd = %v, want %v", c.policy, c.days, got, c.want)
		}
	}
}

func TestNormalizeAmenities(t *testing.T) {
	in := []string{"WiFi", "  kitchen ", "Air Conditioning", "air_conditioning", "unknown", "wifi"}
	got := NormalizeAmenities(in)
	want := []string{"wifi", "kitchen", "air-conditioning"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestValidCancellationPolicy_DefaultsToFlexible(t *testing.T) {
	if ValidCancellationPolicy("nonsense") != PolicyFlexible {
		t.Error("unknown policy should default to flexible")
	}
	if ValidCancellationPolicy(PolicyStrict) != PolicyStrict {
		t.Error("known policy should be preserved")
	}
}
