-- Promotional discount codes a guest can apply to a booking.
CREATE TABLE IF NOT EXISTS coupons (
    id              UUID PRIMARY KEY,
    code            TEXT NOT NULL,
    kind            TEXT NOT NULL,
    percent         DOUBLE PRECISION NOT NULL DEFAULT 0,
    amount_cents    BIGINT NOT NULL DEFAULT 0,
    currency        TEXT NOT NULL DEFAULT '',
    min_nights      INTEGER NOT NULL DEFAULT 0,
    max_redemptions INTEGER NOT NULL DEFAULT 0,
    redemptions     INTEGER NOT NULL DEFAULT 0,
    expires_at      TIMESTAMPTZ,
    active          BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Codes are matched case-insensitively, so uniqueness is on the upper-cased code.
CREATE UNIQUE INDEX IF NOT EXISTS idx_coupons_code ON coupons (upper(code));
