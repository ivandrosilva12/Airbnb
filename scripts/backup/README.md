# backup scripts

Operational scripts referenced by [`docs/runbooks/BackupAndRecovery.md`](../../docs/runbooks/BackupAndRecovery.md).

| Script | Cadence | What it does |
| --- | --- | --- |
| `pg_dump.sh` | nightly | Custom-format pg_dump of the `airhost` DB to a target dir, 30-day retention |
| `minio_mirror.sh` | nightly | `mc mirror --overwrite --remove` from live MinIO to a backup bucket |
| `keycloak_export.sh` | nightly | `kc.sh export --realm airhost` of users + clients to a JSON file |
| `drill.sh` | quarterly | End-to-end restore drill against a parallel docker-compose stack |

All scripts are `set -euo pipefail` and exit non-zero on any
recoverable failure — cron MUST forward stderr to the on-call channel.

Run from the repo root, e.g.:

```bash
./scripts/backup/pg_dump.sh /backups/postgres
./scripts/backup/keycloak_export.sh /backups/keycloak
./scripts/backup/minio_mirror.sh   # reads endpoint config from env
./scripts/backup/drill.sh /backups # quarterly only
```

The `drill_log.md` (in `docs/runbooks/`) is append-only — every
successful drill writes a one-line entry there. Missing entries
flag a missed quarterly drill at the next audit.
