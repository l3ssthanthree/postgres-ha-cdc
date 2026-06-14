#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

CONNECT_URL="${CONNECT_URL:-http://localhost:8083}"
CONNECTOR_NAME="${DEBEZIUM_CONNECTOR_NAME:-postgres-connector}"
CONFIG_FILE="${CONFIG_FILE:-${SCRIPT_DIR}/postgres-connector.json}"

if curl -fsS \
  -X PUT \
  -H "Content-Type: application/json" \
  --data-binary "@${CONFIG_FILE}" \
  -o /dev/null \
  "${CONNECT_URL}/connectors/${CONNECTOR_NAME}/config"; then
  printf 'Connector %s registered via %s\n' "${CONNECTOR_NAME}" "${CONNECT_URL}"
  exit 0
fi

printf 'Direct Kafka Connect REST call failed, retrying from kafka-connect container...\n' >&2

CONTAINER_CONFIG="/tmp/${CONNECTOR_NAME}.json"
docker compose cp "${CONFIG_FILE}" "kafka-connect:${CONTAINER_CONFIG}"
docker compose exec -T kafka-connect curl -fsS \
  -X PUT \
  -H "Content-Type: application/json" \
  --data-binary "@${CONTAINER_CONFIG}" \
  -o /dev/null \
  "http://localhost:8083/connectors/${CONNECTOR_NAME}/config"

printf 'Connector %s registered from kafka-connect container\n' "${CONNECTOR_NAME}"
