// Package taxremittanceapp computes per-jurisdiction tax-remittance reports
// (S62) by aggregating booking.JurisdictionTaxLines over a period. Pure
// read-model — no writes, no persistence beyond what the underlying
// booking and property repos already do.
package taxremittanceapp

import (
	"context"
	"sort"

	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/taxremittance"
)

// Service computes remittance reports from booking + property repos.
type Service struct {
	bookings   booking.Repository
	properties property.Repository
}

// NewService wires the remittance read-model service.
func NewService(bookings booking.Repository, properties property.Repository) *Service {
	return &Service{bookings: bookings, properties: properties}
}

// Generate scans all confirmed/completed bookings whose check-out falls in
// the period and returns one Report per (country, city, currency)
// jurisdiction. A booking with no JurisdictionTaxLines is ignored (no
// matching rules — nothing to remit). Returns an empty slice when no
// bookings match; never returns nil.
//
// Property look-ups are cached for the duration of one call so a
// jurisdiction with N bookings only triggers one property fetch per listing,
// not per booking.
func (s *Service) Generate(ctx context.Context, period taxremittance.Period) ([]taxremittance.Report, error) {
	bs, err := s.bookings.ListSettledInPeriod(ctx, period.From(), period.To())
	if err != nil {
		return nil, err
	}
	// (country, city, currency) → accumulator
	type bucketKey struct{ country, city, currency string }
	type bucket struct {
		jur          taxremittance.Jurisdiction
		currency     string
		byLine       map[string]int64
		bookingCount int
		// distinct booking IDs that touched this bucket so we can count
		// "did this booking contribute to this jurisdiction's report",
		// which is not the same as "sum of all lines' bookingCount".
		seenBookings map[string]struct{}
		// per-line booking counts.
		lineBookings map[string]map[string]struct{}
	}
	buckets := map[bucketKey]*bucket{}
	propCache := map[string]*property.Property{} // propertyID → property

	for _, b := range bs {
		if len(b.Pricing.JurisdictionTaxLines) == 0 {
			continue
		}
		propID := b.PropertyID.String()
		prop, ok := propCache[propID]
		if !ok {
			p, err := s.properties.FindByID(ctx, b.PropertyID)
			if err != nil {
				return nil, err
			}
			prop = p
			propCache[propID] = prop
		}
		key := bucketKey{
			country:  prop.Address.Country,
			city:     prop.Address.City,
			currency: b.Pricing.Subtotal.Currency(),
		}
		bk, ok := buckets[key]
		if !ok {
			bk = &bucket{
				jur:          taxremittance.Jurisdiction{Country: key.country, City: key.city},
				currency:     key.currency,
				byLine:       map[string]int64{},
				seenBookings: map[string]struct{}{},
				lineBookings: map[string]map[string]struct{}{},
			}
			buckets[key] = bk
		}
		bID := b.ID.String()
		if _, seen := bk.seenBookings[bID]; !seen {
			bk.seenBookings[bID] = struct{}{}
			bk.bookingCount++
		}
		for _, line := range b.Pricing.JurisdictionTaxLines {
			bk.byLine[line.Name] += line.AmountCents
			lb, ok := bk.lineBookings[line.Name]
			if !ok {
				lb = map[string]struct{}{}
				bk.lineBookings[line.Name] = lb
			}
			lb[bID] = struct{}{}
		}
	}

	out := make([]taxremittance.Report, 0, len(buckets))
	for _, bk := range buckets {
		lines := make([]taxremittance.LineSummary, 0, len(bk.byLine))
		var total int64
		for name, cents := range bk.byLine {
			lines = append(lines, taxremittance.LineSummary{
				Name:         name,
				AmountCents:  cents,
				BookingCount: len(bk.lineBookings[name]),
			})
			total += cents
		}
		sort.Slice(lines, func(i, j int) bool { return lines[i].Name < lines[j].Name })
		out = append(out, taxremittance.Report{
			Period:       period,
			Jurisdiction: bk.jur,
			Currency:     bk.currency,
			Lines:        lines,
			TotalCents:   total,
			BookingCount: bk.bookingCount,
		})
	}
	// Stable jurisdiction ordering: country first, then city, then currency.
	// Without this the map iteration would shuffle output between runs and
	// regulator submissions would diff every time.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Jurisdiction.Country != out[j].Jurisdiction.Country {
			return out[i].Jurisdiction.Country < out[j].Jurisdiction.Country
		}
		if out[i].Jurisdiction.City != out[j].Jurisdiction.City {
			return out[i].Jurisdiction.City < out[j].Jurisdiction.City
		}
		return out[i].Currency < out[j].Currency
	})
	return out, nil
}
