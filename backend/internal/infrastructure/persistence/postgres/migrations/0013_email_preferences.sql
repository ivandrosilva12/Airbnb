-- 0013_email_preferences.sql — per-user transactional email opt-outs.
--
-- In-app notifications are always delivered; these flags only gate whether the
-- matching transactional email is sent. Existing users default to opted-in.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS email_opt_bookings BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS email_opt_messages BOOLEAN NOT NULL DEFAULT TRUE;
