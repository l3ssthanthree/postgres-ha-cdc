SELECT
    date_trunc('day', o.created_at) AS day_bucket,
    c.name AS category_name,
    COUNT(DISTINCT o.id) AS orders_count,
    SUM(oi.quantity) AS items_sold,
    SUM(oi.quantity * oi.sale_price_at_purchase) AS revenue
FROM orders o
JOIN order_statuses os ON os.id = o.status_id
JOIN order_items oi ON oi.order_id = o.id
JOIN products p ON p.id = oi.product_id
JOIN categories c ON c.id = p.category_id
WHERE o.created_at >= NOW() - make_interval(days => $1)
  AND os.name <> 'cancelled'
GROUP BY 1, 2
ORDER BY day_bucket DESC, revenue DESC
LIMIT $2;
