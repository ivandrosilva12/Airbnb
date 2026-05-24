-- 0021_instant_book.sql — per-listing instant-book flag.
--
-- When true, a guest's reservation is auto-confirmed at creation time instead of
-- being held as "pending" for the host to approve. Existing listings default to
-- request-to-book (false).

ALTER TABLE properties ADD COLUMN IF NOT EXISTS instant_book BOOLEAN NOT NULL DEFAULT false;
