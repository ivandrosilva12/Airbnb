-- S160 — RFC-style Idempotency-Key middleware on mutating endpoints.
--
-- Mobile clients on flaky networks retry POST/PATCH/PUT/DELETE requests
-- and would otherwise risk duplicate writes (the booking EXCLUDE
-- constraint catches double-bookings, but conversations, reviews,
-- offers, splits etc. have no such backstop). A client-supplied
-- Idempotency-Key dedupes the SAME mutation across retries: a hit
-- with the same (method, path, body_hash) replays the original
-- response; a hit with a DIFFERENT request body under the same key
-- returns 409 Conflict (the client recycled the key by accident).
--
-- The composite PK (user_id, key) scopes the namespace to the
-- authenticated caller, so one user's key cannot leak another user's
-- captured response — even if a malicious client guesses the literal
-- key string of a victim.
CREATE TABLE IF NOT EXISTS request_idempotency (
    key TEXT NOT NULL,
    user_id UUID NOT NULL,
    method TEXT NOT NULL,
    path TEXT NOT NULL,
    body_hash BYTEA NOT NULL,
    status_code INT NOT NULL,
    response_body BYTEA NOT NULL,
    response_content_type TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, key)
);

-- Supports the hourly cleanup sweep that purges entries older than the
-- 24h TTL window. The PK already covers the (user_id, key) lookup path
-- the middleware uses on every request, so no extra index is needed
-- there.
CREATE INDEX IF NOT EXISTS idx_request_idempotency_created_at
    ON request_idempotency(created_at);
