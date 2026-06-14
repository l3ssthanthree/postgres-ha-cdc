BEGIN;

CREATE TEMP TABLE cdc_smoke_ids (
    id bigint PRIMARY KEY
) ON COMMIT DROP;

CREATE TEMP TABLE cdc_smoke_result (
    action text PRIMARY KEY,
    rows_count integer NOT NULL
) ON COMMIT DROP;

WITH picked_order AS (
    SELECT id, status_id
    FROM orders
    ORDER BY id DESC
    LIMIT 1
),
inserted AS (
    INSERT INTO order_event_log (order_id, event_type, actor_type, payload, created_at)
    SELECT
        po.id,
        'manual_bi_check',
        CASE WHEN gs.n = 1 THEN 'user' ELSE 'worker' END,
        jsonb_build_object(
            'step', 90 + gs.n,
            'order_id', po.id,
            'status_id', po.status_id,
            'manual_check', true
        ),
        now()
    FROM picked_order po
    CROSS JOIN generate_series(1, 2) AS gs(n)
    RETURNING id
)
INSERT INTO cdc_smoke_ids (id)
SELECT id
FROM inserted;

INSERT INTO cdc_smoke_result (action, rows_count)
SELECT 'inserted', count(*)::integer
FROM cdc_smoke_ids;

WITH updated AS (
    UPDATE order_event_log
    SET
        actor_type = 'system',
        payload = payload || jsonb_build_object('step', 99, 'manual_update', true)
    WHERE id = (SELECT min(id) FROM cdc_smoke_ids)
    RETURNING id
)
INSERT INTO cdc_smoke_result (action, rows_count)
SELECT 'updated', count(*)::integer
FROM updated;

WITH deleted AS (
    DELETE FROM order_event_log
    WHERE id = (SELECT max(id) FROM cdc_smoke_ids)
    RETURNING id
)
INSERT INTO cdc_smoke_result (action, rows_count)
SELECT 'deleted', count(*)::integer
FROM deleted;

SELECT
    max(rows_count) FILTER (WHERE action = 'inserted') AS inserted_rows,
    max(rows_count) FILTER (WHERE action = 'updated') AS updated_rows,
    max(rows_count) FILTER (WHERE action = 'deleted') AS deleted_rows
FROM cdc_smoke_result;

COMMIT;
