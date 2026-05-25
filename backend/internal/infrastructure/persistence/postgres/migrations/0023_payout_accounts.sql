-- 0023_payout_accounts.sql — per-host Stripe Connect payout accounts.
--
-- A host onboards a Stripe Connect connected account to receive payouts; its id
-- and the payouts_enabled flag live on the user profile. Existing users default
-- to no account (must onboard before requesting a disbursement).

ALTER TABLE users ADD COLUMN IF NOT EXISTS payout_account_id TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS payouts_enabled BOOLEAN NOT NULL DEFAULT false;
