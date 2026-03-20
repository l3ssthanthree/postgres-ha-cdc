#!/usr/bin/env bash
set -e

current_migration="$1"

if [ -z "$current_migration" ]; then
  echo "usage: ci-up-one.sh <migration-file>"
  exit 1
fi

version=$(basename "$current_migration" | cut -d'_' -f1)

export GOOSE_DRIVER=postgres
export GOOSE_DBSTRING="$TEST_GOOSE_DBSTRING_DOCKER"
export GOOSE_MIGRATION_DIR=/app/migrations

echo "applying migrations up to ${version} on test db..."
goose up-to "$version"