#!/usr/bin/env bash
set -e

current_migration="$1"

if [ -z "$current_migration" ]; then
  echo "usage: ci-down-one.sh <migration_file>"
  exit 1
fi

export GOOSE_DRIVER=postgres
export GOOSE_DBSTRING="$TEST_GOOSE_DBSTRING_DOCKER"
export GOOSE_MIGRATION_DIR=/app/migrations

echo "rolling back one migration on test db..."
goose down