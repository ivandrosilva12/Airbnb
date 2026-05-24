// Package payoutapp contains host-earnings use cases. The ledger is driven by
// the booking lifecycle through domain events (earnings on confirmation, a
// refund debit on cancellation) and exposes read-models for the host dashboard.
package payoutapp

import (
	"context"
	"errors"
	"time"

	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/payout"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/google/uuid"
)

// Service orchestrates host-earnings use cases.
type Service struct {
	payouts    payout.Repository
	bookings   booking.Repository
	properties property.Repository
}

// NewService wires the payout application service. The booking and property
// repositories are read-only dependencies used to resolve the host and the net
// payout amount when reacting to booking events.
func NewService(payouts payout.Repository, bookings booking.Repository, properties property.Repository) *Service {
	return &Service{payouts: payouts, bookings: bookings, properties: properties}
}

// Summary returns the host's balance per currency.
func (s *Service) Summary(ctx context.Context, hostID uuid.UUID) ([]payout.Balance, error) {
	return s.payouts.BalancesByHost(ctx, hostID)
}

// ListEntries returns the host's ledger entries, newest first.
func (s *Service) ListEntries(ctx context.Context, hostID uuid.UUID, page shared.Page) (shared.PageResult[*payout.Entry], error) {
	return s.payouts.ListByHost(ctx, hostID, page)
}

// ExportRow is a flattened ledger line for a CSV/statement export, enriched with
// the listing title and the signed amount (refunds are negative).
type ExportRow struct {
	CreatedAt     time.Time
	Kind          string
	PropertyID    uuid.UUID
	PropertyTitle string
	BookingID     uuid.UUID
	Currency      string
	SignedCents   int64
}

// ExportEntries returns the host's full ledger (all pages), newest first, with
// listing titles resolved, for a statement export.
func (s *Service) ExportEntries(ctx context.Context, hostID uuid.UUID) ([]ExportRow, error) {
	var entries []*payout.Entry
	for offset := 0; ; {
		res, err := s.payouts.ListByHost(ctx, hostID, shared.Page{Limit: 100, Offset: offset})
		if err != nil {
			return nil, err
		}
		entries = append(entries, res.Items...)
		offset += len(res.Items)
		if len(res.Items) == 0 || int64(offset) >= res.Total {
			break
		}
	}

	titles := map[uuid.UUID]string{}
	rows := make([]ExportRow, 0, len(entries))
	for _, e := range entries {
		title, ok := titles[e.PropertyID]
		if !ok {
			if p, err := s.properties.FindByID(ctx, e.PropertyID); err == nil {
				title = p.Title
			} else if !errors.Is(err, shared.ErrNotFound) {
				return nil, err
			}
			titles[e.PropertyID] = title
		}
		rows = append(rows, ExportRow{
			CreatedAt:     e.CreatedAt,
			Kind:          string(e.Kind),
			PropertyID:    e.PropertyID,
			PropertyTitle: title,
			BookingID:     e.BookingID,
			Currency:      e.Amount.Currency(),
			SignedCents:   e.SignedCents(),
		})
	}
	return rows, nil
}

// hostNet derives the amount owed to the host for a booking: the guest's total
// minus the platform service fee (i.e. the nightly subtotal plus cleaning fee).
func hostNet(b *booking.Booking) (shared.Money, error) {
	netCents := b.Pricing.Total.AmountCents() - b.Pricing.ServiceFee.AmountCents()
	return shared.NewMoney(netCents, b.Pricing.Total.Currency())
}
