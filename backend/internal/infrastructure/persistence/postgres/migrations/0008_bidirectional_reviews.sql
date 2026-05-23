-- 0008_bidirectional_reviews.sql — host-to-guest reviews alongside guest-to-property.

ALTER TABLE reviews ADD COLUMN IF NOT EXISTS kind      TEXT NOT NULL DEFAULT 'guest_to_property';
ALTER TABLE reviews ADD COLUMN IF NOT EXISTS author_id UUID;

-- Existing reviews were written by the guest about the property.
UPDATE reviews SET author_id = guest_id WHERE author_id IS NULL;

-- A booking may now have one review per direction, not just one overall.
ALTER TABLE reviews DROP CONSTRAINT IF EXISTS reviews_booking_id_key;
CREATE UNIQUE INDEX IF NOT EXISTS uq_reviews_booking_kind ON reviews (booking_id, kind);

-- Index for "reviews about a guest" lookups.
CREATE INDEX IF NOT EXISTS idx_reviews_guest_subject ON reviews (guest_id) WHERE kind = 'host_to_guest';
