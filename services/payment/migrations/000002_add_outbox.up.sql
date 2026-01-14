-- Transactional outbox: domain events are written here in the same transaction as the state
-- change they describe, and a relay publishes them to Kafka.
CREATE TABLE outbox_messages (
    id UUID PRIMARY KEY,
    topic VARCHAR(255) NOT NULL,
    event_type VARCHAR(255) NOT NULL,
    aggregate_id VARCHAR(255) NOT NULL,
    payload JSONB NOT NULL,
    correlation_id VARCHAR(255),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    published_at TIMESTAMP WITH TIME ZONE,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT
);

-- Speeds up the relay's poll for unpublished rows without scanning already published ones.
CREATE INDEX idx_outbox_unpublished ON outbox_messages (created_at) WHERE published_at IS NULL;

-- Records handled Kafka event ids so consumers can recognize redelivered messages.
CREATE TABLE processed_events (
    event_id UUID PRIMARY KEY,
    event_type VARCHAR(255) NOT NULL,
    processed_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
