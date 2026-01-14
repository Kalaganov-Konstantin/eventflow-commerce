-- Tracks the order saga so a failed payment or a later downstream failure knows what to undo:
-- release the stock reservation, refund the payment, or both, in reverse order of the steps that
-- created the order.
CREATE TABLE order_sagas (
    order_id UUID PRIMARY KEY REFERENCES orders(id) ON DELETE CASCADE,
    state VARCHAR(50) NOT NULL CHECK (state IN (
        'started', 'stock_reserved', 'awaiting_payment', 'paid',
        'completed', 'compensating', 'compensated', 'failed'
    )),
    reservation_id UUID,
    payment_id UUID,
    last_error TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_order_sagas_state ON order_sagas(state);

CREATE TRIGGER update_order_sagas_updated_at
    BEFORE UPDATE ON order_sagas
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
