// Constant 20 VUs hitting the authenticated "my dashboard" mix every
// guest sees on app load: /me + /me/bookings + /me/disputes +
// /notifications. The S27 N+1 fix landed on /me/disputes specifically,
// so this script is the canonical regression gate for that path — a
// jump in p95 here likely means the dispute service grew another loop.
//
// Read-only. No state mutation; safe to run as often as you like.
import { sleep } from 'k6';
import http from 'k6/http';
import { check } from 'k6';
import { BASE_URL, VUS, DURATION } from './lib.js';
import { authHeaders } from './auth.js';

export const options = {
  scenarios: {
    me_dashboard: {
      executor: 'constant-vus',
      vus: VUS || 20,
      duration: DURATION || '2m',
    },
  },
  thresholds: {
    // /me/disputes does multiple internal lookups even after S27; budget
    // is a touch looser than the pure public reads.
    'http_req_duration{expected_response:true}': ['p(95)<600', 'p(99)<1200'],
    'http_req_failed': ['rate<0.01'],
    'checks': ['rate>0.99'],
  },
};

// Hit pattern weights — matches what the web app actually fetches on a
// cold dashboard load (some reads happen more than others as the user
// expands sections).
const ROUTES = [
  { path: '/api/v1/me',           weight: 1, label: 'me'           },
  { path: '/api/v1/bookings/me',  weight: 3, label: 'my-bookings'  },
  { path: '/api/v1/me/disputes',  weight: 2, label: 'my-disputes'  },
  { path: '/api/v1/notifications', weight: 3, label: 'notifications'},
];

function pickRoute() {
  const total = ROUTES.reduce((s, r) => s + r.weight, 0);
  let n = Math.random() * total;
  for (const r of ROUTES) {
    if ((n -= r.weight) <= 0) return r;
  }
  return ROUTES[0];
}

export default function () {
  const route = pickRoute();
  const res = http.get(`${BASE_URL}${route.path}`, {
    headers: { Accept: 'application/json', ...authHeaders('guest') },
    tags: { name: route.label }, // groups metrics per logical route in the summary
  });
  check(res, {
    [`${route.label}: status 200`]: (r) => r.status === 200,
    [`${route.label}: body non-empty`]: (r) => r.body && r.body.length > 0,
  });
  sleep(0.15);
}
