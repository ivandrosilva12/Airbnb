-- 0042_deposit_holds.sql — security-deposit holds attached to a booking.
-- One hold per booking; the aggregate tracks the running CapturedCents and a
-- terminal status (released / captured / failed). Capture detail rows live in
-- payment_adjustments under a new 'deposit_capture' kind, so the audit trail
-- for a booking stays in one place.
--
-- The unique index on booking_id keeps the BookingConfirmed subscriber
-- idempotent at storage level — replayed events return ErrConflict and the
-- application code treats that as "already authorized, nothing to do".

CREATE TABLE IF NOT EXISTS deposit_holds (
    id              UUID PRIMARY KEY,
    booking_id      UUID        NOT NULL UNIQUE REFERENCES bookings (id) ON DELETE CASCADE,
    guest_id        UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    amount_cents    BIGINT      NOT NULL CHECK (amount_cents >= 0),
    currency        CHAR(3)     NOT NULL,
    captured_cents  BIGINT      NOT NULL DEFAULT 0 CHECK (captured_cents >= 0),
    status          TEXT        NOT NULL
                    CHECK (status IN ('pending','authorized','partially_captured','captured','released','failed')),
    gateway_ref     TEXT        NOT NULL DEFAULT '',
    failure_reason  TEXT        NOT NULL DEFAULT '',
    released_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,
    CHECK (captured_cents <= amount_cents)
);

CREATE INDEX IF NOT EXISTS idx_deposit_holds_guest ON deposit_holds (guest_id, created_at DESC);

-- The payment_adjustments table is shared between Payment and DepositHold —
-- relax the check constraint so 'deposit_capture' rows are allowed.
ALTER TABLE payment_adjustments DROP CONSTRAINT IF EXISTS payment_adjustments_kind_check;
ALTER TABLE payment_adjustments ADD CONSTRAINT payment_adjustments_kind_check
    CHECK (kind IN ('refund','damage_claim','deposit_capture'));
