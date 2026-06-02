#!/usr/bin/env bash
# Mirror the live MinIO bucket to a backup target.
#
# Usage:
#   minio_mirror.sh
#
# Environment:
#   MINIO_LIVE_URL      — live MinIO endpoint (default: http://localhost:9000)
#   MINIO_LIVE_KEY      — access key for the live instance
#   MINIO_LIVE_SECRET   — secret key for the live instance
#   MINIO_LIVE_BUCKET   — source bucket (default: airhost-media)
#
#   MINIO_BACKUP_URL    — backup endpoint (S3-compatible)
#   MINIO_BACKUP_KEY    — access key for the backup target
#   MINIO_BACKUP_SECRET — secret key for the backup target
#   MINIO_BACKUP_BUCKET — destination bucket (default: airhost-media-backup)
#
# Requires the `mc` binary on PATH. The script configures aliases for
# both endpoints (idempotent — re-setting an alias overwrites it) and
# runs `mc mirror --overwrite --remove` so the backup bucket exactly
# tracks the live one.

set -euo pipefail

: "${MINIO_LIVE_URL:?required}"
: "${MINIO_LIVE_KEY:?required}"
: "${MINIO_LIVE_SECRET:?required}"
LIVE_BUCKET="${MINIO_LIVE_BUCKET:-airhost-media}"

: "${MINIO_BACKUP_URL:?required}"
: "${MINIO_BACKUP_KEY:?required}"
: "${MINIO_BACKUP_SECRET:?required}"
BACKUP_BUCKET="${MINIO_BACKUP_BUCKET:-airhost-media-backup}"

# Configure aliases. --api S3v4 keeps signature compatibility broad.
mc alias set airhost-live "$MINIO_LIVE_URL" "$MINIO_LIVE_KEY" "$MINIO_LIVE_SECRET" --api S3v4 >/dev/null
mc alias set airhost-backup "$MINIO_BACKUP_URL" "$MINIO_BACKUP_KEY" "$MINIO_BACKUP_SECRET" --api S3v4 >/dev/null

# Create the destination bucket if missing. --ignore-existing is the
# difference between "already there" being silent vs failing the run.
mc mb --ignore-existing "airhost-backup/${BACKUP_BUCKET}"

# Mirror. --overwrite re-pushes objects whose etag changed; --remove
# deletes objects that no longer exist on the source so a wrongly-
# uploaded large file does not linger on the backup forever. The
# log line at the end is what cron grep'd-for monitoring looks for.
mc mirror --overwrite --remove \
    "airhost-live/${LIVE_BUCKET}" \
    "airhost-backup/${BACKUP_BUCKET}"

echo "minio_mirror.sh: mirrored ${LIVE_BUCKET} → ${BACKUP_BUCKET} OK at $(date -u +%Y-%m-%dT%H:%M:%SZ)"
