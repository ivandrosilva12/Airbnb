-- 0016_reports.sql — listing/abuse reports.
--
-- A user flags a property listing for moderation; a platform administrator
-- resolves or dismisses the report. A user may hold at most one open report per
-- listing (enforced in the application layer).

CREATE TABLE IF NOT EXISTS reports (
    id          UUID PRIMARY KEY,
    property_id UUID        NOT NULL REFERENCES properties (id) ON DELETE CASCADE,
    reporter_id UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    reason      TEXT        NOT NULL CHECK (reason IN ('spam', 'inappropriate', 'scam', 'inaccurate', 'other')),
    note        TEXT        NOT NULL DEFAULT '',
    status      TEXT        NOT NULL CHECK (status IN ('open', 'resolved', 'dismissed')),
    resolution  TEXT        NOT NULL DEFAULT '',
    resolver_id UUID        REFERENCES users (id) ON DELETE SET NULL,
    resolved_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_reports_status ON reports (status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_reports_property ON reports (property_id);
