import uuid
from datetime import UTC, datetime

import pytest
from fastapi.testclient import TestClient

from notification.main import app
from notification.storage.notifications import Notification


class FakeConnection:
    def __init__(self, fetchval_result=1, raise_error=False):
        self.fetchval_result = fetchval_result
        self.raise_error = raise_error

    async def fetchval(self, query, *args):
        if self.raise_error:
            raise RuntimeError("db down")
        return self.fetchval_result


class FakeAcquire:
    def __init__(self, connection):
        self._connection = connection

    async def __aenter__(self):
        return self._connection

    async def __aexit__(self, exc_type, exc, tb):
        return False


class FakePool:
    def __init__(self, connection=None):
        self._connection = connection or FakeConnection()

    def acquire(self):
        return FakeAcquire(self._connection)


class FakeConsumer:
    def __init__(self, running=True):
        self._running = running

    def is_running(self):
        return self._running


def make_notification(**overrides):
    fields = {
        "id": uuid.uuid4(),
        "recipient_id": uuid.uuid4(),
        "recipient_address": "buyer@example.com",
        "type": "email",
        "template_id": None,
        "subject": "Order confirmed",
        "body": "Body",
        "status": "sent",
        "reference_id": None,
        "reference_type": None,
        "retry_count": 0,
        "error_message": None,
        "created_at": datetime.now(UTC),
        "sent_at": datetime.now(UTC),
        "failed_at": None,
    }
    fields.update(overrides)
    return Notification(**fields)


@pytest.fixture
def client():
    return TestClient(app)


def test_liveness_always_ok(client):
    response = client.get("/health/live")

    assert response.status_code == 200
    assert response.json() == {"status": "alive"}


def test_readiness_ok_when_database_and_kafka_up(client):
    app.state.pool = FakePool()
    app.state.consumer = FakeConsumer(running=True)

    response = client.get("/health/ready")

    assert response.status_code == 200
    assert response.json()["status"] == "ready"


def test_readiness_fails_when_database_down(client):
    app.state.pool = FakePool(FakeConnection(raise_error=True))
    app.state.consumer = FakeConsumer(running=True)

    response = client.get("/health/ready")

    assert response.status_code == 503
    assert response.json()["checks"]["database"] is False


def test_readiness_fails_when_kafka_not_running(client):
    app.state.pool = FakePool()
    app.state.consumer = FakeConsumer(running=False)

    response = client.get("/health/ready")

    assert response.status_code == 503
    assert response.json()["checks"]["kafka"] is False


def test_list_notifications_by_recipient(client, monkeypatch):
    recipient_id = uuid.uuid4()
    notification = make_notification(recipient_id=recipient_id)

    async def fake_list_by_recipient(pool, rid, limit=50):
        assert rid == recipient_id
        return [notification]

    monkeypatch.setattr(
        "notification.api.notifications.storage.list_by_recipient", fake_list_by_recipient
    )
    app.state.pool = FakePool()

    response = client.get("/api/v1/notifications", headers={"X-User-ID": str(recipient_id)})

    assert response.status_code == 200
    body = response.json()
    assert len(body) == 1
    assert body[0]["id"] == str(notification.id)
    assert body[0]["recipient_id"] == str(recipient_id)


def test_list_notifications_rejects_invalid_user_id(client):
    app.state.pool = FakePool()

    response = client.get("/api/v1/notifications", headers={"X-User-ID": "not-a-uuid"})

    assert response.status_code == 400


def test_get_notification_by_id(client, monkeypatch):
    notification = make_notification()

    async def fake_get_notification(pool, notification_id):
        assert notification_id == notification.id
        return notification

    monkeypatch.setattr(
        "notification.api.notifications.storage.get_notification", fake_get_notification
    )
    app.state.pool = FakePool()

    response = client.get(f"/api/v1/notifications/{notification.id}")

    assert response.status_code == 200
    assert response.json()["id"] == str(notification.id)


def test_get_notification_not_found(client, monkeypatch):
    async def fake_get_notification(pool, notification_id):
        return None

    monkeypatch.setattr(
        "notification.api.notifications.storage.get_notification", fake_get_notification
    )
    app.state.pool = FakePool()

    response = client.get(f"/api/v1/notifications/{uuid.uuid4()}")

    assert response.status_code == 404
