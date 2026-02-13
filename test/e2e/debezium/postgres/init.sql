-- PostgreSQL initialization script for Debezium CDC testing
-- This script creates the test tables and enables logical replication

-- Create the test database (if not created by POSTGRES_DB)
-- Note: POSTGRES_DB env var creates the database automatically

-- Create a schema for CDC testing
CREATE SCHEMA IF NOT EXISTS inventory;

-- Enable pgcrypto extension for UUID generation
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Create the orders table for CDC testing
CREATE TABLE IF NOT EXISTS inventory.orders (
    id SERIAL PRIMARY KEY,
    order_id VARCHAR(50) NOT NULL UNIQUE,
    customer_id VARCHAR(50) NOT NULL,
    amount DECIMAL(10, 2) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Create an audit table to track changes
CREATE TABLE IF NOT EXISTS inventory.order_audit (
    id SERIAL PRIMARY KEY,
    order_id VARCHAR(50) NOT NULL,
    action VARCHAR(10) NOT NULL,
    old_data JSONB,
    new_data JSONB,
    changed_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Create trigger function for audit logging
CREATE OR REPLACE FUNCTION inventory.audit_order_changes()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        INSERT INTO inventory.order_audit (order_id, action, new_data)
        VALUES (NEW.order_id, 'INSERT', to_jsonb(NEW));
    ELSIF TG_OP = 'UPDATE' THEN
        INSERT INTO inventory.order_audit (order_id, action, old_data, new_data)
        VALUES (NEW.order_id, 'UPDATE', to_jsonb(OLD), to_jsonb(NEW));
    ELSIF TG_OP = 'DELETE' THEN
        INSERT INTO inventory.order_audit (order_id, action, old_data)
        VALUES (OLD.order_id, 'DELETE', to_jsonb(OLD));
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger for orders table
DROP TRIGGER IF EXISTS orders_audit_trigger ON inventory.orders;
CREATE TRIGGER orders_audit_trigger
AFTER INSERT OR UPDATE OR DELETE ON inventory.orders
FOR EACH ROW EXECUTE FUNCTION inventory.audit_order_changes();

-- Create publication for logical replication (Debezium)
CREATE PUBLICATION debezium_publication FOR ALL TABLES;

-- Grant permissions
GRANT ALL PRIVILEGES ON SCHEMA inventory TO postgres;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA inventory TO postgres;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA inventory TO postgres;

-- Insert some initial data
INSERT INTO inventory.orders (order_id, customer_id, amount, status) VALUES
    ('initial-001', 'customer-init', 50.00, 'pending');

-- Output confirmation
DO $$
BEGIN
    RAISE NOTICE 'PostgreSQL CDC test database initialized successfully';
    RAISE NOTICE 'Tables created: inventory.orders, inventory.order_audit';
    RAISE NOTICE 'Publication created: debezium_publication';
END $$;
