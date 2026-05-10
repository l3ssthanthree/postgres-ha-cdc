-- +goose Up
CREATE INDEX idx_product_price_history_product_id_created_at_desc
    ON product_price_history (product_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_product_price_history_product_id_created_at_desc;
