// Constant 30 VUs hitting the property-detail endpoint. The second-most-
// hit path in the public read mix: every search click leads here, and
// every shared listing URL lands here cold. A regression on the detail
// page tail (e.g. an N+1 over photos or reviews) is the most user-visible
// of the public surfaces.
//
// Strategy: cache a list of property ids at scenario setup, then VUs
// rotate through them. This avoids the test pounding the same row over
// and over (which would inflate the buffer pool hit rate and hide a
// real cold-cache regression).
import { sleep } from 'k6';
import { get, checkOK, pickRandomID, VUS, DURATION } from './lib.js';

export const options = {
  scenarios: {
    detail_steady: {
      executor: 'constant-vus',
      vus: VUS || 30,
      duration: DURATION || '3m',
    },
  },
  thresholds: {
    'http_req_duration{expected_response:true}': ['p(95)<500', 'p(99)<1000'],
    'http_req_failed': ['rate<0.01'],
    'checks': ['rate>0.99'],
  },
};

// setup runs once before any VUs start. Cache a handful of property ids
// so VUs aren't all hitting the first id in the table (cold-cache realism).
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
  const res = get(`/api/v1/properties/${id}`);
  checkOK(res, 'detail', {
    'detail: body has id': (r) => {
      try {
        return JSON.parse(r.body).id === id;
      } catch {
        return false;
      }
    },
  });
  sleep(0.2);
}
