import os
import uuid

import asyncpg
import pytest_asyncio
from aiokafka import AIOKafkaProducer
from aiokafka.admin import AIOKafkaAdminClient, NewTopic
from aiokafka.errors import TopicAlreadyExistsError

from notification.config.config import KafkaConfig
from notification.consumer import ORDERS_TOPIC, PAYMENTS_TOPIC, create_consumer

TEST_DATABASE_URL = os.environ.get(
    "NOTIFICATION_TEST_DATABASE_URL",
    "postgresql://notifications_user:notifications_pass@localhost:5433/notifications",
)
TEST_KAFKA_BROKER = os.environ.get("TEST_KAFKA_BROKER", "localhost:9093")

# A brand new consumer group starts reading from a topic's current tail, so tests give the
# consumer this long to join its group before publishing the message it is meant to observe.
CONSUMER_JOIN_DELAY_SECONDS = 2


class FakeSender:
    """A sender that never fails, so tests only exercise the consumer's own logic."""

    async def send(self, notification) -> None:
        return None


async def _ensure_topics_exist() -> None:
    admin = AIOKafkaAdminClient(bootstrap_servers=TEST_KAFKA_BROKER)
    await admin.start()
    try:
        for topic in (ORDERS_TOPIC, PAYMENTS_TOPIC):
            try:
                await admin.create_topics(
                    [NewTopic(name=topic, num_partitions=1, replication_factor=1)]
                )
            except TopicAlreadyExistsError:
                pass
    finally:
        await admin.close()


@pytest_asyncio.fixture
async def pool():
    """A real asyncpg pool against the integration test database."""
    db_pool = await asyncpg.create_pool(dsn=TEST_DATABASE_URL)
    try:
        yield db_pool
    finally:
        await db_pool.close()


@pytest_asyncio.fixture
async def kafka_producer():
    """A real Kafka producer against the integration test broker."""
    await _ensure_topics_exist()
    producer = AIOKafkaProducer(bootstrap_servers=TEST_KAFKA_BROKER)
    await producer.start()
    try:
        yield producer
    finally:
        await producer.stop()


@pytest_asyncio.fixture
async def consumer(pool):
    """A real Consumer wired to the integration test broker and database, running in the
    background for the duration of the test."""
    await _ensure_topics_exist()
    config = KafkaConfig(brokers=TEST_KAFKA_BROKER, group_id=f"notification-test-{uuid.uuid4()}")
    instance = create_consumer(config, pool, FakeSender())
    await instance.start()
    try:
        yield instance
    finally:
        await instance.stop()
