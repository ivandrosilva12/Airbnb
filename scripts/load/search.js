// Sustained ramp to 50 VUs against the public search endpoint. The most-
// hit path on the marketing site and the mobile cold start; also the one
// most likely to expose a slow query plan when the property table grows.
//
// Workload: a mix of empty searches (the default landing page) and
// city-filtered ones (the most common refinement). Pagination is exercised
// by rotating through the first three pages so the LIMIT/OFFSET plan is
// hit, not just the cached first page.
import { sleep } from 'k6';
import { get, checkOK, VUS, DURATION } from './lib.js';

export const options = {
  scenarios: {
    search_ramp: {
      executor: 'ramping-vus',
      startVUs: 1,
      stages: [
        { duration: '30s', target: 10 },
        { duration: '1m',  target: VUS || 50 },
        { duration: DURATION || '2m', target: VUS || 50 },
        { duration: '20s', target: 0 },
      ],
      gracefulRampDown: '10s',
    },
  },
  thresholds: {
    // Budget — see scripts/load/README.md for rationale.
    'http_req_duration{expected_response:true}': ['p(95)<500', 'p(99)<1000'],
    'http_req_failed': ['rate<0.01'],
    'checks': ['rate>0.99'],
  },
};

// Mixed query mix: ~60% landing-page (no filter), ~30% city-only,
// ~10% city+price-band (the next-most-refined query the UI offers).
const CITIES = ['Lisbon', 'Porto', 'Faro', 'Coimbra', 'Braga'];

function pickQuery() {
  const r = Math.random();
  const page = Math.floor(Math.random() * 3) + 1; // 1..3
  if (r < 0.6) return `?page=${page}`;
  const city = CITIES[Math.floor(Math.random() * CITIES.length)];
  if (r < 0.9) return `?city=${city}&page=${page}`;
  return `?city=${city}&minPrice=5000&maxPrice=30000&page=${page}`;
}

export default function () {
  const q = pickQuery();
  const res = get(`/api/v1/properties${q}`);
  checkOK(res, 'search', {
    'search: body has items array': (r) => {
      try {
        const body = JSON.parse(r.body);
        return Array.isArray(body.items);
      } catch {
        return false;
      }
    },
  });
  // Short think-time so a single VU isn't a tight loop. Real browsers spend
  // far more than 100ms on a search results page, but the goal here is to
  // exercise the server, not simulate a user.
  sleep(0.1);
}
