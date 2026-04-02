import asyncio
import contextlib
import json
import logging
import uuid

from aiokafka import AIOKafkaConsumer, AIOKafkaProducer, TopicPartition

from notification.config.config import KafkaConfig
from notification.senders.base import Sender, deliver
from notification.storage import notifications as storage
from notification.templates.renderer import render_template

logger = logging.getLogger(__name__)

ORDERS_TOPIC = "orders.events"
PAYMENTS_TOPIC = "payments.events"

MAX_ATTEMPTS = 3

_TEMPLATE_BY_EVENT_TYPE = {
    "order.confirmed": "order_confirmed",
    "order.cancelled": "order_cancelled",
    "payment.failed": "payment_failed",
}


async def handle_event(pool, sender: Sender, event: dict) -> None:
    """Turn a relevant order or payment event into a stored, sent notification."""
    event_type = event.get("type")
    template_name = _TEMPLATE_BY_EVENT_TYPE.get(event_type)
    if template_name is None:
        return

    event_id = event.get("id", "")
    if await storage.was_event_processed(pool, event_id):
        return

    data = event.get("data") or {}
    context = _context_from_data(template_name, data)
    subject, body = await render_template(pool, template_name, context)

    notification = await storage.create_notification(
        pool,
        recipient_id=uuid.UUID(data["customer_id"]),
        recipient_address=_placeholder_address(data["customer_id"]),
        notification_type="email",
        body=body,
        subject=subject,
        reference_id=uuid.UUID(data["order_id"]) if data.get("order_id") else None,
        reference_type="order",
    )

    await deliver(sender, pool, notification)
    await storage.mark_event_processed(pool, event_id, event_type)


def _context_from_data(template_name: str, data: dict) -> dict:
    context = {"order_id": data.get("order_id", "")}
    if template_name == "order_confirmed":
        context["total_amount"] = _format_cents(data.get("total_amount_cents", 0))
        context["currency"] = data.get("currency", "")
    elif template_name == "payment_failed":
        context["reason"] = data.get("reason", "")
    return context


def _format_cents(cents) -> str:
    return f"{cents / 100:.2f}"


def _placeholder_address(customer_id: str) -> str:
    # No user/profile service exists yet to resolve a real contact address for customer_id.
    return f"{customer_id}@customers.eventflow.local"


def build_kafka_consumer(config: KafkaConfig) -> AIOKafkaConsumer:
    return AIOKafkaConsumer(
        ORDERS_TOPIC,
        PAYMENTS_TOPIC,
        bootstrap_servers=config.brokers,
        group_id=config.group_id,
        enable_auto_commit=False,
    )


def build_kafka_producer(config: KafkaConfig) -> AIOKafkaProducer:
    return AIOKafkaProducer(bootstrap_servers=config.brokers)


class Consumer:
    """Consumes orders.events and payments.events, retrying failures before routing to a DLQ."""

    def __init__(self, consumer, producer, pool, sender: Sender):
        self._consumer = consumer
        self._producer = producer
        self._pool = pool
        self._sender = sender
        self._task: asyncio.Task | None = None

    async def start(self) -> None:
        await self._consumer.start()
        await self._producer.start()
        self._task = asyncio.create_task(self._run())

    async def stop(self) -> None:
        if self._task is not None:
            self._task.cancel()
            with contextlib.suppress(asyncio.CancelledError):
                await self._task
        await self._producer.stop()
        await self._consumer.stop()

    async def _run(self) -> None:
        async for message in self._consumer:
            await self._process(message)

    async def _process(self, message) -> None:
        try:
            event = json.loads(message.value)
        except (json.JSONDecodeError, UnicodeDecodeError, TypeError):
            logger.exception("failed to decode kafka message from %s", message.topic)
            await self._send_to_dlq(message)
            await self._commit(message)
            return

        for attempt in range(1, MAX_ATTEMPTS + 1):
            try:
                await handle_event(self._pool, self._sender, event)
                break
            except Exception:
                logger.exception(
                    "failed to handle event %s (attempt %s/%s)",
                    event.get("id"),
                    attempt,
                    MAX_ATTEMPTS,
                )
                if attempt == MAX_ATTEMPTS:
                    await self._send_to_dlq(message)
        await self._commit(message)

    async def _send_to_dlq(self, message) -> None:
        await self._producer.send_and_wait(f"{message.topic}.dlq", message.value)

    async def _commit(self, message) -> None:
        partition = TopicPartition(message.topic, message.partition)
        await self._consumer.commit({partition: message.offset + 1})


def create_consumer(config: KafkaConfig, pool, sender: Sender) -> Consumer:
    return Consumer(build_kafka_consumer(config), build_kafka_producer(config), pool, sender)
