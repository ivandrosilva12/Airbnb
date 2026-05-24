# Runbook — `AirhostHighErrorRate`

**Severity:** warning · **Fires after:** `sum(rate(airhost_http_requests_total{status=~"5.."}[5m])) > 1` for 5m

## What it means

The API is returning more than one HTTP 5xx response per second, averaged over
5 minutes, sustained for 5 minutes. Something server-side is failing for a
meaningful share of requests.

## Impact

Users hit errors intermittently. Severity depends on which routes are affected —
a 5xx on `POST /bookings` is worse than on a rarely used admin route.

## Diagnose

1. **Which routes?** Grafana → *AirHost API* → *Error rate (5xx)* and *Request
   rate* (by route). Then break down in Prometheus:
   ```promql
   sum(rate(airhost_http_requests_total{status=~"5.."}[5m])) by (route, status)
   ```
2. **Logs for those routes:**
   ```sh
   docker compose logs --tail=300 api | grep -E "5[0-9]{2}|panic|error"
   ```
3. **Dependency health** — most 5xx spikes trace to a downstream dependency:
   - Postgres: connection pool exhaustion, slow queries, failed migration.
   - Keycloak: token validation failing (JWKS unreachable) → check `/readyz`.
   - MinIO/S3: photo upload/download routes 5xx if object storage is down.
   - Payment gateways: see *Webhook events by outcome* and the gateway logs.
4. **Recent deploy?** Correlate the start time with the latest release/commit.

## Remediate

- **Bad deploy:** roll back; the rate should fall within one scrape interval.
- **Dependency:** restore/scale the dependency; verify the pool/timeout settings.
- **Single hot route:** if it is non-critical, consider a targeted fix; if it is
  critical (bookings/payments), escalate.

## Verify resolved

The *Error rate (5xx)* panel returns to baseline and the alert clears after the
`for: 5m` window with the rate back under threshold.
