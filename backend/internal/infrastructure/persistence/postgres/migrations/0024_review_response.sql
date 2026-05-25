-- 0024_review_response.sql — host public reply to a guest's property review.

ALTER TABLE reviews ADD COLUMN IF NOT EXISTS response TEXT NOT NULL DEFAULT '';
ALTER TABLE reviews ADD COLUMN IF NOT EXISTS responded_at TIMESTAMPTZ;
