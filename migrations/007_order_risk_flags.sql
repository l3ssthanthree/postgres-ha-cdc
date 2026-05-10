-- +goose Up
CREATE TABLE order_risk_flags
(
    id         BIGSERIAL PRIMARY KEY,
    order_id   BIGINT      NOT NULL REFERENCES orders (id),
    flag_code  VARCHAR(64) NOT NULL,
    severity   SMALLINT    NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (order_id, flag_code)
);

-- +goose Down
DROP TABLE IF EXISTS order_risk_flags;
