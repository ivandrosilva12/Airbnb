-- 0026_wishlist_collections.sql — named wishlist collections.
-- A user organises their saved listings into named lists; a favorite may be
-- filed under one collection (collection_id NULL = the default "Saved" bucket).

CREATE TABLE IF NOT EXISTS wishlist_collections (
    id         UUID        PRIMARY KEY,
    user_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

-- One collection name per user (case-insensitive).
CREATE UNIQUE INDEX IF NOT EXISTS uq_wishlist_collections_user_name
    ON wishlist_collections (user_id, lower(name));
CREATE INDEX IF NOT EXISTS idx_wishlist_collections_user ON wishlist_collections (user_id);

-- Deleting a collection drops its listings back to the default bucket rather
-- than unsaving them.
ALTER TABLE favorites
    ADD COLUMN IF NOT EXISTS collection_id UUID REFERENCES wishlist_collections (id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_favorites_collection ON favorites (user_id, collection_id);
