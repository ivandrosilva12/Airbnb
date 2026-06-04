-- 0047_push_token_endpoint.sql
--
-- S96 — Web Push: real flow (service worker + VAPID + push_tokens.web).
--
-- The platform check in 0039 already accepts 'web', but a browser push
-- subscription is more than a single FCM/APNs token: it is an (endpoint,
-- p256dh, auth) triple. The Push Service URL (endpoint) is long-form (200+
-- chars), the public client key + auth secret are short base64-url strings.
--
-- We keep the original `token` column as the unique identity for the row
-- (the platform-token uniqueness already in place still works), and add
-- one optional `endpoint` column. For web rows the client sends the
-- endpoint URL as `token` and the JSON-encoded {p256dh, auth} blob as
-- `endpoint`'s text payload — the sender keeps both halves available
-- without forcing a wider schema on the FCM/APNs rows.
--
-- A simpler alternative — packing every field into `token` — would have
-- meant string-splitting on the hot path. The dedicated column makes the
-- web push sender's query trivially typed and lets future migrations
-- add per-platform indexes without touching the FCM/APNs path.

ALTER TABLE push_tokens
    ADD COLUMN endpoint text;
