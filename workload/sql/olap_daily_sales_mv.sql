SELECT
    day_bucket,
    category_name,
    orders_count,
    items_sold,
    revenue
FROM mv_daily_category_sales
WHERE day_bucket >= date_trunc('day', NOW() - make_interval(days => $1))
ORDER BY day_bucket DESC, revenue DESC
LIMIT $2;
