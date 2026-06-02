#!/usr/bin/env bash
# Postgres custom-format backup with gzip compression and 30-day retention.
#
# Usage:
#   pg_dump.sh /backups/postgres
#
# Environment overrides (all optional):
#   PG_CONTAINER       — docker container name (default: airhost-postgres-1)
#   PG_DB              — database name (default: airhost)
#   PG_USER            — db user (default: airhost)
#   RETENTION_DAYS     — prune anything older than N days (default: 30)
#
# Exit codes: 0 success, non-zero on any step failure. Cron MUST forward
# stderr to the on-call channel so silent breakage is impossible.

set -euo pipefail

TARGET_DIR="${1:?usage: pg_dump.sh /target/dir}"
PG_CONTAINER="${PG_CONTAINER:-airhost-postgres-1}"
PG_DB="${PG_DB:-airhost}"
PG_USER="${PG_USER:-airhost}"
RETENTION_DAYS="${RETENTION_DAYS:-30}"

mkdir -p "$TARGET_DIR"

stamp="$(date -u +%Y-%m-%d-%H%M%S)"
out="${TARGET_DIR}/airhost-${stamp}.dump.gz"

# --format=custom lets pg_restore parallelise; --compress=9 packs it tight
# and the second gzip --rsyncable lets incremental uploads dedup the
# unchanged tail bytes against the previous dump.
docker exec -i "$PG_CONTAINER" \
    pg_dump --format=custom --compress=9 --username="$PG_USER" "$PG_DB" \
  | gzip --rsyncable > "$out"

# Refuse a suspiciously tiny dump — even an empty schema is bigger than
# a few hundred bytes. Catches the "pg_dump produced 0 bytes and exited
# zero because the database was unreachable" failure mode.
size=$(stat -c%s "$out" 2>/dev/null || stat -f%z "$out")
if [ "$size" -lt 1024 ]; then
    echo "pg_dump output suspiciously small: ${size} bytes at $out" >&2
    exit 2
fi

echo "wrote $out ($(numfmt --to=iec --suffix=B "$size" 2>/dev/null || echo "${size}B"))"

# Prune. -mtime +N catches files MORE than N days old. Keep the most
# recent dump regardless so a long quiet period doesn't leave us with
# nothing to restore from.
find "$TARGET_DIR" -name 'airhost-*.dump.gz' -type f -mtime +"$RETENTION_DAYS" \
    ! -newer "$out" -delete
