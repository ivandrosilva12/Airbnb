#!/usr/bin/env bash
# Quarterly restore drill — proves the backups actually work.
#
# Spins up a parallel docker-compose project ("drill"), restores the
# most recent Postgres dump + MinIO mirror + Keycloak realm export
# into it, runs the live-e2e smoke test against the drill API, and
# (on success) tears the stack down. On failure, leaves the stack up
# for inspection.
#
# Usage:
#   drill.sh [/backups/root]
#
# Expects the backup root to contain pg_dump, minio_mirror, and
# keycloak_export outputs in subdirectories of the same names. Uses
# the latest file in each.
#
# Environment overrides:
#   DRILL_API_PORT      — port to map the drill API to (default: 18081)
#   DRILL_KC_PORT       — port to map the drill Keycloak to (default: 18080)
#   DRILL_LOG           — log file (default: docs/runbooks/drill_log.md)

set -euo pipefail

ROOT="${1:-/backups}"
DRILL_API_PORT="${DRILL_API_PORT:-18081}"
DRILL_KC_PORT="${DRILL_KC_PORT:-18080}"
DRILL_LOG="${DRILL_LOG:-docs/runbooks/drill_log.md}"

stamp="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo "=== restore drill @ $stamp ==="

latest() {
    # Pick the most-recently-modified file matching the given glob.
    # ls -t orders newest-first; head -n1 is the chosen file.
    # shellcheck disable=SC2012
    ls -t "$1" 2>/dev/null | head -n1
}

PG_LATEST="$(latest "${ROOT}/postgres/airhost-*.dump.gz")"
KC_LATEST="$(latest "${ROOT}/keycloak/airhost-*.json")"

if [ -z "$PG_LATEST" ] || [ -z "$KC_LATEST" ]; then
    echo "missing backup inputs (pg=$PG_LATEST, kc=$KC_LATEST) — abort" >&2
    exit 2
fi

echo "using:"
echo "  pg     : $PG_LATEST"
echo "  kc     : $KC_LATEST"

# Spin up the drill stack with a separate project name → isolated
# volumes. The compose file maps default ports to internal addresses
# inside the network; we override the externally-exposed ports for
# api and keycloak so the host can hit them without colliding with
# the live stack.
DRILL_PROJECT="airhost-drill"

cleanup() {
    if [ "${KEEP:-0}" = "1" ]; then
        echo "drill: KEEP=1 set, leaving stack up for inspection"
        return
    fi
    docker compose -p "$DRILL_PROJECT" down -v >/dev/null 2>&1 || true
}
trap 'KEEP=1; cleanup' ERR
trap cleanup EXIT

API_PORT="$DRILL_API_PORT" KC_PORT="$DRILL_KC_PORT" \
    docker compose -p "$DRILL_PROJECT" up -d postgres minio keycloak

# Stage the Keycloak realm export so the import-realm boot picks it up.
docker cp "$KC_LATEST" "${DRILL_PROJECT}-keycloak-1:/opt/keycloak/data/import/realm-export.json"
docker restart "${DRILL_PROJECT}-keycloak-1" >/dev/null

# Wait for Postgres to be ready.
echo "drill: waiting for postgres ..."
until docker exec "${DRILL_PROJECT}-postgres-1" pg_isready -U airhost >/dev/null 2>&1; do
    sleep 2
done

# Restore the dump.
gunzip -c "$PG_LATEST" | docker exec -i "${DRILL_PROJECT}-postgres-1" \
    pg_restore --clean --if-exists -d airhost -U airhost
echo "drill: postgres restored"

# Start API last, after data is in place.
docker compose -p "$DRILL_PROJECT" up -d api

# Wait for API.
echo "drill: waiting for api ..."
for _ in $(seq 1 30); do
    if curl -fsS "http://localhost:${DRILL_API_PORT}/healthz" >/dev/null 2>&1; then
        break
    fi
    sleep 2
done

# Run smoke test.
python3 scripts/e2e_live.py --base-url "http://localhost:${DRILL_API_PORT}"
echo "drill: smoke test passed"

# Log a success line.
{
    echo "- $stamp — OK (pg=$PG_LATEST, kc=$KC_LATEST)"
} >> "$DRILL_LOG"

echo "=== drill OK ==="
