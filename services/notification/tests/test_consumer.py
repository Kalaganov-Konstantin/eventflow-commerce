import asyncio
import json
import uuid

import pytest

from notification import consumer as consumer_module
from notification.storage.notifications import Notification


def make_event(event_type, data, event_id="evt-1"):
    return {"id": event_id, "type": event_type, "data": data}


def make_notification(**overrides):
    fields = {
        "id": uuid.uuid4(),
        "recipient_id": uuid.uuid4(),
        "recipient_address": "customer@customers.eventflow.local",
        "type": "email",
        "template_id": None,
        "subject": "Order confirmed",
        "body": "Body",
        "status": "pending",
        "reference_id": None,
        "reference_type": None,
        "retry_count": 0,
        "error_message": None,
        "created_at": None,
        "sent_at": None,
        "failed_at": None,
    }
    fields.update(overrides)
    return Notification(**fields)


class FakeSender:
    async def send(self, notification):
        return None


class FakeConsumerRecord:
    def __init__(self, topic, partition, offset, value):
        self.topic = topic
        self.partition = partition
        self.offset = offset
        self.value = value


class FakeKafkaConsumer:
    def __init__(self):
        self.started = False
        self.stopped = False
        self.commits = []

    async def start(self):
        self.started = True

    async def stop(self):
        self.stopped = True

    async def commit(self, offsets):
        self.commits.append(offsets)


class FakeKafkaProducer:
    def __init__(self):
        self.started = False
        self.stopped = False
        self.sent = []

    async def start(self):
        self.started = True

    async def stop(self):
        self.stopped = True

    async def send_and_wait(self, topic, value):
        self.sent.append((topic, value))


@pytest.mark.asyncio
async def test_handle_event_skips_unrelated_event_type(monkeypatch):
    calls = {"was_processed": 0}

    async def fake_was_processed(pool, event_id):
        calls["was_processed"] += 1
        return False

    monkeypatch.setattr(consumer_module.storage, "was_event_processed", fake_was_processed)

    event = make_event("order.created", {})
    await consumer_module.handle_event(pool=object(), sender=object(), event=event)

    assert calls["was_processed"] == 0


@pytest.mark.asyncio
async def test_handle_event_skips_already_processed_event(monkeypatch):
    async def fake_was_processed(pool, event_id):
        return True

    rendered = {"called": False}

    async def fake_render(pool, name, context):
        rendered["called"] = True
        return "subject", "body"

    monkeypatch.setattr(consumer_module.storage, "was_event_processed", fake_was_processed)
    monkeypatch.setattr(consumer_module, "render_template", fake_render)

    event = make_event(
        "order.confirmed",
        {"customer_id": str(uuid.uuid4()), "order_id": str(uuid.uuid4())},
    )
    await consumer_module.handle_event(pool=object(), sender=object(), event=event)

    assert rendered["called"] is False


@pytest.mark.asyncio
async def test_handle_event_creates_and_delivers_order_confirmed(monkeypatch):
    customer_id = uuid.uuid4()
    order_id = uuid.uuid4()
    created_kwargs = {}
    delivered = {}
    marked_processed = {}
    notification = make_notification(recipient_id=customer_id, reference_id=order_id)

    async def fake_was_processed(pool, event_id):
        return False

    async def fake_render(pool, name, context):
        assert name == "order_confirmed"
        assert context == {
            "order_id": str(order_id),
            "total_amount": "19.99",
            "currency": "USD",
        }
        return "Order confirmed", "Body"

    async def fake_create_notification(pool, **kwargs):
        created_kwargs.update(kwargs)
        return notification

    async def fake_deliver(sender, pool, notif):
        delivered["notification"] = notif
        return True

    async def fake_mark_event_processed(pool, event_id, event_type):
        marked_processed["event_id"] = event_id
        marked_processed["event_type"] = event_type
        return True

    monkeypatch.setattr(consumer_module.storage, "was_event_processed", fake_was_processed)
    monkeypatch.setattr(consumer_module, "render_template", fake_render)
    monkeypatch.setattr(consumer_module.storage, "create_notification", fake_create_notification)
    monkeypatch.setattr(consumer_module, "deliver", fake_deliver)
    monkeypatch.setattr(consumer_module.storage, "mark_event_processed", fake_mark_event_processed)

    event = make_event(
        "order.confirmed",
        {
            "order_id": str(order_id),
            "customer_id": str(customer_id),
            "total_amount_cents": 1999,
            "currency": "USD",
        },
        event_id="evt-42",
    )

    await consumer_module.handle_event(pool=object(), sender=object(), event=event)

    assert created_kwargs["recipient_id"] == customer_id
    assert created_kwargs["reference_id"] == order_id
    assert created_kwargs["reference_type"] == "order"
    assert created_kwargs["notification_type"] == "email"
    assert delivered["notification"] is notification
    assert marked_processed == {"event_id": "evt-42", "event_type": "order.confirmed"}


@pytest.mark.asyncio
async def test_handle_event_order_cancelled_context(monkeypatch):
    order_id = uuid.uuid4()
    seen_context = {}

    async def fake_was_processed(pool, event_id):
        return False

    async def fake_render(pool, name, context):
        seen_context.update(context)
        return None, "Body"

    async def fake_create_notification(pool, **kwargs):
        return make_notification()

    async def fake_deliver(sender, pool, notif):
        return True

    async def fake_mark_event_processed(pool, event_id, event_type):
        return True

    monkeypatch.setattr(consumer_module.storage, "was_event_processed", fake_was_processed)
    monkeypatch.setattr(consumer_module, "render_template", fake_render)
    monkeypatch.setattr(consumer_module.storage, "create_notification", fake_create_notification)
    monkeypatch.setattr(consumer_module, "deliver", fake_deliver)
    monkeypatch.setattr(consumer_module.storage, "mark_event_processed", fake_mark_event_processed)

    event = make_event(
        "order.cancelled",
        {"order_id": str(order_id), "customer_id": str(uuid.uuid4())},
    )
    await consumer_module.handle_event(pool=object(), sender=object(), event=event)

    assert seen_context == {"order_id": str(order_id)}


@pytest.mark.asyncio
async def test_handle_event_payment_failed_context(monkeypatch):
    order_id = uuid.uuid4()
    seen_context = {}

    async def fake_was_processed(pool, event_id):
        return False

    async def fake_render(pool, name, context):
        assert name == "payment_failed"
        seen_context.update(context)
        return "subject", "body"

    async def fake_create_notification(pool, **kwargs):
        return make_notification()

    async def fake_deliver(sender, pool, notif):
        return True

    async def fake_mark_event_processed(pool, event_id, event_type):
        return True

    monkeypatch.setattr(consumer_module.storage, "was_event_processed", fake_was_processed)
    monkeypatch.setattr(consumer_module, "render_template", fake_render)
    monkeypatch.setattr(consumer_module.storage, "create_notification", fake_create_notification)
    monkeypatch.setattr(consumer_module, "deliver", fake_deliver)
    monkeypatch.setattr(consumer_module.storage, "mark_event_processed", fake_mark_event_processed)

    event = make_event(
        "payment.failed",
        {
            "order_id": str(order_id),
            "customer_id": str(uuid.uuid4()),
            "reason": "card declined",
        },
    )
    await consumer_module.handle_event(pool=object(), sender=object(), event=event)

    assert seen_context == {"order_id": str(order_id), "reason": "card declined"}


@pytest.mark.asyncio
async def test_process_commits_without_dlq_on_success(monkeypatch):
    async def fake_handle_event(pool, sender, event):
        return None

    monkeypatch.setattr(consumer_module, "handle_event", fake_handle_event)

    kafka_consumer = FakeKafkaConsumer()
    kafka_producer = FakeKafkaProducer()
    instance = consumer_module.Consumer(kafka_consumer, kafka_producer, object(), FakeSender())

    payload = json.dumps({"id": "evt-1", "type": "order.confirmed", "data": {}}).encode()
    message = FakeConsumerRecord("orders.events", 0, 7, payload)
    await instance._process(message)

    assert kafka_producer.sent == []
    assert kafka_consumer.commits == [{consumer_module.TopicPartition("orders.events", 0): 8}]


@pytest.mark.asyncio
async def test_process_sends_to_dlq_after_max_attempts(monkeypatch):
    attempts = {"count": 0}

    async def fake_handle_event(pool, sender, event):
        attempts["count"] += 1
        raise RuntimeError("boom")

    monkeypatch.setattr(consumer_module, "handle_event", fake_handle_event)

    kafka_consumer = FakeKafkaConsumer()
    kafka_producer = FakeKafkaProducer()
    instance = consumer_module.Consumer(kafka_consumer, kafka_producer, object(), FakeSender())

    payload = json.dumps({"id": "evt-2", "type": "order.confirmed", "data": {}}).encode()
    message = FakeConsumerRecord("orders.events", 0, 9, payload)
    await instance._process(message)

    assert attempts["count"] == consumer_module.MAX_ATTEMPTS
    assert kafka_producer.sent == [("orders.events.dlq", payload)]
    assert kafka_consumer.commits == [{consumer_module.TopicPartition("orders.events", 0): 10}]


@pytest.mark.asyncio
async def test_process_sends_undecodable_message_to_dlq(monkeypatch):
    handled = {"called": False}

    async def fake_handle_event(pool, sender, event):
        handled["called"] = True

    monkeypatch.setattr(consumer_module, "handle_event", fake_handle_event)

    kafka_consumer = FakeKafkaConsumer()
    kafka_producer = FakeKafkaProducer()
    instance = consumer_module.Consumer(kafka_consumer, kafka_producer, object(), FakeSender())

    payload = b"not json"
    message = FakeConsumerRecord("payments.events", 1, 3, payload)
    await instance._process(message)

    assert handled["called"] is False
    assert kafka_producer.sent == [("payments.events.dlq", payload)]
    assert kafka_consumer.commits == [{consumer_module.TopicPartition("payments.events", 1): 4}]


@pytest.mark.asyncio
async def test_start_and_stop_manage_kafka_clients():
    kafka_consumer = FakeKafkaConsumer()
    kafka_producer = FakeKafkaProducer()
    instance = consumer_module.Consumer(kafka_consumer, kafka_producer, object(), FakeSender())

    async def noop_run():
        await asyncio.sleep(3600)

    instance._run = noop_run

    await instance.start()
    assert kafka_consumer.started is True
    assert kafka_producer.started is True

    await instance.stop()
    assert kafka_consumer.stopped is True
    assert kafka_producer.stopped is True
