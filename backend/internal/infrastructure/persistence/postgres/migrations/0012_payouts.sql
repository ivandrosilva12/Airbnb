-- 0012_payouts.sql — host earnings ledger.
--
-- Each row is a ledger entry tied to a booking: an 'earning' credits the host
-- with the net payout (booking total minus the platform service fee) when the
-- booking is confirmed; a 'refund' debits the host when a cancellation returns
-- money to the guest. The host balance is the signed sum of these entries.

CREATE TABLE IF NOT EXISTS payouts (
    id           UUID PRIMARY KEY,
    host_id      UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    booking_id   UUID        NOT NULL REFERENCES bookings (id) ON DELETE CASCADE,
    property_id  UUID        NOT NULL REFERENCES properties (id) ON DELETE CASCADE,
    kind         TEXT        NOT NULL CHECK (kind IN ('earning', 'refund')),
    amount_cents BIGINT      NOT NULL CHECK (amount_cents >= 0),
    currency     CHAR(3)     NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_payouts_host ON payouts (host_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_payouts_booking ON payouts (booking_id);
