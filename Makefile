include .env

export POSTGRES_DB
export POSTGRES_USER
export POSTGRES_PASSWORD
export POSTGRES_PORT
export GOOSE_DRIVER
export GOOSE_MIGRATION_DIR
export GOOSE_DBSTRING

up:
	goose -dir $(GOOSE_MIGRATION_DIR) up

up-by-one:
	goose -dir $(GOOSE_MIGRATION_DIR) up-by-one

down:
	goose -dir $(GOOSE_MIGRATION_DIR) down

redo:
	goose -dir $(GOOSE_MIGRATION_DIR) redo

status:
	goose -dir $(GOOSE_MIGRATION_DIR) status

version:
	goose -dir $(GOOSE_MIGRATION_DIR) version

create:
	goose -dir $(GOOSE_MIGRATION_DIR) create $(name) sql

up-to:
	goose -dir $(GOOSE_MIGRATION_DIR) up-to $(version)

down-to:
	goose -dir $(GOOSE_MIGRATION_DIR) down-to $(version)

reset:
	goose -dir $(GOOSE_MIGRATION_DIR) reset

validate:
	goose -dir $(GOOSE_MIGRATION_DIR) validate

fix:
	goose -dir $(GOOSE_MIGRATION_DIR) fix

migrate:
ifdef MIGRATION_VERSION
	goose -dir $(GOOSE_MIGRATION_DIR) up-to $(MIGRATION_VERSION)
else
	goose -dir $(GOOSE_MIGRATION_DIR) up
endif