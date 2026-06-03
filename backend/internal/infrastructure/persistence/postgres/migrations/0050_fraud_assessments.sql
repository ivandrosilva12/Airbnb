-- Fraud detection assessments (S68 → promoted to postgres in S72).
--
-- One row per booking that has been scored. booking_id is UNIQUE
-- because the application's Save is upsert-on-bookingId (a re-Assess
-- replaces, never appends), so the constraint enforces the invariant
-- at the storage edge.
--
-- Signals is JSONB so the heuristic set can evolve without a migration
-- per added rule. The shape is an array of {name, impact, note} objects;
-- the Go side validates names at the domain edge.
CREATE TABLE IF NOT EXISTS fraud_assessments (
    id         UUID        PRIMARY KEY,
    booking_id UUID        NOT NULL UNIQUE,
    guest_id   UUID        NOT NULL,
    score      INTEGER     NOT NULL CHECK (score >= 0 AND score <= 100),
    level      TEXT        NOT NULL CHECK (level IN ('low','medium','high')),
    signals    JSONB       NOT NULL DEFAULT '[]'::JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Read patterns:
--   "show me the latest high-risk bookings"     → idx on level, created_at DESC
--   "what's the assessment for booking X?"      → UNIQUE on booking_id already serves
--   "list everything newest-first"              → idx on created_at DESC
CREATE INDEX IF NOT EXISTS idx_fraud_assessments_level_created
    ON fraud_assessments (level, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_fraud_assessments_created_at
    ON fraud_assessments (created_at DESC);
