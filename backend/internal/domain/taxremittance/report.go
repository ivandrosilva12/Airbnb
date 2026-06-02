// Package taxremittance is a read-only bounded context that aggregates the
// per-jurisdiction tax captured on confirmed/completed bookings into a
// periodic remittance report. It owns no aggregate of its own — the source
// of truth is the booking aggregate's JurisdictionTaxLines (S49) — but it
// owns the shape regulators and accountants need: one report per
// (country, city, currency) per period, broken down by rule name.
//
// The context is intentionally minimal: no remittance state machine, no
// "filed/unfiled" markers, no auditor sign-off. Those belong in a later
// slice once we know what shape the operator (and the tax authority)
// actually want.
package taxremittance

import (
	"fmt"
	"time"

	"github.com/airhost/backend/internal/domain/shared"
)

// Period is a calendar month identified by (Year, Month). Tax authorities
// in PT and most of the EU expect monthly filing for tourism tax; VAT is
// quarterly or annual, but those higher cardinalities can be derived from
// monthly buckets without storing them separately.
//
// A Period is half-open: [From, To) — exclusive upper bound so a stay
// checking out on the first day of the next month is counted in the next
// month's report, matching most regulators' "stay realised at check-out"
// rule.
type Period struct {
	Year  int
	Month time.Month
}

// NewPeriod validates a (year, month) pair. Year must be between 2000 and
// 2100 (a generous sanity window — outside that and it's almost certainly a
// caller error); month must be 1..12.
func NewPeriod(year, month int) (Period, error) {
	if year < 2000 || year > 2100 {
		return Period{}, shared.NewValidationError(fmt.Sprintf("period: year %d out of range [2000,2100]", year))
	}
	if month < 1 || month > 12 {
		return Period{}, shared.NewValidationError(fmt.Sprintf("period: month %d out of range [1,12]", month))
	}
	return Period{Year: year, Month: time.Month(month)}, nil
}

// From is the inclusive start of the period in UTC.
func (p Period) From() time.Time {
	return time.Date(p.Year, p.Month, 1, 0, 0, 0, 0, time.UTC)
}

// To is the exclusive end of the period in UTC — i.e. the first instant of
// the following month.
func (p Period) To() time.Time {
	return p.From().AddDate(0, 1, 0)
}

// String renders the period as YYYY-MM, matching how the wire shape encodes
// it. Useful for log lines.
func (p Period) String() string {
	return fmt.Sprintf("%04d-%02d", p.Year, int(p.Month))
}

// Jurisdiction labels a (country, city) pair. Both are required — a city-
// less rule would over-aggregate (every PT booking lumped together) and a
// country-less one would mix VAT regimes that legally cannot be netted.
type Jurisdiction struct {
	Country string
	City    string
}

// LineSummary is one row in the report: the per-rule total a regulator
// expects to receive, plus the number of bookings that contributed (so an
// auditor can sanity-check the breakdown against the raw bookings list).
type LineSummary struct {
	Name         string // tax-rule name as it appears on the booking
	AmountCents  int64
	BookingCount int
}

// Report is one (Period, Jurisdiction) bucket in a remittance run. Lines
// are sorted by Name so successive runs over the same period produce
// identical output (regulator filings tolerate no diff drift).
type Report struct {
	Period       Period
	Jurisdiction Jurisdiction
	Currency     string
	Lines        []LineSummary
	TotalCents   int64
	BookingCount int // distinct bookings contributing to this report
}
