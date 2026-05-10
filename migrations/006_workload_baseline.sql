-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

CREATE TABLE order_event_log
(
    id         BIGSERIAL PRIMARY KEY,
    order_id    BIGINT REFERENCES orders (id),
    event_type  VARCHAR(64) NOT NULL,
    actor_type  VARCHAR(64) NOT NULL,
    payload     JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE IF EXISTS order_event_log;
DROP EXTENSION IF EXISTS pg_stat_statements;
