DROP TRIGGER IF EXISTS update_inventory_reservations_updated_at ON inventory_reservations;

DROP INDEX IF EXISTS idx_inventory_reservations_status;
DROP INDEX IF EXISTS idx_inventory_reservations_order_product;

DROP TABLE IF EXISTS inventory_reservations;

DROP INDEX IF EXISTS idx_inventory_product_warehouse;
CREATE UNIQUE INDEX idx_inventory_product_warehouse ON inventory (product_id, warehouse_id);
