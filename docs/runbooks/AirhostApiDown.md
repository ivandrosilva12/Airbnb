# Runbook — `AirhostApiDown`

**Severity:** critical · **Fires after:** `up{job="airhost-api"} == 0` for 1m

## What it means

Prometheus has been unable to scrape the AirHost API `/metrics` endpoint for over
a minute. The API process is either crashed, unreachable on the network, or
stuck (not serving HTTP). While this fires, the `inhibit_rules` mute the noisier
`warning` alerts so the page stays focused.

## Impact

The public API is almost certainly down for users: bookings, search, payments
and webhooks all fail. Treat as a customer-facing outage.

## Diagnose

1. **Confirm scope** — Grafana → *AirHost API* → *Request rate* / *In-flight
   requests*. Flat-lined at zero corroborates the outage.
2. **Is the container up?**
   ```sh
   docker compose ps api
   docker compose logs --tail=200 api
   ```
   Look for a panic stack trace, an `OOMKilled` status, or a failed dependency
   (Postgres/Keycloak/MinIO) at boot.
3. **Health endpoints** (from inside the network):
   ```sh
   docker compose exec api wget -qO- http://localhost:8081/healthz
   docker compose exec api wget -qO- http://localhost:8081/readyz
   ```
   `/readyz` failing points at a dependency (DB connection, etc.).
4. **Dependencies** — `docker compose ps` for `db`, `keycloak`, `minio`. A
   crash-loop on startup is usually a bad migration or an unreachable dependency.

## Remediate

- **Crash / panic:** capture the stack trace, then `docker compose restart api`.
  If it crash-loops, roll back to the previous image/commit.
- **Dependency down:** restore the dependency first; `/readyz` will recover and
  the alert resolves on the next scrape.
- **OOM:** raise the container memory limit and investigate the leak/regression.

## Verify resolved

`up{job="airhost-api"}` returns to `1` and the *Request rate* panel recovers.
Alertmanager sends the resolved notification to `resolved@airhost.dev` /
`#airhost-resolved`.
