-- 0045_split_payments.sql — split-payment plans and their per-traveller shares.
--
-- A SplitPayment is created alongside a booking when the guest invites N
-- travellers to split the cost. The booking confirms only when every share
-- transitions to "paid". Restricted to instant-book listings (the split
-- itself is the commitment; no host approval after).

CREATE TABLE IF NOT EXISTS split_payments (
    id            UUID PRIMARY KEY,
    booking_id    UUID NOT NULL UNIQUE REFERENCES bookings(id) ON DELETE CASCADE,
    organizer_id  UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    currency      TEXT NOT NULL,
    total_cents   BIGINT NOT NULL CHECK (total_cents > 0),
    status        TEXT NOT NULL CHECK (status IN ('pending', 'completed', 'cancelled')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at  TIMESTAMPTZ,
    cancelled_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS split_payments_organizer_idx
    ON split_payments (organizer_id);

CREATE TABLE IF NOT EXISTS split_payment_shares (
    id                UUID PRIMARY KEY,
    split_payment_id  UUID NOT NULL REFERENCES split_payments(id) ON DELETE CASCADE,
    payer_email       TEXT NOT NULL,
    payer_user_id     UUID REFERENCES users(id) ON DELETE SET NULL,
    amount_cents      BIGINT NOT NULL CHECK (amount_cents > 0),
    status            TEXT NOT NULL CHECK (status IN ('pending', 'paid')),
    paid_at           TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- A payer email is unique per split (the domain rejects duplicates; the
-- database enforces it too as a race-safe backstop).
CREATE UNIQUE INDEX IF NOT EXISTS split_payment_shares_split_email_uniq
    ON split_payment_shares (split_payment_id, lower(payer_email));

-- "Splits I'm a payer in" lookup uses payer_email (the inviting key) since
-- payer_user_id is only set once the share is authorised.
CREATE INDEX IF NOT EXISTS split_payment_shares_email_idx
    ON split_payment_shares (lower(payer_email));
