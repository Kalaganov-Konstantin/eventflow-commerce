from yoyo import step

steps = [
    step(
        """
        INSERT INTO notification_templates (name, type, subject_template, body_template)
        VALUES (
            'order_confirmed',
            'email',
            'Order {{ order_id }} confirmed',
            'Your order {{ order_id }} for {{ total_amount }} {{ currency }} is confirmed.'
        ), (
            'order_cancelled',
            'email',
            'Order {{ order_id }} cancelled',
            'Your order {{ order_id }} has been cancelled.'
        ), (
            'payment_failed',
            'email',
            'Payment failed for order {{ order_id }}',
            'Payment for order {{ order_id }} failed: {{ reason }}.'
        )
        """,
        """
        DELETE FROM notification_templates
        WHERE name IN ('order_confirmed', 'order_cancelled', 'payment_failed')
        """,
    )
]
