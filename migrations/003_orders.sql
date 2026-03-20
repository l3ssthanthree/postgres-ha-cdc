-- +goose Up
CREATE TABLE order_statuses
(
    id   BIGSERIAL PRIMARY KEY,
    name VARCHAR(256) NOT NULL UNIQUE
);

CREATE TABLE orders
(
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users (id),
    status_id  BIGINT      NOT NULL REFERENCES order_statuses (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE order_items
(
    id                     BIGSERIAL PRIMARY KEY,
    order_id               BIGINT         NOT NULL REFERENCES orders (id),
    product_id             BIGINT         NOT NULL REFERENCES products (id),
    quantity               INTEGER        NOT NULL,
    sale_price_at_purchase NUMERIC(10, 2) NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS order_items;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS order_statuses;