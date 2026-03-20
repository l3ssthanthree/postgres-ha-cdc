-- +goose Up
CREATE TABLE users
(
    id         BIGSERIAL PRIMARY KEY,
    full_name  VARCHAR(256) NOT NULL,
    email      VARCHAR(256) NOT NULL UNIQUE,
    phone      VARCHAR(32)  NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE addresses
(
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT       NOT NULL REFERENCES users (id),
    city        VARCHAR(128) NOT NULL,
    street      VARCHAR(256) NOT NULL,
    postal_code VARCHAR(128) NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS addresses;
DROP TABLE IF EXISTS users;