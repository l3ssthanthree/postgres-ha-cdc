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

object_name="${1:-latest}"

: "${MINIO_ACCESS_KEY:=${MINIO_RESTORE_ACCESS_KEY:-}}"
: "${MINIO_SECRET_KEY:=${MINIO_RESTORE_SECRET_KEY:-}}"
: "${POSTGRES_HOST:=db}"
: "${POSTGRES_PORT:=5432}"
: "${MINIO_ENDPOINT:=http://minio:9000}"

require_env POSTGRES_DB
require_env POSTGRES_USER
require_env POSTGRES_PASSWORD
require_env BUCKET_BACKUP_NAME
require_env MINIO_ACCESS_KEY
require_env MINIO_SECRET_KEY

backup_file=""

cleanup() {
  if [ -n "$backup_file" ] && [ -f "$backup_file" ]; then
    rm -f "$backup_file"
  fi
}

trap cleanup EXIT INT TERM

export PGPASSWORD="$POSTGRES_PASSWORD"

until pg_isready -h "$POSTGRES_HOST" -p "$POSTGRES_PORT" -U "$POSTGRES_USER" -d "$POSTGRES_DB" >/dev/null 2>&1; do
  sleep 2
done

until mc alias set restore "$MINIO_ENDPOINT" "$MINIO_ACCESS_KEY" "$MINIO_SECRET_KEY" >/dev/null 2>&1; do
  sleep 2
done

if [ "$object_name" = "latest" ]; then
  object_name=$(
    mc ls --recursive "restore/${BUCKET_BACKUP_NAME}" \
      | awk '{print $NF}' \
      | grep "^${POSTGRES_DB}_[0-9]\\{8\\}T[0-9]\\{6\\}Z\\.dump$" \
      | sort \
      | tail -n 1
  )
fi

if [ -z "$object_name" ]; then
  echo "no backup objects found in bucket ${BUCKET_BACKUP_NAME}" >&2
  exit 1
fi

backup_file="/tmp/${object_name}"

echo "downloading backup ${object_name}"
mc cp "restore/${BUCKET_BACKUP_NAME}/${object_name}" "$backup_file" >/dev/null

echo "restoring backup ${object_name}"
pg_restore \
  -h "$POSTGRES_HOST" \
  -p "$POSTGRES_PORT" \
  -U "$POSTGRES_USER" \
  -d "$POSTGRES_DB" \
  --clean \
  --if-exists \
  --no-owner \
  --no-privileges \
  "$backup_file"

echo "restore completed from ${object_name}"
