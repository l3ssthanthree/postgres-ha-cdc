#!/usr/bin/env bash
set -e

echo "waiting for db..."
until pg_isready -h db -p 5432 -U "$POSTGRES_USER"; do
  sleep 1
done

export GOOSE_DRIVER=postgres
export GOOSE_DBSTRING="$GOOSE_DBSTRING_DOCKER"
export GOOSE_MIGRATION_DIR=/app/migrations

echo "validating migrations..."
goose validate

echo "running seqwall staircase on test db..."
/go/bin/seqwall staircase \
  --postgres-url "$TEST_GOOSE_DBSTRING_DOCKER" \
  --migrations-path /app/migrations \
  --upgrade "/app/scripts/ci-up-one.sh {current_migration}" \
  --downgrade "/app/scripts/ci-down-one.sh {current_migration}"

echo "running migrations on main db..."
if [ -n "$MIGRATION_VERSION" ]; then
  goose up-to "$MIGRATION_VERSION"
else
  goose up
fi

if [ "$APP_ENV" != "prod" ]; then
  echo "running seeds..."
  /app/scripts/seed.sh
else
  echo "APP_ENV=prod, skipping seeds"
fi

echo "migrations completed"