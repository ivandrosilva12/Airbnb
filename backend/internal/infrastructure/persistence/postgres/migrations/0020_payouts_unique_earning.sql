-- Defence-in-depth for the at-least-once outbox: at most one 'earning' ledger
-- row per booking, so a duplicate BookingConfirmed delivery that races the
-- application-level guard cannot double-credit the host. (A booking can still
-- have multiple 'refund' rows, so the index is partial on kind='earning'.)
CREATE UNIQUE INDEX IF NOT EXISTS uniq_payouts_earning_per_booking
    ON payouts (booking_id)
    WHERE kind = 'earning';
