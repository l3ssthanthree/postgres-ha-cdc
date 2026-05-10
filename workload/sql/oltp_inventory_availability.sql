SELECT
    p.id,
    p.name,
    c.name AS category_name,
    latest_price.price AS current_price,
    SUM(ws.qty_available) AS total_warehouse_stock,
    COALESCE(AVG(ss.qty_available), 0) AS avg_supplier_stock
FROM products p
JOIN categories c ON c.id = p.category_id
JOIN warehouse_stock ws ON ws.product_id = p.id
LEFT JOIN supplier_products sp ON sp.product_id = p.id
LEFT JOIN supplier_stock ss ON ss.supplier_product_id = sp.id
JOIN LATERAL (
    SELECT pph.price
    FROM product_price_history pph
    WHERE pph.product_id = p.id
    ORDER BY pph.created_at DESC
    LIMIT 1
) AS latest_price ON true
WHERE p.name ILIKE $1
GROUP BY p.id, p.name, c.name, latest_price.price
ORDER BY total_warehouse_stock DESC, p.id
LIMIT $2;
