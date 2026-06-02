// Constant 20 VUs against the availability endpoint. Historically the
// slowest public read: each call runs the date-overlap query across the
// booking table, which is more expensive than a single-row property
// fetch. This script's purpose is to keep that tail honest — a +30% p95
// jump versus the README baseline is a regression to investigate.
//
// Looser threshold than search/detail (p95 < 800ms vs 500ms) to reflect
// the heavier query plan; the goal is still "no perceptible lag" but the
// budget acknowledges the work.
import { sleep } from 'k6';
import { get, checkOK, pickRandomID, VUS, DURATION } from './lib.js';

export const options = {
  scenarios: {
    availability_steady: {
      executor: 'constant-vus',
      vus: VUS || 20,
      duration: DURATION || '2m',
    },
  },
  thresholds: {
    // Looser p95 budget than the other public reads — see file header.
    'http_req_duration{expected_response:true}': ['p(95)<800', 'p(99)<1500'],
    'http_req_failed': ['rate<0.01'],
    'checks': ['rate>0.99'],
  },
};

export function setup() {
  const res = get('/api/v1/properties?page=1');
  if (res.status !== 200) {
    throw new Error(`setup: search returned ${res.status}; is the API up + seeded?`);
  }
  const body = JSON.parse(res.body);
  const ids = (body.items || []).map((p) => p.id);
  if (ids.length === 0) {
    throw new Error('setup: no properties returned; run scripts/seed.py first');
  }
  return { ids };
}

export default function (data) {
  const id = pickRandomID(data.ids.map((x) => ({ id: x })));
  const res = get(`/api/v1/properties/${id}/availability`);
  checkOK(res, 'availability', {
    'availability: body has booked array': (r) => {
      try {
        const body = JSON.parse(r.body);
        return Array.isArray(body.booked);
      } catch {
        return false;
      }
    },
  });
  sleep(0.3);
}
