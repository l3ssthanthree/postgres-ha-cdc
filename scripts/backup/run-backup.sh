#!/bin/sh
set -eu

require_env() {
  var_name="$1"
  eval "var_value=\${$var_name:-}"

  if [ -z "$var_value" ]; then
    echo "missing required environment variable: $var_name" >&2
    exit 1
  fi
}

require_env POSTGRES_HOST
require_env POSTGRES_PORT
require_env POSTGRES_DB
require_env POSTGRES_USER
require_env POSTGRES_PASSWORD
require_env MINIO_ENDPOINT
require_env MINIO_BACKUP_ACCESS_KEY
require_env MINIO_BACKUP_SECRET_KEY
require_env BUCKET_BACKUP_NAME
require_env BACKUP_RETENTION_COUNT

case "$BACKUP_RETENTION_COUNT" in
  ''|*[!0-9]*)
    echo "BACKUP_RETENTION_COUNT must be a non-negative integer" >&2
    exit 1
    ;;
esac

: "${BACKUP_STATE_FILE:=/var/lib/backup/state/state.json}"
: "${BACKUP_TEMP_DIR:=/tmp/backups}"

lock_dir="/tmp/backup.lock"
backup_file=""

cleanup() {
  if [ -n "$backup_file" ] && [ -f "$backup_file" ]; then
    rm -f "$backup_file"
  fi

  if [ -d "$lock_dir" ]; then
    rmdir "$lock_dir"
  fi
}

if ! mkdir "$lock_dir" 2>/dev/null; then
  echo "backup is already running, skipping overlapping execution"
  exit 0
fi

trap cleanup EXIT INT TERM

mkdir -p "$BACKUP_TEMP_DIR"
export PGPASSWORD="$POSTGRES_PASSWORD"

echo "waiting for postgres at ${POSTGRES_HOST}:${POSTGRES_PORT}"
until pg_isready -h "$POSTGRES_HOST" -p "$POSTGRES_PORT" -U "$POSTGRES_USER" -d "$POSTGRES_DB" >/dev/null 2>&1; do
  sleep 2
done

echo "connecting to MinIO at ${MINIO_ENDPOINT}"
until mc alias set backup "$MINIO_ENDPOINT" "$MINIO_BACKUP_ACCESS_KEY" "$MINIO_BACKUP_SECRET_KEY" >/dev/null 2>&1; do
  sleep 2
done

echo "waiting for bucket ${BUCKET_BACKUP_NAME}"
until mc ls "backup/${BUCKET_BACKUP_NAME}" >/dev/null 2>&1; do
  sleep 2
done

start_ts=$(date +%s)
timestamp=$(date -u +"%Y%m%dT%H%M%SZ")
object_name="${POSTGRES_DB}_${timestamp}.dump"
backup_file="${BACKUP_TEMP_DIR}/${object_name}"

echo "creating backup ${object_name}"
pg_dump \
  -h "$POSTGRES_HOST" \
  -p "$POSTGRES_PORT" \
  -U "$POSTGRES_USER" \
  -d "$POSTGRES_DB" \
  --format=custom \
  --compress=9 \
  --file="$backup_file"

backup_size_bytes=$(wc -c < "$backup_file" | tr -d '[:space:]')

mc cp "$backup_file" "backup/${BUCKET_BACKUP_NAME}/${object_name}" >/dev/null

existing_objects=$(
  mc ls --recursive "backup/${BUCKET_BACKUP_NAME}" \
    | awk '{print $NF}' \
    | grep "^${POSTGRES_DB}_[0-9]\\{8\\}T[0-9]\\{6\\}Z\\.dump$" \
    | sort || true
)

existing_count=$(printf '%s\n' "$existing_objects" | sed '/^$/d' | wc -l | tr -d '[:space:]')

if [ "$existing_count" -gt "$BACKUP_RETENTION_COUNT" ]; then
  remove_count=$((existing_count - BACKUP_RETENTION_COUNT))

  printf '%s\n' "$existing_objects" | head -n "$remove_count" | while IFS= read -r old_object; do
    [ -n "$old_object" ] || continue

    echo "removing old backup ${old_object}"
    mc rm "backup/${BUCKET_BACKUP_NAME}/${old_object}" >/dev/null
  done
fi

duration=$(( $(date +%s) - start_ts ))

/opt/backup/write-state.sh "$start_ts" "$backup_size_bytes" "$duration" "$object_name"

echo "backup ${object_name} uploaded successfully (${backup_size_bytes} bytes, ${duration}s)"
