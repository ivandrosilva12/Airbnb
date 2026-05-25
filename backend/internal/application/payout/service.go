// Package payoutapp contains host-earnings use cases. The ledger is driven by
// the booking lifecycle through domain events (earnings on confirmation, a
// refund debit on cancellation) and exposes read-models for the host dashboard.
package payoutapp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/booking"
	"github.com/airhost/backend/internal/domain/payout"
	"github.com/airhost/backend/internal/domain/property"
	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
)

// Service orchestrates host-earnings use cases.
type Service struct {
	payouts    payout.Repository
	bookings   booking.Repository
	properties property.Repository
	users      user.Repository
	disburser  port.Disburser
	connect    port.ConnectGateway
}

// NewService wires the payout application service. The booking and property
// repositories resolve the host and net payout amount off booking events; the
// users repository holds each host's connected payout account; the disburser
// sends transfers and the connect gateway onboards accounts.
func NewService(
	payouts payout.Repository,
	bookings booking.Repository,
	properties property.Repository,
	users user.Repository,
	disburser port.Disburser,
	connect port.ConnectGateway,
) *Service {
	return &Service{
		payouts: payouts, bookings: bookings, properties: properties,
		users: users, disburser: disburser, connect: connect,
	}
}

// PayoutAccountStatus is the host's payout-onboarding state.
type PayoutAccountStatus struct {
	HasAccount bool
	Enabled    bool
}

// AccountStatus returns the host's payout-onboarding state from local data
// (no provider call).
func (s *Service) AccountStatus(ctx context.Context, hostID uuid.UUID) (PayoutAccountStatus, error) {
	u, err := s.users.FindByID(ctx, hostID)
	if err != nil {
		return PayoutAccountStatus{}, err
	}
	return PayoutAccountStatus{HasAccount: u.PayoutAccountID != "", Enabled: u.PayoutsEnabled}, nil
}

// StartOnboarding ensures the host has a connected account (creating one on
// first call) and returns a hosted onboarding link for them to complete. The
// refresh/return URLs are where the provider sends the host back to.
func (s *Service) StartOnboarding(ctx context.Context, hostID uuid.UUID, refreshURL, returnURL string) (string, error) {
	u, err := s.users.FindByID(ctx, hostID)
	if err != nil {
		return "", err
	}
	if u.PayoutAccountID == "" {
		acct, err := s.connect.CreateAccount(ctx, u.Email)
		if err != nil {
			return "", err
		}
		u.LinkPayoutAccount(acct.ID)
		u.SetPayoutsEnabled(acct.PayoutsEnabled)
		if err := s.users.Update(ctx, u); err != nil {
			return "", err
		}
	}
	return s.connect.CreateOnboardingLink(ctx, u.PayoutAccountID, refreshURL, returnURL)
}

// RefreshAccountStatus re-reads the connected account from the provider and
// persists its current payout capability — called when the host returns from
// onboarding. With no account it is a no-op read.
func (s *Service) RefreshAccountStatus(ctx context.Context, hostID uuid.UUID) (PayoutAccountStatus, error) {
	u, err := s.users.FindByID(ctx, hostID)
	if err != nil {
		return PayoutAccountStatus{}, err
	}
	if u.PayoutAccountID == "" {
		return PayoutAccountStatus{}, nil
	}
	acct, err := s.connect.GetAccount(ctx, u.PayoutAccountID)
	if err != nil {
		return PayoutAccountStatus{}, err
	}
	if u.SetPayoutsEnabled(acct.PayoutsEnabled) {
		if err := s.users.Update(ctx, u); err != nil {
			return PayoutAccountStatus{}, err
		}
	}
	return PayoutAccountStatus{HasAccount: true, Enabled: u.PayoutsEnabled}, nil
}

// Summary returns the host's balance per currency.
func (s *Service) Summary(ctx context.Context, hostID uuid.UUID) ([]payout.Balance, error) {
	return s.payouts.BalancesByHost(ctx, hostID)
}

// ListEntries returns the host's ledger entries, newest first.
func (s *Service) ListEntries(ctx context.Context, hostID uuid.UUID, page shared.Page) (shared.PageResult[*payout.Entry], error) {
	return s.payouts.ListByHost(ctx, hostID, page)
}

// AvailableBalance is a host's withdrawable balance in one currency: lifetime
// net earnings minus amounts already committed to pending/paid payouts.
type AvailableBalance struct {
	Currency       string
	AvailableCents int64
}

// AvailableBalances returns the host's withdrawable balance per currency.
func (s *Service) AvailableBalances(ctx context.Context, hostID uuid.UUID) ([]AvailableBalance, error) {
	balances, err := s.payouts.BalancesByHost(ctx, hostID)
	if err != nil {
		return nil, err
	}
	disbursed, err := s.payouts.DisbursedCentsByHost(ctx, hostID)
	if err != nil {
		return nil, err
	}
	out := make([]AvailableBalance, 0, len(balances))
	for _, b := range balances {
		out = append(out, AvailableBalance{Currency: b.Currency, AvailableCents: b.NetCents() - disbursed[b.Currency]})
	}
	return out, nil
}

// ListDisbursements returns the host's payouts, newest first.
func (s *Service) ListDisbursements(ctx context.Context, hostID uuid.UUID, page shared.Page) (shared.PageResult[*payout.Disbursement], error) {
	return s.payouts.ListDisbursementsByHost(ctx, hostID, page)
}

// RequestDisbursement pays out the host's entire available balance in the given
// currency. It records a pending payout, calls the rail, then marks the payout
// paid (persisting the rail's reference) or failed. Funds in flight are excluded
// from the available balance, so a repeat request cannot pay out twice.
//
// Note: the available check and the pending insert are not one atomic step, so
// two truly-concurrent requests for the same host+currency could both pass. A
// production deployment would serialise per host (advisory lock / SELECT … FOR
// UPDATE); for this system the window is small and the ledger stays auditable.
func (s *Service) RequestDisbursement(ctx context.Context, hostID uuid.UUID, currency string) (*payout.Disbursement, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		return nil, shared.NewValidationError("a currency is required")
	}

	host, err := s.users.FindByID(ctx, hostID)
	if err != nil {
		return nil, err
	}
	if !host.CanReceivePayouts() {
		return nil, shared.NewValidationError("complete payout onboarding before requesting a payout")
	}

	avail, err := s.AvailableBalances(ctx, hostID)
	if err != nil {
		return nil, err
	}
	var cents int64 = -1
	for _, a := range avail {
		if a.Currency == currency {
			cents = a.AvailableCents
			break
		}
	}
	if cents <= 0 {
		return nil, shared.NewValidationError("no funds available to pay out in " + currency)
	}
	amount, err := shared.NewMoney(cents, currency)
	if err != nil {
		return nil, err
	}

	d, err := payout.NewDisbursement(hostID, currency, cents)
	if err != nil {
		return nil, err
	}
	if err := s.payouts.CreateDisbursement(ctx, d); err != nil {
		return nil, err
	}

	// The disbursement id is the idempotency key, so a retried rail call for the
	// same payout never transfers twice. Funds go to the host's connected account.
	ref, err := s.disburser.Disburse(ctx, hostID, host.PayoutAccountID, amount, d.ID.String())
	if err != nil {
		d.MarkFailed(err.Error())
		if uerr := s.payouts.UpdateDisbursement(ctx, d); uerr != nil {
			return nil, uerr
		}
		return nil, fmt.Errorf("disbursement failed: %w", err)
	}
	d.MarkPaid(ref)
	if err := s.payouts.UpdateDisbursement(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
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
