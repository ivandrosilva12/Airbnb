-- Transactional-outbox table: domain events are recorded here and relayed to
-- in-process subscribers. A record left unprocessed (e.g. a crash mid-dispatch)
-- is re-delivered on startup / by the periodic relay.
CREATE TABLE IF NOT EXISTS outbox (
    id           UUID PRIMARY KEY,
    event_name   TEXT        NOT NULL,
    payload      JSONB       NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ,
    failed_at    TIMESTAMPTZ,
    attempts     INT         NOT NULL DEFAULT 0
);

-- Partial index over the work queue (records still awaiting delivery).
CREATE INDEX IF NOT EXISTS idx_outbox_pending ON outbox (created_at)
    WHERE processed_at IS NULL AND failed_at IS NULL;
