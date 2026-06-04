-- S97 — outbox dead-letter columns (WF-GAP-018).
--
-- Records that exceed the relay's retry budget land in dead-letter state instead
-- of looping forever. The columns live on the existing `outbox` table — same
-- payload, same id — so the entire record's history (attempts, last_error,
-- when it was dead-lettered) is preserved for admin inspection and requeue.
--
-- last_error captures the most recent failure reason so the operator can read
-- it before deciding to requeue or drop.
ALTER TABLE outbox
    ADD COLUMN IF NOT EXISTS dead_lettered_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS last_error TEXT NULL;

-- Rebuild the pending-work index so it excludes DLQ rows: the recovery scan
-- already filters by processed_at IS NULL AND failed_at IS NULL; this keeps
-- it cheap even as DLQ rows accumulate.
DROP INDEX IF EXISTS idx_outbox_pending;
CREATE INDEX idx_outbox_pending ON outbox (created_at)
    WHERE processed_at IS NULL AND failed_at IS NULL AND dead_lettered_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_outbox_dlq ON outbox (dead_lettered_at)
    WHERE dead_lettered_at IS NOT NULL;
