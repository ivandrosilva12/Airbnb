-- 0038_price_rules.sql — date-range nightly-price overrides per listing.

CREATE TABLE IF NOT EXISTS price_rules (
    id          UUID PRIMARY KEY,
    property_id UUID        NOT NULL REFERENCES properties (id) ON DELETE CASCADE,
    start_date  DATE        NOT NULL,
    end_date    DATE        NOT NULL,
    price_cents BIGINT      NOT NULL CHECK (price_cents > 0),
    currency    TEXT        NOT NULL,
    label       TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL,
    CHECK (end_date > start_date)
);
CREATE INDEX IF NOT EXISTS idx_price_rules_property ON price_rules (property_id, start_date);
