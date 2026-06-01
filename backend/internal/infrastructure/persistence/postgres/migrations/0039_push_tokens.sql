-- 0039_push_tokens.sql
--
-- S12 — Push notifications nativos. A push_tokens table stores the device
-- registration handed back by the OS (FCM token for Android, APNs device token
-- for iOS, Web Push endpoint for browsers) so the notification subscriber can
-- fan a domain event out to all of a user's devices. (platform, token) is
-- unique so the same physical handset re-registering only updates last_seen.
--
-- Users also gain push_bookings/push_messages opt-outs that mirror the existing
-- email_opt_bookings/email_opt_messages columns, so a user can mute push for a
-- category independently of email.

CREATE TABLE push_tokens (
    id          uuid PRIMARY KEY,
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    platform    text NOT NULL CHECK (platform IN ('ios', 'android', 'web')),
    token       text NOT NULL,
    last_seen   timestamptz NOT NULL DEFAULT now(),
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX push_tokens_platform_token_idx ON push_tokens (platform, token);
CREATE INDEX push_tokens_user_id_idx ON push_tokens (user_id);

ALTER TABLE users
    ADD COLUMN push_bookings boolean NOT NULL DEFAULT true,
    ADD COLUMN push_messages boolean NOT NULL DEFAULT true;
