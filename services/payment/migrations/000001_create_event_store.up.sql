-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Create event store table for Event Sourcing
CREATE TABLE payment_events (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    aggregate_id UUID NOT NULL,
    aggregate_type VARCHAR(100) NOT NULL DEFAULT 'payment',
    event_type VARCHAR(100) NOT NULL,
    event_version INTEGER NOT NULL,
    event_data JSONB NOT NULL,
    metadata JSONB,
    occurred_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    sequence_number BIGSERIAL
);

-- Create payment snapshots table for performance optimization
CREATE TABLE payment_snapshots (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    aggregate_id UUID NOT NULL,
    aggregate_version INTEGER NOT NULL,
    snapshot_data JSONB NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create payment read model for queries
CREATE TABLE payment_status (
    id UUID PRIMARY KEY,
    customer_id UUID NOT NULL,
    order_id UUID,
    amount DECIMAL(12,2) NOT NULL,
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    status VARCHAR(50) NOT NULL CHECK (status IN ('initiated', 'processing', 'completed', 'failed', 'refunded', 'cancelled')),
    payment_method VARCHAR(50),
    transaction_id VARCHAR(255),
    gateway_response JSONB,
    created_at TIMESTAMP WITH TIME ZONE,
    updated_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE,
    version INTEGER NOT NULL
);

-- Create indexes for event store
CREATE INDEX idx_payment_events_aggregate_id ON payment_events(aggregate_id);
CREATE INDEX idx_payment_events_aggregate_type ON payment_events(aggregate_type);
CREATE INDEX idx_payment_events_event_type ON payment_events(event_type);
CREATE INDEX idx_payment_events_occurred_at ON payment_events(occurred_at DESC);
CREATE INDEX idx_payment_events_sequence ON payment_events(sequence_number);

-- Composite index for event reconstruction
CREATE INDEX idx_payment_events_aggregate_version ON payment_events(aggregate_id, event_version);

-- Create indexes for snapshots
CREATE INDEX idx_payment_snapshots_aggregate_id ON payment_snapshots(aggregate_id);
CREATE INDEX idx_payment_snapshots_version ON payment_snapshots(aggregate_id, aggregate_version DESC);

-- Create indexes for read model
CREATE INDEX idx_payment_status_customer_id ON payment_status(customer_id);
CREATE INDEX idx_payment_status_order_id ON payment_status(order_id);
CREATE INDEX idx_payment_status_status ON payment_status(status);
CREATE INDEX idx_payment_status_created_at ON payment_status(created_at DESC);

-- Create unique constraint to ensure event ordering
CREATE UNIQUE INDEX idx_payment_events_unique_version ON payment_events(aggregate_id, event_version);

-- Create unique constraint for latest snapshot
CREATE UNIQUE INDEX idx_payment_snapshots_latest ON payment_snapshots(aggregate_id, aggregate_version DESC);

-- Add constraint to ensure positive amounts
ALTER TABLE payment_status ADD CONSTRAINT chk_positive_amount CHECK (amount >= 0);
