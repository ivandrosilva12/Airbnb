# Backup & disaster recovery

The AirHost stack has four stateful components. This runbook
catalogues each one, names the targets (RPO / RTO), and lists the
exact procedure for backup, restore, and the routine drill that
proves the backups actually work.

## Stateful inventory

| Component | What lives there | Loss impact |
| --- | --- | --- |
| **PostgreSQL** | Every domain aggregate (bookings, payments, disputes, audit trail, etc.) | Total business loss — every record. |
| **MinIO** | Listing photos, message attachments, payment receipts | User-uploaded media; receipts can be re-rendered from the DB but media is irrecoverable. |
| **Keycloak realm** | Users, roles, password hashes, OAuth client configuration | Every authenticated session breaks; users can re-register, but admin/host role mappings are lost. |
| **Grafana dashboards** | Observability dashboards (per-team panels) | Cosmetic — alerts and metrics keep firing; rebuild ~1 hour from `infra/grafana/dashboards/*.json`. |

## Targets

| Component | RPO (data we accept losing) | RTO (time to back online) |
| --- | --- | --- |
| PostgreSQL | **5 minutes** (WAL streaming) or **24 hours** (snapshots only) | **30 minutes** from a snapshot, **2 hours** for a fresh node from cold backup |
| MinIO | **24 hours** (nightly mirror) | **4 hours** (S3 sync from the mirror bucket) |
| Keycloak realm | **24 hours** (nightly realm export) | **15 minutes** (re-import realm-export.json) |
| Grafana | **on every change** (config in git) | **5 minutes** (re-mount provisioning volume) |

Choose the tier per deployment:

- **MVP / single-host**: snapshot-only Postgres, manual nightly
  `mc mirror`, weekly realm export. Cheap; RPO 24h.
- **Production**: WAL streaming Postgres to a second host or
  managed service, MinIO bucket replication (or S3 cross-region),
  Keycloak realm exported nightly to the same backup target.
  RPO 5min for transactions.

Anything tighter (RPO < 1min) requires synchronous replication
and is a separate slice — out of scope for this runbook.

## Backup procedure

### 1. PostgreSQL — pg_dump snapshot (baseline)

Run by `scripts/backup/pg_dump.sh`:

```bash
# Example cron line for the host running docker-compose:
0 2 * * * /opt/airhost/scripts/backup/pg_dump.sh /backups/postgres
```

The script:

1. Issues `pg_dump --format=custom --compress=9` against the
   running container.
2. Streams the output through `gzip --rsyncable` so incremental
   uploads (e.g. to S3) deduplicate well.
3. Names the file `airhost-YYYY-MM-DD-HHMMSS.dump.gz`.
4. Prunes anything in the target older than 30 days.

Exit status: non-zero if any step failed (the cron MUST
forward stderr to the on-call channel).

### 2. PostgreSQL — WAL streaming (production)

Out-of-the-box options:

- **Self-managed**: configure `archive_command` to push WAL
  segments to an S3 bucket; pair with weekly base backups via
  `pg_basebackup`. Document the bucket path in the deployment
  config.
- **Managed (recommended)**: switch to a managed Postgres
  (RDS / Cloud SQL / Crunchy Bridge) with point-in-time
  restore enabled. The script above is then the offsite
  belt-and-braces backup.

### 3. MinIO — bucket mirror

`scripts/backup/minio_mirror.sh` runs `mc mirror --overwrite
--remove` from the live bucket to a backup bucket (or another
provider). Schedule nightly:

```bash
0 3 * * * /opt/airhost/scripts/backup/minio_mirror.sh
```

For production cross-region durability, configure server-side
replication on the MinIO bucket so writes propagate in real
time. The cron above is then the periodic consistency check.

### 4. Keycloak realm export

`scripts/backup/keycloak_export.sh` shells into the Keycloak
container and runs `kc.sh export` against the `airhost` realm,
writing to the same backup target. The exported file is the
same shape as `infra/keycloak/realm-export.json` so a fresh
boot can re-import it.

Schedule daily.

### 5. Grafana dashboards

Dashboards live in `infra/grafana/dashboards/*.json` and are
provisioned at boot. There is no extra backup step — the
provisioning files ARE the backup. Any dashboard edit done
through the UI must be saved back to the repo via "Settings →
Save JSON" and committed.

## Restore procedure

### PostgreSQL — full restore from pg_dump

1. **Provision** a fresh Postgres instance (same major version;
   currently 16).
2. **Run migrations** to create the schema:
   `migrate -database $DSN -path internal/infrastructure/persistence/postgres/migrations up`.
3. **Restore data**:
   `gunzip -c backup.dump.gz | pg_restore --clean --if-exists -d $DSN`.
4. **Sanity check** — count rows in `users`, `bookings`, `payments`
   against the latest dashboard snapshot.
5. **Cut over** — update API env to the new DSN and restart.

### MinIO — restore from mirror

1. Run `mc mirror --overwrite --remove BACKUP/airhost-media LIVE/airhost-media`.
2. The API does not need a restart — presigned URLs the API
   issued before the outage continue to resolve once MinIO is
   reachable again at the same hostname.

### Keycloak — realm re-import

1. Stop the Keycloak container.
2. Drop the export file into
   `infra/keycloak/realm-export.json`.
3. Start with `--import-realm` (the compose already does this).
4. Existing JWTs whose `iss` matches the realm continue to
   validate (the signing key is part of the export); guest
   sessions, however, are bound to the prior Keycloak instance's
   session table and will require re-login.

### Grafana

1. Re-mount the provisioning volume.
2. Restart Grafana — dashboards re-load from JSON.

## The drill (every 90 days)

Schedule a calendar reminder to run the drill quarterly. The
script `scripts/backup/drill.sh` automates it:

1. Spins up a parallel stack with `docker compose -p drill -f
   docker-compose.yml up -d` (separate project name → isolated
   volumes).
2. Pipes the most recent Postgres dump into the drill Postgres.
3. Mirrors the most recent MinIO backup into the drill MinIO.
4. Imports the most recent Keycloak realm export.
5. Smoke-tests via `scripts/e2e_live.py --base-url
   http://localhost:8082` (drill stack maps the API to a
   different port).
6. On success, tears the drill stack down and writes a one-line
   entry to `docs/runbooks/drill_log.md`.
7. On failure, leaves the stack up for inspection and pages the
   on-call.

A successful drill is the only proof the backups actually work.
Treat the run as a hard quarterly commitment — anything else
is "we hope it works".

## Out of scope (acknowledged)

- **Continuous PITR** (point-in-time recovery sub-minute) —
  needs WAL streaming infra; deferred to a managed Postgres
  migration.
- **Cross-region active-active** — needs application changes
  (every aggregate would need a conflict-resolution strategy).
- **Encrypted-at-rest backups** — assumes the backup target
  already enforces it (S3 SSE, etc.). Document the target's
  encryption posture in the deployment runbook, not here.
- **Automated restore on alert** — the drill runs the restore
  manually. A "restore on RPO breach" automation is a separate
  slice with its own failure modes (false-positive restores).
