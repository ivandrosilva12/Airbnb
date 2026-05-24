# Runbook — `AirhostWebhookProcessingErrors`

**Severity:** warning · **Fires after:** `sum(rate(airhost_webhook_events_total{outcome="error"}[5m])) > 0` for 5m

## What it means

The API is accepting webhooks (signature OK, not a duplicate) but **failing to
reconcile** them — `ReconcileGatewayEvent` is returning an error, recorded as
`outcome="error"`. Because the dedupe key is only `Record`-ed **after** a
successful reconcile, the gateway will keep retrying, so errors persist until the
root cause is fixed.

## Impact

Payment state drifts from the gateway: captures, refunds and failures reported by
the provider are not applied to the local booking/payment. The repeated retries
can also trip [`AirhostRateLimitingSustained`](AirhostRateLimitingSustained.md).

## Diagnose

1. **Logs first** — the error text is the fastest signal:
   ```sh
   docker compose logs --tail=300 api | grep -iE "reconcile|webhook|gateway"
   ```
2. **Common causes:**
   - **Unknown reference** — the event's reference does not match any payment.
     Recall refs are tagged `provider:ref`; `findByGatewayRef` tries the tagged
     and bare forms. A mismatch suggests events from a different account/env.
   - **DB failure** — Postgres unavailable or a constraint violation when
     persisting the new payment state.
   - **Unexpected event shape** — a provider event type/field the parser does not
     handle yet.
3. **Scope** — Grafana → *AirHost API* → *Webhook events by outcome*; check
   whether it is one provider or all (DB-wide issue).

## Remediate

- **DB issue:** restore Postgres / fix the constraint; retries then succeed and
  the key gets recorded.
- **Code/parse bug:** ship a fix; the gateway's retries reconcile the backlog
  once deploys land.
- **Wrong environment:** if events are from a foreign account, stop that delivery
  at the provider; do **not** widen lookup to avoid mis-applying payments.

## Verify resolved

`rate(airhost_webhook_events_total{outcome="error"}[5m])` returns to 0 and the
same deliveries show up as `reconciled` (or `duplicate` on retry).
