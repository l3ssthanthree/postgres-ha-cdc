SELECT
    o.id,
    o.created_at,
    u.id AS user_id,
    u.full_name,
    os.name AS status_name,
    COUNT(oi.id) AS items_count,
    SUM(oi.quantity * oi.sale_price_at_purchase) AS order_total,
    MAX(p.name) AS sample_product_name
FROM orders o
JOIN users u ON u.id = o.user_id
JOIN order_statuses os ON os.id = o.status_id
JOIN order_items oi ON oi.order_id = o.id
JOIN products p ON p.id = oi.product_id
WHERE o.created_at >= NOW() - make_interval(days => $1)
GROUP BY o.id, o.created_at, u.id, u.full_name, os.name
ORDER BY o.created_at DESC
LIMIT $2 OFFSET $3;
