-- 0009_listing_ratings.sql — denormalised review aggregates on listings.

ALTER TABLE properties ADD COLUMN IF NOT EXISTS average_rating DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE properties ADD COLUMN IF NOT EXISTS review_count   INTEGER          NOT NULL DEFAULT 0;

-- Backfill from any existing guest-to-property reviews.
UPDATE properties p SET
    average_rating = COALESCE(s.avg, 0),
    review_count   = COALESCE(s.cnt, 0)
FROM (
    SELECT property_id, AVG(rating)::double precision AS avg, COUNT(*) AS cnt
    FROM reviews
    WHERE kind = 'guest_to_property'
    GROUP BY property_id
) s
WHERE p.id = s.property_id;

CREATE INDEX IF NOT EXISTS idx_properties_rating ON properties (average_rating);
