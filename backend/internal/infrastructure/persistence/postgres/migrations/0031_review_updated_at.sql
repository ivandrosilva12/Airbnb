-- 0031_review_updated_at.sql — track author edits to a review (edit/delete window).

ALTER TABLE reviews ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ;
