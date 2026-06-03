-- Experiences (S71 stub → promoted to postgres in S75).
--
-- Per-guest activities (cooking classes, food tours, surf lessons) sold
-- separately from overnight stays. Aggregate is in
-- internal/domain/experience; this table is the storage edge for
-- experience.Repository.
--
-- Schema-level CHECKs enforce the same invariants the domain
-- constructor does — defence in depth so a buggy adapter or a manual
-- INSERT can't land a row the application would later refuse to load:
--   category IN the closed enum that the catalogue UI groups by;
--   status IN the closed publication state machine;
--   duration_minutes between 30 and 24h (multi-day retreats are a
--     different product);
--   max_guests between 1 and 50 (a workshop-sized cap);
--   price_amount_cents non-negative (free experiences are allowed,
--     negative prices are nonsense).
--
-- Photos are JSONB so the per-photo metadata shape (id, objectKey,
-- url, position) can evolve without a column migration — the
-- experience.AddPhoto domain method owns the schema.
CREATE TABLE IF NOT EXISTS experiences (
    id               UUID             PRIMARY KEY,
    host_id          UUID             NOT NULL,
    title            TEXT             NOT NULL,
    description      TEXT             NOT NULL DEFAULT '',
    category         TEXT             NOT NULL CHECK (category IN ('cooking','tour','sport','art','wellness','other')),
    status           TEXT             NOT NULL CHECK (status IN ('draft','published','suspended')),
    city             TEXT             NOT NULL,
    country          TEXT             NOT NULL,
    latitude         DOUBLE PRECISION NOT NULL,
    longitude        DOUBLE PRECISION NOT NULL,
    duration_minutes INTEGER          NOT NULL CHECK (duration_minutes BETWEEN 30 AND 1440),
    max_guests       INTEGER          NOT NULL CHECK (max_guests BETWEEN 1 AND 50),
    price_amount_cents BIGINT         NOT NULL CHECK (price_amount_cents >= 0),
    price_currency   CHAR(3)          NOT NULL,
    language         CHAR(2)          NOT NULL,
    photos           JSONB            NOT NULL DEFAULT '[]'::JSONB,
    created_at       TIMESTAMPTZ      NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ      NOT NULL DEFAULT now()
);

-- Read patterns:
--   host dashboard "my listings, newest first"    → idx_experiences_host
--   public catalogue "published in $country, …"   → idx_experiences_search
--                                                   (partial: published only)
CREATE INDEX IF NOT EXISTS idx_experiences_host
    ON experiences (host_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_experiences_search
    ON experiences (status, category, country, created_at DESC)
    WHERE status = 'published';
