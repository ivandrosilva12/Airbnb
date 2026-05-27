-- 0035_offers.sql — host pre-approvals and special offers sent to a guest.

CREATE TABLE IF NOT EXISTS offers (
    id          UUID PRIMARY KEY,
    property_id UUID        NOT NULL REFERENCES properties (id) ON DELETE CASCADE,
    host_id     UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    guest_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    check_in    TIMESTAMPTZ NOT NULL,
    check_out   TIMESTAMPTZ NOT NULL,
    guests      INT         NOT NULL,
    price_cents BIGINT      NOT NULL DEFAULT 0, -- nightly override; 0 = listing price
    currency    TEXT        NOT NULL,
    message     TEXT        NOT NULL DEFAULT '',
    kind        TEXT        NOT NULL CHECK (kind IN ('pre_approval', 'special_offer')),
    status      TEXT        NOT NULL CHECK (status IN ('pending', 'accepted', 'declined', 'withdrawn', 'expired')),
    created_at  TIMESTAMPTZ NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_offers_guest ON offers (guest_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_offers_host ON offers (host_id, created_at DESC);
