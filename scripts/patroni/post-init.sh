#!/usr/bin/env bash
set -euo pipefail

POST_INIT_ENV_FILE=/etc/patroni/post-init.env

if [ -f "${POST_INIT_ENV_FILE}" ]; then
  # shellcheck disable=SC1091
  source "${POST_INIT_ENV_FILE}"
fi

: "${POSTGRES_DB:?POSTGRES_DB is required}"
: "${POSTGRES_USER:?POSTGRES_USER is required}"
: "${POSTGRES_PASSWORD:?POSTGRES_PASSWORD is required}"
: "${TEST_POSTGRES_DB:?TEST_POSTGRES_DB is required}"
: "${METRICS_DB_USER:?METRICS_DB_USER is required}"
: "${METRICS_DB_PASSWORD:?METRICS_DB_PASSWORD is required}"
: "${PATRONI_SUPERUSER_USERNAME:=postgres}"
: "${PATRONI_SUPERUSER_PASSWORD:?PATRONI_SUPERUSER_PASSWORD is required}"
: "${PATRONI_REPLICATION_USERNAME:=replicator}"
: "${PATRONI_REPLICATION_PASSWORD:?PATRONI_REPLICATION_PASSWORD is required}"
: "${PATRONI_REWIND_USERNAME:=rewind_user}"
: "${PATRONI_REWIND_PASSWORD:?PATRONI_REWIND_PASSWORD is required}"

export PGPASSWORD="${PATRONI_SUPERUSER_PASSWORD}"

psql -v ON_ERROR_STOP=1 \
  -h /var/run/postgresql \
  -U "${PATRONI_SUPERUSER_USERNAME}" \
  -d postgres \
  -v superuser="${PATRONI_SUPERUSER_USERNAME}" \
  -v superuser_password="${PATRONI_SUPERUSER_PASSWORD}" \
  -v replication_user="${PATRONI_REPLICATION_USERNAME}" \
  -v replication_password="${PATRONI_REPLICATION_PASSWORD}" \
  -v rewind_user="${PATRONI_REWIND_USERNAME}" \
  -v rewind_password="${PATRONI_REWIND_PASSWORD}" \
  -v app_user="${POSTGRES_USER}" \
  -v app_password="${POSTGRES_PASSWORD}" \
  -v app_db="${POSTGRES_DB}" \
  -v test_db="${TEST_POSTGRES_DB}" \
  -v metrics_user="${METRICS_DB_USER}" \
  -v metrics_password="${METRICS_DB_PASSWORD}" <<'SQL'
SELECT format('ALTER ROLE %I WITH LOGIN SUPERUSER PASSWORD %L', :'superuser', :'superuser_password')
\gexec

SELECT format('CREATE ROLE %I WITH REPLICATION LOGIN PASSWORD %L', :'replication_user', :'replication_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'replication_user')
\gexec

SELECT format('ALTER ROLE %I WITH REPLICATION LOGIN PASSWORD %L', :'replication_user', :'replication_password')
\gexec

SELECT format('CREATE ROLE %I WITH SUPERUSER LOGIN PASSWORD %L', :'rewind_user', :'rewind_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'rewind_user')
\gexec

SELECT format('ALTER ROLE %I WITH SUPERUSER LOGIN PASSWORD %L', :'rewind_user', :'rewind_password')
\gexec

SELECT format('CREATE ROLE %I WITH LOGIN CREATEDB PASSWORD %L', :'app_user', :'app_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'app_user')
\gexec

SELECT format('ALTER ROLE %I WITH LOGIN CREATEDB PASSWORD %L', :'app_user', :'app_password')
\gexec

SELECT format('CREATE DATABASE %I OWNER %I', :'app_db', :'app_user')
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = :'app_db')
\gexec

SELECT format('CREATE DATABASE %I OWNER %I', :'test_db', :'app_user')
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = :'test_db')
\gexec

SELECT format('CREATE ROLE %I WITH LOGIN PASSWORD %L', :'metrics_user', :'metrics_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'metrics_user')
\gexec

SELECT format('ALTER ROLE %I WITH LOGIN PASSWORD %L', :'metrics_user', :'metrics_password')
\gexec

GRANT CONNECT ON DATABASE postgres TO :"metrics_user";
GRANT CONNECT ON DATABASE :"app_db" TO :"metrics_user";
GRANT pg_monitor TO :"metrics_user";
SQL

echo "Patroni post-init completed: users and databases are ready"
