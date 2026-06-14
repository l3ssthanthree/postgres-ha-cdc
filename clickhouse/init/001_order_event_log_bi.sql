CREATE DATABASE IF NOT EXISTS fruits_bi;

CREATE TABLE IF NOT EXISTS fruits_bi.order_event_log_queue
(
    message String
)
ENGINE = Kafka
SETTINGS
    kafka_broker_list = 'kafka:9092',
    kafka_topic_list = 'fruits.public.order_event_log',
    kafka_group_name = 'fruits_clickhouse_order_event_log_cdc',
    kafka_format = 'JSONAsString',
    kafka_num_consumers = 1,
    kafka_skip_broken_messages = 100;

CREATE TABLE IF NOT EXISTS fruits_bi.order_event_log_cdc
(
    id UInt64,
    order_id Nullable(UInt64),
    event_type LowCardinality(String),
    actor_type LowCardinality(String),
    payload String,
    step Nullable(Int32),
    status_id Nullable(UInt64),
    created_at DateTime64(6, 'UTC'),
    op LowCardinality(String),
    is_deleted UInt8,
    version_ts_ms UInt64,
    source_lsn UInt64,
    event_version UInt64,
    kafka_loaded_at DateTime64(3, 'UTC') DEFAULT now64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(event_version)
PARTITION BY toYYYYMM(created_at)
ORDER BY id;

CREATE MATERIALIZED VIEW IF NOT EXISTS fruits_bi.mv_order_event_log_cdc
TO fruits_bi.order_event_log_cdc
AS
WITH
    JSONExtractString(message, 'op') AS op_value,
    if(op_value = 'd', JSONExtractRaw(message, 'before'), JSONExtractRaw(message, 'after')) AS row_json,
    JSONExtractRaw(row_json, 'payload') AS payload_json_raw,
    JSONExtractString(row_json, 'payload') AS payload_json_string,
    if(payload_json_string != '', payload_json_string, if(payload_json_raw = '' OR payload_json_raw = 'null', '{}', payload_json_raw)) AS payload_json,
    JSON_VALUE(row_json, '$.created_at') AS created_at_raw,
    toUInt64OrZero(JSON_VALUE(row_json, '$.id')) AS event_id,
    toUInt64OrZero(JSON_VALUE(message, '$.ts_ms')) AS ts_ms_value,
    toUInt64OrZero(JSON_VALUE(message, '$.source.lsn')) AS source_lsn_value,
    if(source_lsn_value > 0, source_lsn_value, ts_ms_value) AS event_version_value
SELECT
    event_id AS id,
    toUInt64OrNull(JSON_VALUE(row_json, '$.order_id')) AS order_id,
    ifNull(nullIf(JSON_VALUE(row_json, '$.event_type'), ''), 'unknown') AS event_type,
    ifNull(nullIf(JSON_VALUE(row_json, '$.actor_type'), ''), 'unknown') AS actor_type,
    payload_json AS payload,
    toInt32OrNull(JSON_VALUE(payload_json, '$.step')) AS step,
    toUInt64OrNull(JSON_VALUE(payload_json, '$.status_id')) AS status_id,
    coalesce(
        parseDateTime64BestEffortOrNull(created_at_raw, 6, 'UTC'),
        now64(6, 'UTC')
    ) AS created_at,
    op_value AS op,
    if(op_value = 'd', 1, 0) AS is_deleted,
    ts_ms_value AS version_ts_ms,
    source_lsn_value AS source_lsn,
    event_version_value AS event_version
FROM fruits_bi.order_event_log_queue
WHERE op_value IN ('c', 'r', 'u', 'd')
  AND row_json != ''
  AND row_json != 'null'
  AND event_id > 0;

CREATE VIEW IF NOT EXISTS fruits_bi.v_order_event_log_current AS
SELECT
    id,
    order_id,
    event_type,
    actor_type,
    payload,
    step,
    status_id,
    created_at,
    last_op,
    is_deleted,
    latest_event_version AS event_version,
    last_cdc_loaded_at
FROM
(
    SELECT
        id,
        argMax(order_id, event_version) AS order_id,
        argMax(event_type, event_version) AS event_type,
        argMax(actor_type, event_version) AS actor_type,
        argMax(payload, event_version) AS payload,
        argMax(step, event_version) AS step,
        argMax(status_id, event_version) AS status_id,
        argMax(created_at, event_version) AS created_at,
        argMax(op, event_version) AS last_op,
        argMax(is_deleted, event_version) AS is_deleted,
        max(event_version) AS latest_event_version,
        max(kafka_loaded_at) AS last_cdc_loaded_at
    FROM fruits_bi.order_event_log_cdc
    GROUP BY id
)
WHERE is_deleted = 0;

CREATE VIEW IF NOT EXISTS fruits_bi.v_bi_order_event_kpi AS
SELECT
    count() AS current_events,
    uniqExact(order_id) AS affected_orders,
    round(count() / nullIf(uniqExact(order_id), 0), 2) AS avg_events_per_order,
    round(avg(step), 2) AS avg_workflow_step,
    max(created_at) AS newest_event_at,
    max(last_cdc_loaded_at) AS last_cdc_loaded_at
FROM fruits_bi.v_order_event_log_current;

CREATE VIEW IF NOT EXISTS fruits_bi.v_bi_order_events_by_day AS
SELECT
    toDate(created_at) AS day,
    count() AS events_count,
    uniqExact(order_id) AS affected_orders,
    countIf(actor_type = 'system') AS system_events,
    countIf(actor_type = 'worker') AS worker_events,
    countIf(actor_type = 'user') AS user_events,
    round(avg(step), 2) AS avg_workflow_step
FROM fruits_bi.v_order_event_log_current
GROUP BY day
ORDER BY day;

CREATE VIEW IF NOT EXISTS fruits_bi.v_bi_order_events_by_type AS
SELECT
    event_type,
    count() AS events_count,
    uniqExact(order_id) AS affected_orders,
    round(avg(step), 2) AS avg_workflow_step
FROM fruits_bi.v_order_event_log_current
GROUP BY event_type
ORDER BY events_count DESC;

CREATE VIEW IF NOT EXISTS fruits_bi.v_bi_order_events_by_actor AS
SELECT
    actor_type,
    count() AS events_count,
    uniqExact(order_id) AS affected_orders,
    round(avg(step), 2) AS avg_workflow_step
FROM fruits_bi.v_order_event_log_current
GROUP BY actor_type
ORDER BY events_count DESC;

CREATE USER IF NOT EXISTS metabase_ch
IDENTIFIED WITH sha256_password BY 'metabase_ch_password';

GRANT SELECT ON fruits_bi.* TO metabase_ch;
