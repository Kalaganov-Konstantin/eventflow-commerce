#!/bin/bash
set -e

# Create separate databases for each service
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
    -- Create users for each service
    CREATE USER orders_user WITH PASSWORD 'orders_pass';
    CREATE USER payments_user WITH PASSWORD 'payments_pass';
    CREATE USER inventory_user WITH PASSWORD 'inventory_pass';
    CREATE USER notifications_user WITH PASSWORD 'notifications_pass';

    -- Create databases for each service and set the owner
    CREATE DATABASE orders OWNER orders_user;
    CREATE DATABASE payments OWNER payments_user;
    CREATE DATABASE inventory OWNER inventory_user;
    CREATE DATABASE notifications OWNER notifications_user;
EOSQL
