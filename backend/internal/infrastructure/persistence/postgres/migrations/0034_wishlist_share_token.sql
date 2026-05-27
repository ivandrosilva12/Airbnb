-- 0034_wishlist_share_token.sql — public share link for a wishlist collection.

ALTER TABLE wishlist_collections ADD COLUMN IF NOT EXISTS share_token TEXT;

-- Tokens are unguessable and unique; the partial index ignores private (NULL)
-- collections so only shared ones occupy the namespace.
CREATE UNIQUE INDEX IF NOT EXISTS uq_wishlist_collections_share_token
    ON wishlist_collections (share_token) WHERE share_token IS NOT NULL;
