-- 0036_saved_searches.sql — a guest's saved searches and their new-listing alerts.

CREATE TABLE IF NOT EXISTS saved_searches (
    id               UUID PRIMARY KEY,
    user_id          UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name             TEXT        NOT NULL,
    query            TEXT        NOT NULL DEFAULT '',
    alerts_enabled   BOOLEAN     NOT NULL DEFAULT false,
    last_notified_at TIMESTAMPTZ NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_saved_searches_user ON saved_searches (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_saved_searches_alertable ON saved_searches (alerts_enabled) WHERE alerts_enabled;
