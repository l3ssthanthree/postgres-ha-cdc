-- +goose Up
CREATE TABLE suppliers
(
    id            BIGSERIAL PRIMARY KEY,
    name          VARCHAR(256) NOT NULL,
    contact_phone VARCHAR(32)  NOT NULL,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE categories
(
    id   BIGSERIAL PRIMARY KEY,
    name VARCHAR(256) NOT NULL UNIQUE
);

CREATE TABLE products
(
    id          BIGSERIAL PRIMARY KEY,
    category_id BIGINT       NOT NULL REFERENCES categories (id),
    name        VARCHAR(256) NOT NULL,
    description TEXT,
    unit        VARCHAR(64)  NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE supplier_products
(
    id             BIGSERIAL PRIMARY KEY,
    supplier_id    BIGINT         NOT NULL REFERENCES suppliers (id),
    product_id     BIGINT         NOT NULL REFERENCES products (id),
    purchase_price NUMERIC(10, 2) NOT NULL,
    created_at     TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    UNIQUE (supplier_id, product_id)
);

CREATE TABLE product_price_history
(
    id         BIGSERIAL PRIMARY KEY,
    product_id BIGINT         NOT NULL REFERENCES products (id),
    price      NUMERIC(10, 2) NOT NULL,
    created_at TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);

CREATE TABLE warehouses
(
    id   BIGSERIAL PRIMARY KEY,
    name VARCHAR(256) NOT NULL UNIQUE
);

CREATE TABLE warehouse_stock
(
    id            BIGSERIAL PRIMARY KEY,
    warehouse_id  BIGINT  NOT NULL REFERENCES warehouses (id),
    product_id    BIGINT  NOT NULL REFERENCES products (id),
    qty_available INTEGER NOT NULL,
    UNIQUE (warehouse_id, product_id)
);

CREATE TABLE supplier_stock
(
    id                  BIGSERIAL PRIMARY KEY,
    supplier_product_id BIGINT      NOT NULL UNIQUE REFERENCES supplier_products (id),
    qty_available       INTEGER     NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down

DROP TABLE IF EXISTS supplier_stock;
DROP TABLE IF EXISTS warehouse_stock;
DROP TABLE IF EXISTS warehouses;
DROP TABLE IF EXISTS product_price_history;
DROP TABLE IF EXISTS supplier_products;
DROP TABLE IF EXISTS products;
DROP TABLE IF EXISTS categories;
DROP TABLE IF EXISTS suppliers;