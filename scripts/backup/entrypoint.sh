#!/bin/sh
set -eu

: "${BACKUP_INTERVAL:?BACKUP_INTERVAL is required}"
: "${BACKUP_STATE_FILE:=/var/lib/backup/state/state.json}"

mkdir -p "$(dirname "$BACKUP_STATE_FILE")"

if [ ! -f "$BACKUP_STATE_FILE" ]; then
  cat > "$BACKUP_STATE_FILE" <<'EOF'
{
  "last_success_timestamp_seconds": 0,
  "last_backup_size_bytes": 0,
  "last_backup_duration_seconds": 0,
  "last_backup_object": ""
}
EOF
fi

cat > /etc/crontabs/root <<EOF
SHELL=/bin/sh
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
${BACKUP_INTERVAL} /opt/backup/run-backup.sh >> /proc/1/fd/1 2>> /proc/1/fd/2
EOF

echo "starting backup exporter on :9091"
backup-exporter -listen-address :9091 -state-file "$BACKUP_STATE_FILE" &

echo "configured backup schedule: $BACKUP_INTERVAL"
exec crond -f -l 2
