-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_roles
        WHERE rolname = 'fruits_debezium'
    ) THEN
        CREATE ROLE fruits_debezium
            WITH REPLICATION LOGIN PASSWORD 'debezium_password';
    ELSE
        ALTER ROLE fruits_debezium
            WITH REPLICATION LOGIN PASSWORD 'debezium_password';
    END IF;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
BEGIN
    EXECUTE format('GRANT CONNECT ON DATABASE %I TO fruits_debezium', current_database());
END $$;
-- +goose StatementEnd

GRANT USAGE ON SCHEMA public TO fruits_debezium;
GRANT SELECT ON TABLE order_event_log TO fruits_debezium;

ALTER TABLE order_event_log REPLICA IDENTITY FULL;

DROP PUBLICATION IF EXISTS fruits_order_event_log_publication;
CREATE PUBLICATION fruits_order_event_log_publication
    FOR TABLE order_event_log;

-- +goose Down
DROP PUBLICATION IF EXISTS fruits_order_event_log_publication;

ALTER TABLE order_event_log REPLICA IDENTITY DEFAULT;

REVOKE SELECT ON TABLE order_event_log FROM fruits_debezium;
REVOKE USAGE ON SCHEMA public FROM fruits_debezium;

-- +goose StatementBegin
DO $$
BEGIN
    EXECUTE format('REVOKE CONNECT ON DATABASE %I FROM fruits_debezium', current_database());
END $$;
-- +goose StatementEnd
