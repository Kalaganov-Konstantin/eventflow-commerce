DROP TRIGGER IF EXISTS update_order_sagas_updated_at ON order_sagas;
DROP INDEX IF EXISTS idx_order_sagas_state;
DROP TABLE order_sagas;
