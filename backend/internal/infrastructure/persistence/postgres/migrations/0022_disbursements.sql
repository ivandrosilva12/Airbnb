-- 0022_disbursements.sql — host payout disbursements.
--
-- A disbursement pays a host's available balance to their external account. The
-- available balance is the net earnings ledger minus the sum of committed
-- (pending + paid) disbursements, so funds are never paid out twice.

CREATE TABLE IF NOT EXISTS disbursements (
    id           UUID PRIMARY KEY,
    host_id      UUID NOT NULL,
    currency     TEXT NOT NULL,
    amount_cents BIGINT NOT NULL CHECK (amount_cents > 0),
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'paid', 'failed')),
    gateway_ref  TEXT NOT NULL DEFAULT '',
    failure      TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_disbursements_host ON disbursements (host_id, created_at DESC);
