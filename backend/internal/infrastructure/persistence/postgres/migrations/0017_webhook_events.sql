-- 0017_webhook_events.sql — processed payment-gateway webhook deliveries.
--
-- Each row records a delivery the API has handled, keyed by (provider,
-- event_id), so retried or duplicated webhooks are skipped at storage level.
-- Reconciliation itself is already idempotent; this avoids redundant work and
-- provides an audit trail.

CREATE TABLE IF NOT EXISTS webhook_events (
    provider   TEXT        NOT NULL,
    event_id   TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, event_id)
);
