-- +goose Up
CREATE TABLE payment_statuses
(
    id   BIGSERIAL PRIMARY KEY,
    name VARCHAR(256) NOT NULL UNIQUE
);

CREATE TABLE payments
(
    id        BIGSERIAL PRIMARY KEY,
    order_id  BIGINT         NOT NULL REFERENCES orders (id),
    status_id BIGINT         NOT NULL REFERENCES payment_statuses (id),
    amount    NUMERIC(10, 2) NOT NULL,
    paid_at   TIMESTAMPTZ
);

-- +goose Down
DROP TABLE IF EXISTS payments;
DROP TABLE IF EXISTS payment_statuses;