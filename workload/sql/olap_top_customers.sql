WITH customer_sales AS (
    SELECT
        u.id AS user_id,
        u.full_name,
        SUM(oi.quantity * oi.sale_price_at_purchase) AS revenue,
        COUNT(DISTINCT o.id) AS orders_count,
        MAX(o.created_at) AS last_order_at
    FROM users u
    JOIN orders o ON o.user_id = u.id
    JOIN order_statuses os ON os.id = o.status_id
    JOIN order_items oi ON oi.order_id = o.id
    WHERE o.created_at >= NOW() - make_interval(days => $1)
      AND os.name <> 'cancelled'
    GROUP BY u.id, u.full_name
),
ranked AS (
    SELECT
        *,
        dense_rank() OVER (ORDER BY revenue DESC) AS revenue_rank
    FROM customer_sales
)
SELECT
    user_id,
    full_name,
    revenue,
    orders_count,
    last_order_at,
    revenue_rank
FROM ranked
WHERE revenue_rank <= $2
ORDER BY revenue_rank, user_id;
