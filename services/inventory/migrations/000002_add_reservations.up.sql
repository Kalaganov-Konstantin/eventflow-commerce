-- Recreate the product/warehouse index treating NULL warehouses as equal, so a product without a
-- warehouse cannot get more than one inventory row.
DROP INDEX IF EXISTS idx_inventory_product_warehouse;
CREATE UNIQUE INDEX idx_inventory_product_warehouse ON inventory (product_id, warehouse_id) NULLS NOT DISTINCT;

CREATE TABLE inventory_reservations (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    order_id UUID NOT NULL,
    product_id UUID NOT NULL REFERENCES products(id),
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    status VARCHAR(20) NOT NULL CHECK (status IN ('reserved', 'released', 'committed')),
    expires_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_inventory_reservations_order_product ON inventory_reservations (order_id, product_id);
CREATE INDEX idx_inventory_reservations_status ON inventory_reservations (status);

CREATE TRIGGER update_inventory_reservations_updated_at
    BEFORE UPDATE ON inventory_reservations
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
