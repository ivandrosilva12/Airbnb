-- 0014_advanced_pricing.sql — length-of-stay discounts, occupancy tax.
--
-- Listings gain optional weekly/monthly discount fractions and a tax rate.
-- Bookings record the discount and tax amounts so the stored breakdown stays
-- self-consistent. All defaults are zero (no discount/tax), so existing rows
-- are unaffected.

ALTER TABLE properties
    ADD COLUMN IF NOT EXISTS weekly_discount_pct  DOUBLE PRECISION NOT NULL DEFAULT 0
        CHECK (weekly_discount_pct >= 0 AND weekly_discount_pct <= 1),
    ADD COLUMN IF NOT EXISTS monthly_discount_pct DOUBLE PRECISION NOT NULL DEFAULT 0
        CHECK (monthly_discount_pct >= 0 AND monthly_discount_pct <= 1),
    ADD COLUMN IF NOT EXISTS tax_rate_pct         DOUBLE PRECISION NOT NULL DEFAULT 0
        CHECK (tax_rate_pct >= 0 AND tax_rate_pct <= 1);

ALTER TABLE bookings
    ADD COLUMN IF NOT EXISTS discount_cents BIGINT NOT NULL DEFAULT 0 CHECK (discount_cents >= 0),
    ADD COLUMN IF NOT EXISTS tax_cents      BIGINT NOT NULL DEFAULT 0 CHECK (tax_cents >= 0);
