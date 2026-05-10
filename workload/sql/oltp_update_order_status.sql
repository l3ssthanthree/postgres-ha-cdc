WITH current_order AS (
    SELECT
        o.id,
        os.name AS current_status
    FROM orders o
    JOIN order_statuses os ON os.id = o.status_id
    WHERE o.id = $1
),
next_status AS (
    SELECT id
    FROM order_statuses
    WHERE name = CASE
        WHEN (SELECT current_status FROM current_order) = 'new' THEN 'paid'
        WHEN (SELECT current_status FROM current_order) = 'paid' THEN 'processing'
        WHEN (SELECT current_status FROM current_order) = 'processing' THEN 'shipped'
        WHEN (SELECT current_status FROM current_order) = 'shipped' THEN 'delivered'
        WHEN (SELECT current_status FROM current_order) = 'delivered' THEN 'delivered'
        ELSE 'cancelled'
    END
)
UPDATE orders o
SET status_id = (SELECT id FROM next_status)
WHERE o.id = $1
RETURNING o.id, o.status_id, o.created_at;
