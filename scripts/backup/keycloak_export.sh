#!/usr/bin/env bash
# Export the Keycloak `airhost` realm to a JSON file the boot import
# can re-consume.
#
# Usage:
#   keycloak_export.sh /backups/keycloak
#
# Environment overrides:
#   KC_CONTAINER — docker container name (default: airhost-keycloak-1)
#   KC_REALM     — realm to export (default: airhost)
#
# The export runs in-process so it works against a live Keycloak (no
# stop-and-export required). Same shape as the realm-export.json the
# compose mounts at boot — so a restore is "drop file in
# infra/keycloak/realm-export.json, restart with --import-realm".

set -euo pipefail

TARGET_DIR="${1:?usage: keycloak_export.sh /target/dir}"
KC_CONTAINER="${KC_CONTAINER:-airhost-keycloak-1}"
KC_REALM="${KC_REALM:-airhost}"

mkdir -p "$TARGET_DIR"

stamp="$(date -u +%Y-%m-%d)"
out_in_container="/tmp/airhost-${KC_REALM}-${stamp}.json"
out_on_host="${TARGET_DIR}/airhost-${KC_REALM}-${stamp}.json"

# Export in single-file mode with users included.
docker exec "$KC_CONTAINER" \
    /opt/keycloak/bin/kc.sh export \
        --file "$out_in_container" \
        --realm "$KC_REALM" \
        --users realm_file

# Pull out of the container.
docker cp "$KC_CONTAINER:$out_in_container" "$out_on_host"
docker exec "$KC_CONTAINER" rm -f "$out_in_container"

# Refuse a zero-byte / tiny export — same defensive check as pg_dump.sh.
size=$(stat -c%s "$out_on_host" 2>/dev/null || stat -f%z "$out_on_host")
if [ "$size" -lt 1024 ]; then
    echo "keycloak export suspiciously small: ${size} bytes at $out_on_host" >&2
    exit 2
fi

echo "wrote $out_on_host ($(numfmt --to=iec --suffix=B "$size" 2>/dev/null || echo "${size}B"))"
