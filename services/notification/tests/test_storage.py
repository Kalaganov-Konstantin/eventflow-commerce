import uuid
from datetime import UTC, datetime

import pytest

from notification.storage import notifications as storage


class FakeConnection:
    def __init__(self, fetchrow_result=None, fetch_result=None, fetchval_result=None):
        self.fetchrow_result = fetchrow_result
        self.fetch_result = fetch_result if fetch_result is not None else []
        self.fetchval_result = fetchval_result
        self.calls = []

    async def fetchrow(self, query, *args):
        self.calls.append(("fetchrow", query, args))
        return self.fetchrow_result

    async def fetch(self, query, *args):
        self.calls.append(("fetch", query, args))
        return self.fetch_result

    async def execute(self, query, *args):
        self.calls.append(("execute", query, args))
        return "OK"

    async def fetchval(self, query, *args):
        self.calls.append(("fetchval", query, args))
        return self.fetchval_result


class FakeAcquire:
    def __init__(self, connection):
        self._connection = connection

    async def __aenter__(self):
        return self._connection

    async def __aexit__(self, exc_type, exc, tb):
        return False


class FakePool:
    def __init__(self, connection):
        self._connection = connection

    def acquire(self):
        return FakeAcquire(self._connection)


def make_row(**overrides):
    row = {
        "id": uuid.uuid4(),
        "recipient_id": uuid.uuid4(),
        "recipient_address": "buyer@example.com",
        "type": "email",
        "template_id": None,
        "subject": "Order confirmed",
        "body": "Thanks for your order!",
        "status": "pending",
        "reference_id": uuid.uuid4(),
        "reference_type": "order",
        "retry_count": 0,
        "error_message": None,
        "created_at": datetime.now(UTC),
        "sent_at": None,
        "failed_at": None,
    }
    row.update(overrides)
    return row


@pytest.mark.asyncio
async def test_create_notification_inserts_and_returns_it():
    row = make_row()
    conn = FakeConnection(fetchrow_result=row)
    pool = FakePool(conn)

    result = await storage.create_notification(
        pool,
        recipient_id=row["recipient_id"],
        recipient_address=row["recipient_address"],
        notification_type=row["type"],
        body=row["body"],
        subject=row["subject"],
        reference_id=row["reference_id"],
        reference_type=row["reference_type"],
    )

    assert result.id == row["id"]
    assert result.status == "pending"
    action, query, _ = conn.calls[0]
    assert action == "fetchrow"
    assert "INSERT INTO notifications" in query


@pytest.mark.asyncio
async def test_mark_sent_updates_status():
    conn = FakeConnection()
    pool = FakePool(conn)
    notification_id = uuid.uuid4()

    await storage.mark_sent(pool, notification_id)

    action, query, args = conn.calls[0]
    assert action == "execute"
    assert "status = 'sent'" in query
    assert args == (notification_id,)


@pytest.mark.asyncio
async def test_mark_failed_updates_status_and_error():
    conn = FakeConnection()
    pool = FakePool(conn)
    notification_id = uuid.uuid4()

    await storage.mark_failed(pool, notification_id, "smtp timeout")

    action, query, args = conn.calls[0]
    assert action == "execute"
    assert "retry_count = retry_count + 1" in query
    assert args == (notification_id, "smtp timeout")


@pytest.mark.asyncio
async def test_record_event_inserts_event():
    conn = FakeConnection()
    pool = FakePool(conn)
    notification_id = uuid.uuid4()

    await storage.record_event(pool, notification_id, "sent", {"provider": "stub"})

    action, query, args = conn.calls[0]
    assert action == "execute"
    assert "INSERT INTO notification_events" in query
    assert args[0] == notification_id
    assert args[1] == "sent"
    assert args[2] == '{"provider": "stub"}'


@pytest.mark.asyncio
async def test_get_notification_returns_none_when_missing():
    conn = FakeConnection(fetchrow_result=None)
    pool = FakePool(conn)

    result = await storage.get_notification(pool, uuid.uuid4())

    assert result is None


@pytest.mark.asyncio
async def test_get_notification_maps_row():
    row = make_row()
    conn = FakeConnection(fetchrow_result=row)
    pool = FakePool(conn)

    result = await storage.get_notification(pool, row["id"])

    assert result.id == row["id"]
    assert result.recipient_address == row["recipient_address"]


@pytest.mark.asyncio
async def test_list_by_recipient_maps_rows():
    row = make_row()
    conn = FakeConnection(fetch_result=[row])
    pool = FakePool(conn)

    result = await storage.list_by_recipient(pool, row["recipient_id"])

    assert len(result) == 1
    assert result[0].id == row["id"]


@pytest.mark.asyncio
async def test_was_event_processed_true():
    conn = FakeConnection(fetchval_result=True)
    pool = FakePool(conn)

    assert await storage.was_event_processed(pool, "11111111-1111-1111-1111-111111111111") is True


@pytest.mark.asyncio
async def test_was_event_processed_false():
    conn = FakeConnection(fetchval_result=False)
    pool = FakePool(conn)

    assert await storage.was_event_processed(pool, "11111111-1111-1111-1111-111111111111") is False


@pytest.mark.asyncio
async def test_mark_event_processed_new_event():
    conn = FakeConnection(fetchrow_result={"event_id": "11111111-1111-1111-1111-111111111111"})
    pool = FakePool(conn)

    result = await storage.mark_event_processed(
        pool, "11111111-1111-1111-1111-111111111111", "order.confirmed"
    )

    assert result is True


@pytest.mark.asyncio
async def test_mark_event_processed_duplicate_event():
    conn = FakeConnection(fetchrow_result=None)
    pool = FakePool(conn)

    result = await storage.mark_event_processed(
        pool, "11111111-1111-1111-1111-111111111111", "order.confirmed"
    )

    assert result is False
