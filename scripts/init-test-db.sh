#!/usr/bin/env bash
set -e

psql -U "$POSTGRES_USER" -d postgres -c "CREATE DATABASE ${TEST_POSTGRES_DB};"