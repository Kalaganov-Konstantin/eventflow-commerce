-- Drop constraints
ALTER TABLE payment_status DROP CONSTRAINT IF EXISTS chk_positive_amount;

-- Drop indexes
DROP INDEX IF EXISTS idx_payment_snapshots_latest;
DROP INDEX IF EXISTS idx_payment_events_unique_version;

DROP INDEX IF EXISTS idx_payment_status_created_at;
DROP INDEX IF EXISTS idx_payment_status_status;
DROP INDEX IF EXISTS idx_payment_status_order_id;
DROP INDEX IF EXISTS idx_payment_status_customer_id;

DROP INDEX IF EXISTS idx_payment_snapshots_version;
DROP INDEX IF EXISTS idx_payment_snapshots_aggregate_id;

DROP INDEX IF EXISTS idx_payment_events_aggregate_version;
DROP INDEX IF EXISTS idx_payment_events_sequence;
DROP INDEX IF EXISTS idx_payment_events_occurred_at;
DROP INDEX IF EXISTS idx_payment_events_event_type;
DROP INDEX IF EXISTS idx_payment_events_aggregate_type;
DROP INDEX IF EXISTS idx_payment_events_aggregate_id;

-- Drop tables
DROP TABLE IF EXISTS payment_status;
DROP TABLE IF EXISTS payment_snapshots;
DROP TABLE IF EXISTS payment_events;
