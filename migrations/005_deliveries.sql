-- +goose Up
CREATE TABLE delivery_statuses
(
    id   BIGSERIAL PRIMARY KEY,
    name VARCHAR(256) NOT NULL UNIQUE
);

CREATE TABLE deliveries
(
    id         BIGSERIAL PRIMARY KEY,
    order_id   BIGINT NOT NULL REFERENCES orders (id),
    address_id BIGINT NOT NULL REFERENCES addresses (id),
    status_id  BIGINT NOT NULL REFERENCES delivery_statuses (id)
);

-- +goose Down
DROP TABLE IF EXISTS deliveries;
DROP TABLE IF EXISTS delivery_statuses;