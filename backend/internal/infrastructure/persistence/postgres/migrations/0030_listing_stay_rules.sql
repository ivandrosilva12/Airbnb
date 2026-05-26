-- Per-listing stay rules and per-guest pricing.
ALTER TABLE properties
    ADD COLUMN IF NOT EXISTS min_nights             INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS max_nights             INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS guests_included        INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS extra_guest_fee_cents  BIGINT  NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS security_deposit_cents BIGINT  NOT NULL DEFAULT 0;

-- Existing listings include all their guests in the base price by default.
UPDATE properties SET guests_included = max_guests WHERE guests_included < 1 OR guests_included > max_guests;
