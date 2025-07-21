-- Drop triggers
DROP TRIGGER IF EXISTS update_inventory_updated_at ON inventory;
DROP TRIGGER IF EXISTS update_products_updated_at ON products;

-- Drop trigger function
DROP FUNCTION IF EXISTS update_updated_at_column();

-- Drop indexes
DROP INDEX IF EXISTS idx_categories_name;
DROP INDEX IF EXISTS idx_categories_parent;

DROP INDEX IF EXISTS idx_inventory_movements_created_at;
DROP INDEX IF EXISTS idx_inventory_movements_reference;
DROP INDEX IF EXISTS idx_inventory_movements_type;
DROP INDEX IF EXISTS idx_inventory_movements_product_id;

DROP INDEX IF EXISTS idx_inventory_product_warehouse;
DROP INDEX IF EXISTS idx_inventory_reorder_level;
DROP INDEX IF EXISTS idx_inventory_quantity_available;
DROP INDEX IF EXISTS idx_inventory_warehouse_id;
DROP INDEX IF EXISTS idx_inventory_product_id;

DROP INDEX IF EXISTS idx_products_created_at;
DROP INDEX IF EXISTS idx_products_is_active;
DROP INDEX IF EXISTS idx_products_brand;
DROP INDEX IF EXISTS idx_products_category;
DROP INDEX IF EXISTS idx_products_name;
DROP INDEX IF EXISTS idx_products_sku;

-- Drop tables (order matters due to foreign keys)
DROP TABLE IF EXISTS inventory_movements;
DROP TABLE IF EXISTS inventory;
DROP TABLE IF EXISTS product_categories;
DROP TABLE IF EXISTS products;
