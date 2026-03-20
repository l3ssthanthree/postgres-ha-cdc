#!/usr/bin/env bash
set -e

if [ "$APP_ENV" = "prod" ]; then
  echo "prod environment, skipping seeds"
  exit 0
fi

SEED_COUNT="${SEED_COUNT:-10}"

echo "running go seeder with SEED_COUNT=${SEED_COUNT}..."
go run ./cmd/seed