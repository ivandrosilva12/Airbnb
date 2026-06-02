// Realistic browsing mix: 70% search, 30% property-detail click-through.
// Each VU is a "user" walking the funnel — search lands them on a list,
// they click one result, then go back to search. This is the workload
// shape most representative of what production traffic actually looks
// like, so it's the script CI should treat as the canonical perf gate
// (the per-endpoint scripts above are diagnostic — they isolate one
// regression source at a time).
//
// Ramp to 60 VUs is the synthetic equivalent of a moderate Lisbon-weekend
// surge against a single-instance backend; pass criteria match the
// stricter of the two per-endpoint scripts (search's 500ms p95).
import { sleep } from 'k6';
import { get, checkOK, pickRandomID, VUS, DURATION } from './lib.js';

export const options = {
  scenarios: {
    browse_ramp: {
      executor: 'ramping-vus',
      startVUs: 1,
      stages: [
        { duration: '30s', target: 20 },
        { duration: '1m',  target: VUS || 60 },
        { duration: DURATION || '3m', target: VUS || 60 },
        { duration: '20s', target: 0 },
      ],
      gracefulRampDown: '10s',
    },
  },
  thresholds: {
    'http_req_duration{expected_response:true}': ['p(95)<500', 'p(99)<1200'],
    'http_req_failed': ['rate<0.01'],
    'checks': ['rate>0.99'],
  },
};

const CITIES = ['Lisbon', 'Porto', 'Faro', 'Coimbra', 'Braga'];

function searchQuery() {
  const r = Math.random();
  const page = Math.floor(Math.random() * 3) + 1;
  if (r < 0.6) return `?page=${page}`;
  const city = CITIES[Math.floor(Math.random() * CITIES.length)];
  return `?city=${city}&page=${page}`;
}

export default function () {
  // 70% of iterations are a search; 30% are a follow-through detail click
  // using a freshly-searched id (so the test exercises both the search
  // result rendering and the cold detail fetch).
  if (Math.random() < 0.7) {
    const res = get(`/api/v1/properties${searchQuery()}`);
    checkOK(res, 'search');
    sleep(0.1);
    return;
  }
  // detail click: search first to pick a real id (some queries return [])
  const searchRes = get(`/api/v1/properties${searchQuery()}`);
  checkOK(searchRes, 'search-before-detail');
  if (searchRes.status !== 200) return;
  const body = JSON.parse(searchRes.body);
  const id = pickRandomID(body.items);
  if (!id) return;
  const detailRes = get(`/api/v1/properties/${id}`);
  checkOK(detailRes, 'detail');
  sleep(0.2);
}
