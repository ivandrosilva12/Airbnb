# AirHost load tests (k6)

Performance budget for the public read endpoints — the hot paths that
the marketing site, the unauthenticated search experience, and the
mobile-app cold-start all hit. Each script encodes a single workload
shape with explicit `thresholds`: if a run violates one the k6 exit
code is non-zero and CI fails the build.

## What's covered today

**Public reads:**

| Script | Endpoint | Shape | Why it matters |
| --- | --- | --- | --- |
| `search.js` | `GET /api/v1/properties` | sustained ramp to 50 VUs | top-of-funnel discovery; query plan stress |
| `property_detail.js` | `GET /api/v1/properties/:id` | constant 30 VUs | second-most-hit endpoint after search |
| `availability.js` | `GET /api/v1/properties/:id/availability` | constant 20 VUs | exercises the date-overlap query (N+1 prone) |
| `mixed_browse.js` | 70% search + 30% detail | ramp to 60 VUs | realistic browsing mix; integration smoke |

**Authenticated reads (S40):**

| Script | Endpoint(s) | Shape | Why it matters |
| --- | --- | --- | --- |
| `me_dashboard.js` | `/me`, `/bookings/me`, `/me/disputes`, `/notifications` | constant 20 VUs, weighted mix | cold-load dashboard pattern; **regression gate for the S27 dispute N+1 fix** |
| `cohost_mailbox.js` | `/me/cohost-mailbox` | constant 15 VUs | **regression gate for the S27 cohost permission N+1 fix** |

`auth.js` is the shared helper — does Keycloak OIDC direct-grant for the
seeded `guest@airhost.dev` / `host@airhost.dev` / `admin@airhost.dev`
users (passwords in the realm export). Tokens are cached per VU. The
dev realm's 5-minute token TTL covers the default 2-minute scenario
durations; bump the scenario past ~4 min and you'll need refresh.

Write paths (POST /bookings with money-moving side effects, POST
/disputes, etc) are still out of scope — they pollute the dev DB and
need cleanup tooling. Add them as a follow-up if you want write-tail
budgets in CI.

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

# 5. Authenticated scripts also accept KEYCLOAK_URL / REALM / CLIENT
#    (defaults match the dev docker-compose)
k6 run scripts/load/me_dashboard.js
KEYCLOAK_URL=https://staging-id.example k6 run scripts/load/cohost_mailbox.js
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
