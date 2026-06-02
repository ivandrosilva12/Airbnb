# AirHost load tests (k6)

Performance budget for the public read endpoints — the hot paths that
the marketing site, the unauthenticated search experience, and the
mobile-app cold-start all hit. Each script encodes a single workload
shape with explicit `thresholds`: if a run violates one the k6 exit
code is non-zero and CI fails the build.

## What's covered today

| Script | Endpoint | Shape | Why it matters |
| --- | --- | --- | --- |
| `search.js` | `GET /api/v1/properties` | sustained ramp to 50 VUs | top-of-funnel discovery; query plan stress |
| `property_detail.js` | `GET /api/v1/properties/:id` | constant 30 VUs | second-most-hit endpoint after search |
| `availability.js` | `GET /api/v1/properties/:id/availability` | constant 20 VUs | exercises the date-overlap query (N+1 prone) |
| `mixed_browse.js` | 70% search + 30% detail | ramp to 60 VUs | realistic browsing mix; integration smoke |

Authenticated paths (`POST /api/v1/bookings`, `/me/disputes`, …) are
deliberately out of scope for this slice — they need a Keycloak token
flow that complicates the scaffold. A follow-up adds an auth helper +
scripts for the write paths.

## How to run

```bash
# 1. Start the stack
docker compose up -d
# 2. (optional) seed some properties so search has rows to return
python scripts/seed.py

# 3. Run one script
k6 run scripts/load/search.js

# 4. Override the base URL or target VU count via env vars
BASE_URL=https://staging.airhost.example k6 run scripts/load/search.js
VUS=100 DURATION=5m k6 run scripts/load/search.js
```

If `k6` isn't installed: <https://k6.io/docs/get-started/installation/>
or use the official Docker image:

```bash
docker run --rm -i --network host -v "$(pwd)/scripts/load:/scripts" \
  grafana/k6 run /scripts/search.js
```

## Thresholds

Each script declares thresholds that document the budget. The current
targets are conservative starting points based on the synthetic
workload and a single-instance Postgres; tighten them once we have a
real baseline from staging.

| Metric | Budget | Rationale |
| --- | --- | --- |
| `http_req_duration{expected_response:true} p(95)` | < 500 ms | a browsing user should not perceive lag |
| `http_req_duration{expected_response:true} p(99)` | < 1 s | tail latency for the slowest 1% |
| `http_req_failed` | < 1% | network blips, NOT 5xx; tighter on writes later |
| `checks` | > 99% | every response must hit a 200 OK and pass schema spot-checks |

The `availability.js` script raises the p(95) budget to 800 ms — the
date-overlap query touches more rows and has historically been the
slowest of the public reads.

## Baselines

Run on a fresh dev stack (single-node Postgres in Docker, search data
seeded by `scripts/seed.py`) to populate the table below:

| Date | Script | p50 | p95 | p99 | RPS | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| _TBD_ | `search.js` | | | | | first run |
| _TBD_ | `property_detail.js` | | | | | first run |
| _TBD_ | `availability.js` | | | | | first run |

Once a row exists, regressions become visible: a +30% p95 jump
between two PRs is a flag.
