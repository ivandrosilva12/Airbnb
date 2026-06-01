-- 0041_payment_adjustments.sql — audit ledger of partial refunds and damage
-- claims layered on top of a Payment. The aggregate keeps running totals
-- (payments.refunded_cents, payments.damage_claim_cents) — the rows here are
-- the per-event detail (kind, amount, reason, source aggregate).
--
-- (ref_kind, ref_id) points at the aggregate that triggered the adjustment
-- (e.g. ref_kind='dispute', ref_id=<disputes.id>). The unique index on
-- (payment_id, kind, ref_kind, ref_id) keeps re-applying the same dispute
-- outcome idempotent at storage level.

ALTER TABLE payments
    ADD COLUMN IF NOT EXISTS damage_claim_cents BIGINT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS payment_adjustments (
    id           UUID PRIMARY KEY,
    payment_id   UUID        NOT NULL REFERENCES payments (id) ON DELETE CASCADE,
    kind         TEXT        NOT NULL CHECK (kind IN ('refund', 'damage_claim')),
    amount_cents BIGINT      NOT NULL CHECK (amount_cents > 0),
    reason       TEXT        NOT NULL DEFAULT '',
    ref_kind     TEXT        NOT NULL DEFAULT '',
    ref_id       UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_payment_adjustments_payment
    ON payment_adjustments (payment_id, created_at);

-- Idempotency: the same dispute resolution must not produce two adjustments
-- of the same kind against the same payment.
CREATE UNIQUE INDEX IF NOT EXISTS uq_payment_adjustments_source
    ON payment_adjustments (payment_id, kind, ref_kind, ref_id)
    WHERE ref_id IS NOT NULL AND ref_kind <> '';
