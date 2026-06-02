// Constant 15 VUs hitting /me/cohost-mailbox under load. The S27 N+1 fix
// collapsed this from "1 + N queries per listing the caller co-hosts" to
// a single query via cohosts.ListByUser; this script is the regression
// gate for that win. A spike here likely means someone re-introduced a
// per-property lookup in the cohost permission gate.
//
// Uses the "host" seeded user — they're the cleanest fit for exercising
// a populated cohort mailbox (the realm's guest user has no cohost
// grants by default). For a fully-loaded test, seed cohost grants for
// the host user first (out of scope for the scaffold).
import { sleep } from 'k6';
import http from 'k6/http';
import { check } from 'k6';
import { BASE_URL, VUS, DURATION } from './lib.js';
import { authHeaders } from './auth.js';

export const options = {
  scenarios: {
    cohost_mailbox_steady: {
      executor: 'constant-vus',
      vus: VUS || 15,
      duration: DURATION || '2m',
    },
  },
  thresholds: {
    // S27 collapsed the N+1; we expect this to be as fast as the
    // simpler authenticated reads. Tighter budget than me_dashboard.
    'http_req_duration{expected_response:true}': ['p(95)<500', 'p(99)<1000'],
    'http_req_failed': ['rate<0.01'],
    'checks': ['rate>0.99'],
  },
};

export default function () {
  const res = http.get(`${BASE_URL}/api/v1/me/cohost-mailbox`, {
    headers: { Accept: 'application/json', ...authHeaders('host') },
    tags: { name: 'cohost-mailbox' },
  });
  check(res, {
    'cohost-mailbox: status 200': (r) => r.status === 200,
    'cohost-mailbox: body is paged shape': (r) => {
      try {
        const body = JSON.parse(r.body);
        // Empty mailbox is still valid; we just want a stable shape.
        return body && Array.isArray(body.items);
      } catch {
        return false;
      }
    },
  });
  sleep(0.2);
}
