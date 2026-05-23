-- 0004_pricing.sql — cleaning fee on listings and full price breakdown on bookings.

ALTER TABLE properties
    ADD COLUMN IF NOT EXISTS cleaning_fee_cents BIGINT NOT NULL DEFAULT 0
        CHECK (cleaning_fee_cents >= 0);

ALTER TABLE bookings
    ADD COLUMN IF NOT EXISTS subtotal_cents     BIGINT NOT NULL DEFAULT 0 CHECK (subtotal_cents >= 0),
    ADD COLUMN IF NOT EXISTS cleaning_fee_cents BIGINT NOT NULL DEFAULT 0 CHECK (cleaning_fee_cents >= 0),
    ADD COLUMN IF NOT EXISTS service_fee_cents  BIGINT NOT NULL DEFAULT 0 CHECK (service_fee_cents >= 0);

-- Backfill the subtotal for any pre-existing rows so totals stay consistent.
UPDATE bookings SET subtotal_cents = total_cents WHERE subtotal_cents = 0 AND total_cents > 0;
