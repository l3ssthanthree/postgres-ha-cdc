#!/bin/sh
set -eu

: "${BACKUP_STATE_FILE:=/var/lib/backup/state/state.json}"

mkdir -p "$(dirname "$BACKUP_STATE_FILE")"

tmp_file="${BACKUP_STATE_FILE}.tmp"

cat > "$tmp_file" <<EOF
{
  "last_success_timestamp_seconds": $1,
  "last_backup_size_bytes": $2,
  "last_backup_duration_seconds": $3,
  "last_backup_object": "$4"
}
EOF

mv "$tmp_file" "$BACKUP_STATE_FILE"
