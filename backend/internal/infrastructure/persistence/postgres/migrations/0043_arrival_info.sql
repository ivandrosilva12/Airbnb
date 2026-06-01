-- 0043_arrival_info.sql — arrival/wifi credentials hosts attach to a listing.
-- These are sensitive: never embedded in public listing/search responses, and
-- only served to a confirmed guest through /bookings/:id/arrival within the
-- 48h-before-checkin → checkout visibility window enforced at the HTTP layer.

ALTER TABLE properties
    ADD COLUMN IF NOT EXISTS check_in_method        TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS arrival_instructions   TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS wifi_ssid              TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS wifi_password          TEXT NOT NULL DEFAULT '';

ALTER TABLE properties
    ADD CONSTRAINT properties_check_in_method_check CHECK (
        check_in_method IN ('', 'self_lockbox', 'smart_lock', 'key_exchange', 'host_greeting')
    );
