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

: "${MINIO_ENDPOINT:=http://minio:9000}"

require_env MINIO_ROOT_USER
require_env MINIO_ROOT_PASSWORD
require_env BUCKET_BACKUP_NAME
require_env MINIO_BACKUP_ACCESS_KEY
require_env MINIO_BACKUP_SECRET_KEY
require_env MINIO_RESTORE_ACCESS_KEY
require_env MINIO_RESTORE_SECRET_KEY

echo "waiting for MinIO at ${MINIO_ENDPOINT}"
until mc alias set local "$MINIO_ENDPOINT" "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null 2>&1; do
  sleep 2
done

echo "ensuring bucket ${BUCKET_BACKUP_NAME} exists"
mc mb --ignore-existing "local/${BUCKET_BACKUP_NAME}" >/dev/null
mc anonymous set none "local/${BUCKET_BACKUP_NAME}" >/dev/null 2>&1 || true

cat > /tmp/backup-writer-policy.json <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetBucketLocation",
        "s3:ListBucket",
        "s3:ListBucketMultipartUploads"
      ],
      "Resource": [
        "arn:aws:s3:::${BUCKET_BACKUP_NAME}"
      ]
    },
    {
      "Effect": "Allow",
      "Action": [
        "s3:AbortMultipartUpload",
        "s3:DeleteObject",
        "s3:ListMultipartUploadParts",
        "s3:PutObject"
      ],
      "Resource": [
        "arn:aws:s3:::${BUCKET_BACKUP_NAME}/*"
      ]
    }
  ]
}
EOF

cat > /tmp/backup-reader-policy.json <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetBucketLocation",
        "s3:ListBucket"
      ],
      "Resource": [
        "arn:aws:s3:::${BUCKET_BACKUP_NAME}"
      ]
    },
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetObject"
      ],
      "Resource": [
        "arn:aws:s3:::${BUCKET_BACKUP_NAME}/*"
      ]
    }
  ]
}
EOF

mc admin policy create local backup-writer /tmp/backup-writer-policy.json >/dev/null 2>&1 || true
mc admin policy create local backup-reader /tmp/backup-reader-policy.json >/dev/null 2>&1 || true

mc admin user info local "$MINIO_BACKUP_ACCESS_KEY" >/dev/null 2>&1 || mc admin user add local "$MINIO_BACKUP_ACCESS_KEY" "$MINIO_BACKUP_SECRET_KEY" >/dev/null
mc admin user info local "$MINIO_RESTORE_ACCESS_KEY" >/dev/null 2>&1 || mc admin user add local "$MINIO_RESTORE_ACCESS_KEY" "$MINIO_RESTORE_SECRET_KEY" >/dev/null

mc admin policy attach local backup-writer --user "$MINIO_BACKUP_ACCESS_KEY" >/dev/null 2>&1 || true
mc admin policy attach local backup-reader --user "$MINIO_RESTORE_ACCESS_KEY" >/dev/null 2>&1 || true

echo "MinIO bucket, users and policies are ready"
