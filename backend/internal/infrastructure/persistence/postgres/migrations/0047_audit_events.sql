-- Audit trail for admin-initiated state changes (S45). Append-only:
-- nothing in the application ever updates or deletes a row here. The
-- table is intentionally separate from the outbox; outbox events feed
-- in-process subscribers and may be re-delivered, audit events are
-- the system of record for "who did what when" and must survive
-- replay.
CREATE TABLE IF NOT EXISTS audit_events (
    id           UUID        PRIMARY KEY,
    actor_id     UUID        NOT NULL,
    action       TEXT        NOT NULL,
    target_type  TEXT        NOT NULL,
    target_id    UUID        NOT NULL,
    metadata     JSONB       NOT NULL DEFAULT '{}'::JSONB,
    request_id   TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Read patterns:
--   "show me the last N audit events"          → idx on created_at DESC
--   "what did admin X do?"                     → idx on actor_id
--   "what happened to property/dispute Y?"     → composite on target
CREATE INDEX IF NOT EXISTS idx_audit_events_created_at
    ON audit_events (created_at DESC);

CREATE INDEX IF NOT EXISTS idx_audit_events_actor
    ON audit_events (actor_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_audit_events_target
    ON audit_events (target_type, target_id, created_at DESC);
