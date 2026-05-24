# AirHost — Alert runbooks

Each Prometheus alert in [`infra/prometheus/alerts.yml`](../../infra/prometheus/alerts.yml)
carries a `runbook_url` annotation that links to the matching page below.
Alertmanager renders that link in both the Slack message and the e-mail, so the
on-call engineer can jump straight from the notification to the diagnosis steps.

| Alert | Severity | Runbook |
| --- | --- | --- |
| `AirhostApiDown` | critical | [AirhostApiDown.md](AirhostApiDown.md) |
| `AirhostHighErrorRate` | warning | [AirhostHighErrorRate.md](AirhostHighErrorRate.md) |
| `AirhostWebhookRejectionSpike` | warning | [AirhostWebhookRejectionSpike.md](AirhostWebhookRejectionSpike.md) |
| `AirhostRateLimitingSustained` | warning | [AirhostRateLimitingSustained.md](AirhostRateLimitingSustained.md) |
| `AirhostWebhookProcessingErrors` | warning | [AirhostWebhookProcessingErrors.md](AirhostWebhookProcessingErrors.md) |

## Conventions

- **`runbook_url` base** — the alerts point at
  `https://github.com/airhost/airhost/blob/main/docs/runbooks/<Alert>.md`.
  If you host the repo elsewhere, update the `runbook_url` annotations in
  `alerts.yml` (and re-load Prometheus) so the links resolve.
- **Dashboards** — the *AirHost API* Grafana dashboard
  (`infra/grafana/dashboards/airhost-api.json`) is the primary place to confirm
  scope and blast radius. Panels referenced from the runbooks live there.
- **Silencing** — to mute an alert during planned maintenance, use the admin
  silence endpoint (`POST /admin/alerts/silences`) rather than editing rules.

## General triage order

1. Open the *AirHost API* dashboard and confirm the alert against the relevant panel.
2. Check `up{job="airhost-api"}` — many warnings are downstream of the API being down.
3. Inspect API logs (`docker compose logs -f api`) around the alert's start time.
4. If the cause is understood and a fix is deploying, silence the alert; otherwise escalate.
