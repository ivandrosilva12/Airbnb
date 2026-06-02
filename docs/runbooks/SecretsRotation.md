# Secrets rotation

This runbook catalogues every secret the AirHost stack consumes, sets a
recommended rotation cadence, and lists the exact steps for each one.
The startup-time validator (`config.Validate`) refuses to boot in
production when known weak placeholders are detected — this document
is the procedure for replacing them with the right values, and for
rotating those values on schedule.

## Inventory

| Env var | Component | Cadence | Owner | Notes |
| --- | --- | --- | --- | --- |
| `DB_PASSWORD` | PostgreSQL | 90 days | Platform | Rotated via the Postgres role; app reconnects on next request |
| `MINIO_ACCESS_KEY` / `MINIO_SECRET_KEY` | MinIO object store | 90 days | Platform | Issue a fresh service account, swap in env, restart API |
| `KEYCLOAK_*` | Keycloak OIDC | n/a (config) | Platform | Issuer URL is not a secret; client secrets live in Keycloak itself |
| `STRIPE_SECRET_KEY` | Stripe payments | 180 days or on incident | Payments | Rotate via Stripe dashboard → roll API key |
| `STRIPE_WEBHOOK_SECRET` | Stripe webhooks | 180 days or on incident | Payments | Per webhook endpoint; new secret returned by Stripe |
| `STRIPE_CONNECT_WEBHOOK_SECRET` | Stripe Connect | 180 days or on incident | Payments | Same procedure, separate endpoint |
| `APPYPAY_TOKEN` | AppyPay payments | 30 days | Payments | OAuth2 client-credentials token; ideally minted by a sidecar |
| `APPYPAY_WEBHOOK_SECRET` | AppyPay webhooks | 180 days or on incident | Payments | Shared secret with AppyPay ops |
| `GPAYANGOLA_API_KEY` | GPay Angola | 180 days or on incident | Payments | Issued by GPay Angola ops |
| `GPAYANGOLA_WEBHOOK_SECRET` | GPay Angola webhooks | 180 days or on incident | Payments | Shared secret |
| `ALERT_WEBHOOK_TOKEN` | Alertmanager → /webhooks/alerts | 180 days | Platform | Bearer token Alertmanager carries |
| `SMTP_PASSWORD` | Outbound email | 180 days | Platform | Provider-issued (Postmark / SendGrid / SES) |
| `FCM_SERVICE_ACCOUNT_JSON` | Firebase Cloud Messaging | n/a (long-lived) | Mobile | Rotate via Firebase console → service account → new key |
| `APNS_PRIVATE_KEY` | Apple Push Notifications | n/a (long-lived) | Mobile | Generated in Apple Developer; .p8 file |

Rotation cadences are guidance, not policy. **Any suspected leak triggers
immediate rotation regardless of age**; the cadence is the upper bound.

## General procedure (Docker Compose deployment)

1. **Mint the new secret** in the upstream system (Stripe dashboard,
   Postgres `ALTER ROLE`, MinIO service-account generator, etc.).
2. **Stage the new env value** in your deployment secret store
   (Vault/Doppler/AWS Secrets Manager). Do NOT edit `.env` files
   tracked in git — those are placeholders for dev only.
3. **Roll one API replica at a time** with the new env so the old
   secret stays valid during the cut-over. The validator
   (`config.Validate`) refuses to boot in production if the new
   value is a known placeholder — that is the safety net for
   typos.
4. **Confirm health** via `/healthz` and the payment dashboard's
   "last successful charge" / "last received webhook" timestamps.
5. **Revoke the old secret** in the upstream system.
6. **Record the rotation** in the platform-team rotation log
   (date, owner, env var, reason).

## Per-secret cut-over notes

### Payment-provider webhook secrets (Stripe / AppyPay / GPay Angola)

Webhook rotation is the trickiest case because in-flight events
signed with the OLD secret arrive AFTER the API has the NEW one,
and the API rejects them with a 4xx. Two strategies:

- **Dual-secret window**: most providers accept multiple signing
  secrets simultaneously. Add the new secret in the dashboard
  before adding it to the API; once every API replica has the new
  secret, remove the old one from the provider. Requires app
  support for accepting either secret — currently NOT implemented
  (the verifier takes a single secret). Treat this as a
  follow-up.
- **Drain-and-cut**: stop new event generation for ~30 seconds
  (Stripe dashboard supports pausing webhook delivery), wait for
  the in-flight queue to drain, swap the secret, re-enable. Brief
  signature-failure window on long-delayed events is tolerated;
  the webhook idempotency persistence (S71) makes redeliveries
  safe.

### Database password

`pgx`'s connection pool reconnects on the next request. To avoid
a thundering herd on the new password, roll the API replicas
one at a time. The migrations runner reads the same env so a
deployment with a fresh password during a migration window will
fail at startup — sequence the rotation OUTSIDE migration windows.

### MinIO credentials

S3 presigned URLs the API has already minted continue to work
until they expire (default 1h). After rotation, photos uploaded
through the API succeed immediately because the new access key
is server-side. No grace window required for reads — MinIO does
not invalidate already-presigned URLs on key rotation.

### FCM / APNs

These are long-lived service credentials. Rotation is incident-
driven (suspected leak, employee turnover). The procedure
mirrors the API-key cases above; mobile clients do not need any
change.

## Validation contract

`config.Validate` (see `backend/internal/config/validate.go`):

- In `APP_ENV=production`, returns a non-nil error for every
  placeholder detected, refusing the boot.
- In every other environment, returns nil with warnings logged at
  start-up so developers see the same diagnosis without being
  blocked.

Add new rotation-relevant env vars to the validator as the catalogue
above grows.
