// Shared helpers for the k6 scripts. Kept tiny on purpose: anything bigger
// belongs in its own module so each test stays readable on its own.
import http from 'k6/http';
import { check } from 'k6';

// BASE_URL defaults to the dev docker-compose API. Override via env var when
// pointing the suite at staging or a specific PR preview.
export const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

// Common workload knobs — every script honours VUS / DURATION so the same
// scenario can scale up for a stress run without editing the file.
export const VUS = Number(__ENV.VUS) || undefined;
export const DURATION = __ENV.DURATION || undefined;

// get wraps http.get with the auth-free defaults we use for public reads:
// JSON Accept header, the deployment's base URL, and a tag so dashboards
// can slice metrics per logical workload (not per URL — search.js fans out
// across many `/properties?<filters>` URLs that we want grouped).
export function get(path, opts = {}) {
  const url = `${BASE_URL}${path}`;
  const params = {
    headers: { Accept: 'application/json' },
    ...opts,
  };
  return http.get(url, params);
}

// checkOK is the canonical "response is healthy" assertion. Each script
// chains its own response-shape checks on top via the third argument.
export function checkOK(res, label, extra = {}) {
  return check(res, {
    [`${label}: status is 200`]: (r) => r.status === 200,
    [`${label}: body non-empty`]: (r) => r.body && r.body.length > 0,
    ...extra,
  });
}

// pickRandomID walks an `{items:[{id, ...}]}` response and returns one id,
// or null when the list is empty. Used by detail/availability scripts to
// avoid hardcoding ids in URLs.
export function pickRandomID(items) {
  if (!items || items.length === 0) return null;
  // k6's randomSeed is per-script — Math.random is fine for picking a fixture.
  return items[Math.floor(Math.random() * items.length)].id;
}
