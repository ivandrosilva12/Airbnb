-- 0005_notifications.sql — in-app notifications.

CREATE TABLE IF NOT EXISTS notifications (
    id         UUID PRIMARY KEY,
    user_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    type       TEXT        NOT NULL,
    title      TEXT        NOT NULL,
    body       TEXT        NOT NULL DEFAULT '',
    related_id UUID,
    read_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_notifications_user ON notifications (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_unread ON notifications (user_id) WHERE read_at IS NULL;
