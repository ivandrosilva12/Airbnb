-- Multi-criteria property reviews: optional per-aspect sub-ratings (1..5), with
-- 0 meaning the guest did not rate that aspect. Only guest-to-property reviews
-- populate these; host-to-guest reviews leave them at 0.
ALTER TABLE reviews
    ADD COLUMN IF NOT EXISTS rating_cleanliness   SMALLINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS rating_accuracy      SMALLINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS rating_communication SMALLINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS rating_location      SMALLINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS rating_checkin       SMALLINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS rating_value         SMALLINT NOT NULL DEFAULT 0;
