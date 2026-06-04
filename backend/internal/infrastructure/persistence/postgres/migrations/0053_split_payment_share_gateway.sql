-- 0053_split_payment_share_gateway.sql — per-share gateway tracking on split payments.
--
-- WF-GAP-001/004/005 (S88). The split-payment BC previously tracked who owed
-- what but never moved real money: AuthorizeShare flipped a row to "paid" in
-- trust mode with no gateway hold, BookingCancelled left every contributor's
-- card un-refunded, and the payout subscriber knew nothing about splits.
--
-- This migration adds the per-share columns the new flow needs:
--
--   * gateway_ref     — the per-share transaction reference returned by the
--                       payment gateway when the hold was authorized. NULL on
--                       pending/failed shares.
--   * failure_reason  — the gateway's rejection text on a failed authorize or
--                       refund attempt, surfaced to operators for triage.
--
-- The CHECK on status is widened to accept the two new transitions
-- (failed, refunded) introduced by the domain.

ALTER TABLE split_payment_shares
    ADD COLUMN IF NOT EXISTS gateway_ref    TEXT,
    ADD COLUMN IF NOT EXISTS failure_reason TEXT;

-- Drop the old narrow CHECK and replace with the widened state set. The
-- IF EXISTS guard keeps the migration idempotent against partial re-runs.
ALTER TABLE split_payment_shares DROP CONSTRAINT IF EXISTS split_payment_shares_status_check;
ALTER TABLE split_payment_shares
    ADD CONSTRAINT split_payment_shares_status_check
    CHECK (status IN ('pending', 'paid', 'failed', 'refunded'));
