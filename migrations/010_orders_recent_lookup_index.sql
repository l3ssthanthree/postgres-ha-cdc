-- +goose Up
CREATE INDEX idx_order_event_log_order_id_created_at_desc
    ON order_event_log (order_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_order_event_log_order_id_created_at_desc;
