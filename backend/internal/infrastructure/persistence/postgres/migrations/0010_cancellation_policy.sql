-- 0010_cancellation_policy.sql — per-listing cancellation policy + payment refunds.

ALTER TABLE properties ADD COLUMN IF NOT EXISTS cancellation_policy TEXT NOT NULL DEFAULT 'flexible'
    CHECK (cancellation_policy IN ('flexible', 'moderate', 'strict'));

ALTER TABLE payments ADD COLUMN IF NOT EXISTS refunded_cents BIGINT NOT NULL DEFAULT 0
    CHECK (refunded_cents >= 0);
