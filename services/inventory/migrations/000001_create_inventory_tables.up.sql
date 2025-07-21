-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Create products table
CREATE TABLE products (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    sku VARCHAR(100) UNIQUE NOT NULL,
    category VARCHAR(100),
    brand VARCHAR(100),
    price DECIMAL(12,2) NOT NULL CHECK (price >= 0),
    cost DECIMAL(12,2) CHECK (cost >= 0),
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    weight DECIMAL(8,3),
    dimensions JSONB, -- {length, width, height}
    attributes JSONB, -- flexible product attributes
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    version INTEGER DEFAULT 1
);

-- Create inventory table for stock management
CREATE TABLE inventory (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    warehouse_id UUID, -- for multi-warehouse support
    quantity_available INTEGER NOT NULL DEFAULT 0 CHECK (quantity_available >= 0),
    quantity_reserved INTEGER NOT NULL DEFAULT 0 CHECK (quantity_reserved >= 0),
    quantity_total INTEGER GENERATED ALWAYS AS (quantity_available + quantity_reserved) STORED,
    reorder_level INTEGER DEFAULT 0,
    max_stock_level INTEGER,
    last_restocked_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    version INTEGER DEFAULT 1
);

-- Create inventory movements table for tracking stock changes
CREATE TABLE inventory_movements (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    product_id UUID NOT NULL REFERENCES products(id),
    movement_type VARCHAR(50) NOT NULL CHECK (movement_type IN ('purchase', 'sale', 'adjustment', 'return', 'transfer', 'reservation', 'release')),
    quantity INTEGER NOT NULL, -- positive for inbound, negative for outbound
    reference_id UUID, -- order_id, purchase_id, etc.
    reference_type VARCHAR(50), -- 'order', 'purchase', 'adjustment', etc.
    reason TEXT,
    cost_per_unit DECIMAL(12,2),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    created_by UUID -- user who made the movement
);

-- Create product categories table
CREATE TABLE product_categories (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(100) UNIQUE NOT NULL,
    description TEXT,
    parent_category_id UUID REFERENCES product_categories(id),
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create indexes for products
CREATE INDEX idx_products_sku ON products(sku);
CREATE INDEX idx_products_name ON products(name);
CREATE INDEX idx_products_category ON products(category);
CREATE INDEX idx_products_brand ON products(brand);
CREATE INDEX idx_products_is_active ON products(is_active);
CREATE INDEX idx_products_created_at ON products(created_at DESC);

-- Create indexes for inventory
CREATE INDEX idx_inventory_product_id ON inventory(product_id);
CREATE INDEX idx_inventory_warehouse_id ON inventory(warehouse_id);
CREATE INDEX idx_inventory_quantity_available ON inventory(quantity_available);
CREATE INDEX idx_inventory_reorder_level ON inventory(reorder_level);

-- Create unique constraint for product-warehouse combination
CREATE UNIQUE INDEX idx_inventory_product_warehouse ON inventory(product_id, warehouse_id);

-- Create indexes for inventory movements
CREATE INDEX idx_inventory_movements_product_id ON inventory_movements(product_id);
CREATE INDEX idx_inventory_movements_type ON inventory_movements(movement_type);
CREATE INDEX idx_inventory_movements_reference ON inventory_movements(reference_id, reference_type);
CREATE INDEX idx_inventory_movements_created_at ON inventory_movements(created_at DESC);

-- Create indexes for categories
CREATE INDEX idx_categories_parent ON product_categories(parent_category_id);
CREATE INDEX idx_categories_name ON product_categories(name);

-- Create trigger function for updated_at
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create triggers for updated_at
CREATE TRIGGER update_products_updated_at
    BEFORE UPDATE ON products
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_inventory_updated_at
    BEFORE UPDATE ON inventory
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
