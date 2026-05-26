-- 0032_report_targets.sql — generalise reports to target a listing or a review.
-- A report still carries property_id (the listing involved), so the moderation
-- queue can show the listing title regardless of target type.

ALTER TABLE reports ADD COLUMN IF NOT EXISTS target_type TEXT NOT NULL DEFAULT 'listing';
ALTER TABLE reports ADD COLUMN IF NOT EXISTS target_id UUID;

-- Existing rows are all listing reports: their target is the property itself.
UPDATE reports SET target_id = property_id WHERE target_id IS NULL;
ALTER TABLE reports ALTER COLUMN target_id SET NOT NULL;

ALTER TABLE reports ADD CONSTRAINT reports_target_type_chk CHECK (target_type IN ('listing', 'review'));
CREATE INDEX IF NOT EXISTS idx_reports_target ON reports (target_type, target_id);
