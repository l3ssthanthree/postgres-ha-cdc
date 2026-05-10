-- +goose Up
ALTER TABLE orders
    ADD COLUMN special_instructions TEXT;

-- +goose Down
ALTER TABLE orders
    DROP COLUMN IF EXISTS special_instructions;
