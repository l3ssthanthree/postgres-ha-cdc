-- +goose Up
CREATE MATERIALIZED VIEW mv_daily_category_sales AS
SELECT
    date_trunc('day', o.created_at) AS day_bucket,
    c.id AS category_id,
    c.name AS category_name,
    COUNT(DISTINCT o.id) AS orders_count,
    SUM(oi.quantity) AS items_sold,
    SUM(oi.quantity * oi.sale_price_at_purchase) AS revenue
FROM orders o
JOIN order_statuses os ON os.id = o.status_id
JOIN order_items oi ON oi.order_id = o.id
JOIN products p ON p.id = oi.product_id
JOIN categories c ON c.id = p.category_id
WHERE os.name <> 'cancelled'
GROUP BY 1, 2, 3;

CREATE UNIQUE INDEX idx_mv_daily_category_sales_day_category
    ON mv_daily_category_sales (day_bucket, category_id);

-- +goose Down
DROP INDEX IF EXISTS idx_mv_daily_category_sales_day_category;
DROP MATERIALIZED VIEW IF EXISTS mv_daily_category_sales;
