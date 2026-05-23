-- 0006_payments.sql — booking payments.

CREATE TABLE IF NOT EXISTS payments (
    id             UUID PRIMARY KEY,
    booking_id     UUID        NOT NULL UNIQUE REFERENCES bookings (id) ON DELETE CASCADE,
    guest_id       UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    amount_cents   BIGINT      NOT NULL CHECK (amount_cents >= 0),
    currency       CHAR(3)     NOT NULL,
    status         TEXT        NOT NULL
                   CHECK (status IN ('pending', 'authorized', 'captured', 'refunded', 'failed')),
    gateway_ref    TEXT        NOT NULL DEFAULT '',
    failure_reason TEXT        NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_payments_guest ON payments (guest_id, created_at DESC);
