# Runbook — `AirhostRateLimitingSustained`

**Severity:** warning · **Fires after:** `sum(rate(airhost_rate_limited_total[5m])) by (route) > 0.5` for 10m

## What it means

A route has been rejecting more than 0.5 requests per second with HTTP 429
(`Too Many Requests`) for over 10 minutes. The in-process per-IP token bucket
(`middleware.RateLimit`) is shedding load on that route.

## Impact

Legitimate clients on the throttled route may be getting turned away. The
webhook route (`POST /webhooks/payments/:provider`) is the most common subject —
sustained 429s there can delay payment reconciliation.

## Diagnose

1. **Which route?** Label `route` on the alert; corroborate on Grafana →
   *AirHost API* → *Rate-limited rejections (per minute)*.
2. **One IP or many?** A single hammering IP (a buggy client, a retry storm, a
   scraper) vs. broad load. Check the reverse-proxy/access logs grouped by IP.
3. **Is it a retry storm?** A gateway re-delivering webhooks because earlier
   attempts errored will look like sustained 429s — cross-check
   *Webhook events by outcome* for a parallel `error` spike (then see
   [`AirhostWebhookProcessingErrors`](AirhostWebhookProcessingErrors.md)).
4. **Config** — current limits come from `SecurityConfig`:
   `WEBHOOK_RATE_RPS` (default 5) and `WEBHOOK_RATE_BURST` (default 20).

## Remediate

- **Abusive single IP:** block it upstream; the bucket recovers immediately.
- **Legit traffic outgrowing the limit:** raise `WEBHOOK_RATE_RPS` /
  `WEBHOOK_RATE_BURST` and restart the API. Re-evaluate after.
- **Retry storm from our own errors:** fix the underlying processing error so the
  gateway stops retrying; the 429s subside as deliveries succeed.

## Verify resolved

`rate(airhost_rate_limited_total[5m])` for the route falls below 0.5/s and stays
there past the `for: 10m` window.
