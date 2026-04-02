#!/bin/sh
set -e

echo "Ensuring metrics user exists"

PGADMIN_DB="${PGADMIN_DB:-postgres}"

psql -v ON_ERROR_STOP=1 \
  --username "${POSTGRES_USER}" \
  --dbname "${PGADMIN_DB}" <<EOF
DO \$\$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_roles
    WHERE rolname = '${METRICS_DB_USER}'
  ) THEN
    EXECUTE format(
      'CREATE USER %I WITH PASSWORD %L',
      '${METRICS_DB_USER}',
      '${METRICS_DB_PASSWORD}'
    );
  END IF;
END
\$\$;

GRANT CONNECT ON DATABASE postgres TO ${METRICS_DB_USER};
GRANT CONNECT ON DATABASE ${POSTGRES_DB} TO ${METRICS_DB_USER};
GRANT pg_monitor TO ${METRICS_DB_USER};
EOF

echo "Metrics user is ready"