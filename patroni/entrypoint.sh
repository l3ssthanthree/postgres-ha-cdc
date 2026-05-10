#!/usr/bin/env bash
set -euo pipefail

: "${PATRONI_SCOPE:=fruits-postgres}"
: "${PATRONI_NAMESPACE:=/service/}"
: "${PATRONI_NAME:?PATRONI_NAME is required}"
: "${PATRONI_ETCD3_HOSTS:=etcd1:2379,etcd2:2379,etcd3:2379}"
: "${PATRONI_RESTAPI_CONNECT_ADDRESS:=${PATRONI_NAME}:8008}"
: "${PATRONI_POSTGRESQL_CONNECT_ADDRESS:=${PATRONI_NAME}:5432}"

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

export PGDATA="${PGDATA:-/var/lib/postgresql/patroni/data}"
: "${PG_MAJOR:=18}"

PATRONI_CONFIG_DIR=/etc/patroni
PATRONI_CONFIG=${PATRONI_CONFIG_DIR}/patroni.yml
PATRONI_POST_INIT_ENV=${PATRONI_CONFIG_DIR}/post-init.env
PGDATA_PARENT="$(dirname "${PGDATA}")"

mkdir -p "${PGDATA}" "${PATRONI_CONFIG_DIR}" /var/run/postgresql
chown -R postgres:postgres "${PGDATA_PARENT}" "${PATRONI_CONFIG_DIR}" /var/run/postgresql
chmod 700 "${PGDATA}"

write_post_init_env_var() {
  local key="$1"
  local value="${!key}"

  printf '%s=' "${key}" >> "${PATRONI_POST_INIT_ENV}"
  printf '%q\n' "${value}" >> "${PATRONI_POST_INIT_ENV}"
}

: > "${PATRONI_POST_INIT_ENV}"
for key in \
  POSTGRES_DB \
  POSTGRES_USER \
  POSTGRES_PASSWORD \
  TEST_POSTGRES_DB \
  METRICS_DB_USER \
  METRICS_DB_PASSWORD \
  PATRONI_SUPERUSER_USERNAME \
  PATRONI_SUPERUSER_PASSWORD \
  PATRONI_REPLICATION_USERNAME \
  PATRONI_REPLICATION_PASSWORD \
  PATRONI_REWIND_USERNAME \
  PATRONI_REWIND_PASSWORD; do
  write_post_init_env_var "${key}"
done

etcd_hosts_yaml=""
IFS=',' read -ra etcd_hosts <<< "${PATRONI_ETCD3_HOSTS}"
for etcd_host in "${etcd_hosts[@]}"; do
  etcd_host="${etcd_host//[[:space:]]/}"
  [ -n "${etcd_host}" ] || continue
  etcd_hosts_yaml+="    - ${etcd_host}"$'\n'
done

cat > "${PATRONI_CONFIG}" <<EOF_CONFIG
scope: ${PATRONI_SCOPE}
namespace: ${PATRONI_NAMESPACE}
name: ${PATRONI_NAME}

restapi:
  listen: 0.0.0.0:8008
  connect_address: ${PATRONI_RESTAPI_CONNECT_ADDRESS}

etcd3:
  hosts:
${etcd_hosts_yaml}

bootstrap:
  dcs:
    ttl: 30
    loop_wait: 10
    retry_timeout: 10
    maximum_lag_on_failover: 1048576
    postgresql:
      use_pg_rewind: true
      use_slots: true
      remove_data_directory_on_rewind_failure: true
      remove_data_directory_on_diverged_timelines: true
      parameters:
        wal_level: replica
        hot_standby: "on"
        wal_keep_size: 512MB
        max_wal_senders: 10
        max_replication_slots: 10
        password_encryption: scram-sha-256

  initdb:
    - encoding: UTF8
    - data-checksums

  pg_hba:
    - local all all trust
    - host replication ${PATRONI_REPLICATION_USERNAME} 0.0.0.0/0 scram-sha-256
    - host all all 127.0.0.1/32 scram-sha-256
    - host all all 0.0.0.0/0 scram-sha-256

  users:
    ${PATRONI_REPLICATION_USERNAME}:
      password: "${PATRONI_REPLICATION_PASSWORD}"
      options:
        - login
        - replication
    ${PATRONI_REWIND_USERNAME}:
      password: "${PATRONI_REWIND_PASSWORD}"
      options:
        - login
        - superuser
    ${POSTGRES_USER}:
      password: "${POSTGRES_PASSWORD}"
      options:
        - login
        - createdb

  post_init: /usr/local/bin/patroni-post-init.sh

postgresql:
  listen: 0.0.0.0:5432
  connect_address: ${PATRONI_POSTGRESQL_CONNECT_ADDRESS}
  data_dir: ${PGDATA}
  bin_dir: /usr/lib/postgresql/${PG_MAJOR}/bin

  use_pg_rewind: true
  use_slots: true
  remove_data_directory_on_rewind_failure: true
  remove_data_directory_on_diverged_timelines: true

  create_replica_methods:
    - basebackup

  basebackup:
    checkpoint: fast

  authentication:
    superuser:
      username: ${PATRONI_SUPERUSER_USERNAME}
      password: ${PATRONI_SUPERUSER_PASSWORD}
    replication:
      username: ${PATRONI_REPLICATION_USERNAME}
      password: ${PATRONI_REPLICATION_PASSWORD}
    rewind:
      username: ${PATRONI_REWIND_USERNAME}
      password: ${PATRONI_REWIND_PASSWORD}

  pg_hba:
    - local all all trust
    - host replication ${PATRONI_REPLICATION_USERNAME} 0.0.0.0/0 scram-sha-256
    - host all all 127.0.0.1/32 scram-sha-256
    - host all all 0.0.0.0/0 scram-sha-256

  parameters:
    unix_socket_directories: /var/run/postgresql
    listen_addresses: '*'
    wal_level: replica
    hot_standby: "on"
    wal_keep_size: 512MB
    max_wal_senders: 10
    max_replication_slots: 10
    password_encryption: scram-sha-256

tags:
  nofailover: false
  noloadbalance: false
  clonefrom: false
  nosync: false
EOF_CONFIG

chown postgres:postgres "${PATRONI_CONFIG}"
chmod 600 "${PATRONI_CONFIG}"
chown postgres:postgres "${PATRONI_POST_INIT_ENV}"
chmod 600 "${PATRONI_POST_INIT_ENV}"

first_etcd_host="${PATRONI_ETCD3_HOSTS%%,*}"
echo "waiting for etcd at ${first_etcd_host}..."
until curl -fsS "http://${first_etcd_host}/health" >/dev/null 2>&1; do
  sleep 2
done

echo "starting Patroni node ${PATRONI_NAME} with PGDATA=${PGDATA}"
exec gosu postgres patroni "${PATRONI_CONFIG}"
