import asyncio
import json
import uuid

import pytest

from notification.consumer import ORDERS_TOPIC

from .conftest import CONSUMER_JOIN_DELAY_SECONDS


def _order_confirmed_event(order_id: uuid.UUID, customer_id: uuid.UUID, event_id: str) -> dict:
    return {
        "id": event_id,
        "type": "order.confirmed",
        "source": "order-service",
        "data": {
            "order_id": str(order_id),
            "customer_id": str(customer_id),
            "total_amount_cents": 4999,
            "currency": "USD",
        },
    }


async def _notifications_for_order(pool, order_id: uuid.UUID):
    async with pool.acquire() as conn:
        return await conn.fetch(
            "SELECT id, status, subject, body FROM notifications WHERE reference_id = $1",
            order_id,
        )


async def _wait_for_notifications(pool, order_id: uuid.UUID, count: int, timeout: float = 15.0):
    deadline = asyncio.get_event_loop().time() + timeout
    while True:
        rows = await _notifications_for_order(pool, order_id)
        if len(rows) >= count:
            return rows
        if asyncio.get_event_loop().time() > deadline:
            raise AssertionError(
                f"expected at least {count} notification(s) for order {order_id}, "
                f"found {len(rows)} after {timeout}s"
            )
        await asyncio.sleep(0.2)


@pytest.mark.integration
@pytest.mark.asyncio
async def test_order_confirmed_creates_notification_row(pool, kafka_producer, consumer):
    order_id = uuid.uuid4()
    customer_id = uuid.uuid4()
    event = _order_confirmed_event(order_id, customer_id, event_id=str(uuid.uuid4()))

    await asyncio.sleep(CONSUMER_JOIN_DELAY_SECONDS)
    await kafka_producer.send_and_wait(ORDERS_TOPIC, json.dumps(event).encode())

    rows = await _wait_for_notifications(pool, order_id, count=1)

    assert len(rows) == 1
    assert rows[0]["subject"] == f"Order {order_id} confirmed"
    assert "confirmed" in rows[0]["body"]


@pytest.mark.integration
@pytest.mark.asyncio
async def test_redelivered_event_does_not_duplicate_notification(pool, kafka_producer, consumer):
    order_id = uuid.uuid4()
    customer_id = uuid.uuid4()
    event_id = str(uuid.uuid4())
    event = _order_confirmed_event(order_id, customer_id, event_id=event_id)

    await asyncio.sleep(CONSUMER_JOIN_DELAY_SECONDS)
    await kafka_producer.send_and_wait(ORDERS_TOPIC, json.dumps(event).encode())
    await _wait_for_notifications(pool, order_id, count=1)

    # Redeliver the exact same event id: the consumer must recognize it via processed_events and
    # not create a second notification row.
    await kafka_producer.send_and_wait(ORDERS_TOPIC, json.dumps(event).encode())
    await asyncio.sleep(3)

    rows = await _notifications_for_order(pool, order_id)
    assert len(rows) == 1
