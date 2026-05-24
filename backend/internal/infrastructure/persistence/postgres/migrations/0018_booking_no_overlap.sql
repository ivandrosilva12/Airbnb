-- Enforce "no double-booking" at the database level. The application-level
-- HasOverlap check in bookingapp.Create is a check-then-insert that races under
-- concurrency; this exclusion constraint is the authoritative guarantee.
--
-- btree_gist provides GiST operator classes for scalar equality, letting one
-- EXCLUDE constraint combine `property_id WITH =` and the half-open stay
-- interval `WITH &&` (overlap). The partial predicate scopes it to active
-- bookings, so completed/cancelled stays free the dates again.
CREATE EXTENSION IF NOT EXISTS btree_gist;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'bookings_no_overlap'
    ) THEN
        ALTER TABLE bookings
            ADD CONSTRAINT bookings_no_overlap
            EXCLUDE USING gist (
                property_id WITH =,
                tstzrange(check_in, check_out, '[)') WITH &&
            ) WHERE (status IN ('pending', 'confirmed'));
    END IF;
END$$;
