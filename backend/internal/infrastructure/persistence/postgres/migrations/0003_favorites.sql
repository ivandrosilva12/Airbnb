-- 0003_favorites.sql — guest wishlists.

CREATE TABLE IF NOT EXISTS favorites (
    user_id     UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    property_id UUID        NOT NULL REFERENCES properties (id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (user_id, property_id)
);

CREATE INDEX IF NOT EXISTS idx_favorites_user ON favorites (user_id, created_at DESC);
