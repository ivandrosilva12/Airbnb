# Runbook — `AirhostWebhookRejectionSpike`

**Severity:** warning · **Fires after:** `sum(rate(airhost_webhook_events_total{outcome="rejected"}[5m])) by (provider) > 0.2` for 5m

## What it means

More than 0.2 webhook deliveries per second (5m avg) are being **rejected** for a
given payment `provider`. A webhook is rejected when its signature fails
verification — either the HMAC does not match or the Stripe timestamp is outside
the 5-minute anti-replay window.

## Impact

Rejected webhooks are **not** reconciled, so payment state can drift from the
gateway (a capture/refund/failure at the provider may never be reflected locally).
This is also the signature that fires on a **forgery or replay attempt**.

## Diagnose

1. **Which provider?** The alert label `provider` is `stripe`, `appypay` or
   `gpayangola`. Confirm on Grafana → *AirHost API* → *Webhook events by outcome*.
2. **Misconfiguration vs. attack:**
   - A spike that starts right after a deploy/secret rotation ⇒ almost certainly a
     **wrong secret**. Check the env var for that provider:
     `STRIPE_WEBHOOK_SECRET`, `APPYPAY_WEBHOOK_SECRET`, `GPAYANGOLA_WEBHOOK_SECRET`.
   - A spike from unexpected source IPs with otherwise-correct traffic ⇒ possible
     **forgery/replay**. Check the reverse-proxy/access logs for the source.
3. **Stripe-specific** — if `outcome="rejected"` for `stripe` with the right
   secret, suspect clock skew (timestamp anti-replay). Verify the API container
   clock; tolerance is `stripeReplayTolerance = 5m`.
4. **Logs:**
   ```sh
   docker compose logs --tail=300 api | grep -iE "webhook|signature|verif"
   ```

## Remediate

- **Wrong secret:** set the correct `*_WEBHOOK_SECRET` from the provider
  dashboard and restart the API. Ask the provider to re-deliver missed events.
- **Clock skew:** fix NTP on the host; the window will start matching again.
- **Attack:** block the offending source upstream; secrets are unaffected because
  rejected events never reach reconciliation.

## Verify resolved

`rate(airhost_webhook_events_total{outcome="rejected"}[5m])` drops below 0.2/s and
the `reconciled` outcome resumes for that provider.
