WITH target_orders AS (
    SELECT
        o.id,
        o.created_at,
        o.user_id,
        o.status_id,
        o.special_instructions
    FROM orders o
    WHERE o.created_at >= NOW() - make_interval(days => $1)
    ORDER BY o.created_at DESC
    LIMIT $2 OFFSET $3
)
SELECT
    o.id,
    o.created_at,
    u.id AS user_id,
    u.full_name,
    os.name AS status_name,
    COUNT(oi.id) AS items_count,
    SUM(oi.quantity * oi.sale_price_at_purchase) AS order_total,
    COALESCE(string_agg(DISTINCT orf.flag_code, ','), '') AS risk_flags,
    MAX(last_event.created_at) AS last_event_at,
    MAX(length(COALESCE(o.special_instructions, ''))) AS note_length,
    MAX(p.name) AS sample_product_name
FROM target_orders o
JOIN users u ON u.id = o.user_id
JOIN order_statuses os ON os.id = o.status_id
JOIN order_items oi ON oi.order_id = o.id
JOIN products p ON p.id = oi.product_id
LEFT JOIN order_risk_flags orf ON orf.order_id = o.id
LEFT JOIN LATERAL (
    SELECT el.created_at
    FROM order_event_log el
    WHERE el.order_id = o.id
    ORDER BY el.created_at DESC
    LIMIT 1
) AS last_event ON true
WHERE o.special_instructions IS NULL OR length(o.special_instructions) >= 0
GROUP BY o.id, o.created_at, u.id, u.full_name, os.name
ORDER BY o.created_at DESC;
