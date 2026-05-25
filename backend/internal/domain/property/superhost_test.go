package property_test

import (
	"testing"

	"github.com/airhost/backend/internal/domain/property"
)

func TestQualifiesAsSuperhost(t *testing.T) {
	cases := []struct {
		name   string
		avg    float64
		count  int
		expect bool
	}{
		{"no reviews", 0, 0, false},
		{"high rating too few reviews", 5.0, 9, false},
		{"enough reviews rating just below", 4.79, 12, false},
		{"exact thresholds qualify", 4.8, 10, true},
		{"comfortably above", 4.95, 40, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := property.QualifiesAsSuperhost(tc.avg, tc.count); got != tc.expect {
				t.Errorf("QualifiesAsSuperhost(%v, %d) = %v, want %v", tc.avg, tc.count, got, tc.expect)
			}
		})
	}
}
