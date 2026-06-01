-- 0037_weekend_price.sql — per-listing weekend nightly price override.

ALTER TABLE properties ADD COLUMN IF NOT EXISTS weekend_price_cents BIGINT NOT NULL DEFAULT 0;
