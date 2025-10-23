ALTER TABLE orders DROP CONSTRAINT orders_status_check;

ALTER TABLE orders ADD CONSTRAINT orders_status_check
    CHECK (status IN ('pending', 'pending_payment', 'payment_failed', 'confirmed', 'processing', 'shipped', 'delivered', 'cancelled'));
