import uuid
from datetime import UTC, datetime

import pytest

from notification.senders.base import deliver
from notification.senders.email import EmailSender
from notification.senders.sms import SMSSender
from notification.storage import notifications as storage
from notification.storage.notifications import Notification


def make_notification(**overrides):
    fields = {
        "id": uuid.uuid4(),
        "recipient_id": uuid.uuid4(),
        "recipient_address": "buyer@example.com",
        "type": "email",
        "template_id": None,
        "subject": "Order confirmed",
        "body": "Thanks for your order!",
        "status": "pending",
        "reference_id": None,
        "reference_type": None,
        "retry_count": 0,
        "error_message": None,
        "created_at": datetime.now(UTC),
        "sent_at": None,
        "failed_at": None,
    }
    fields.update(overrides)
    return Notification(**fields)


class FakeSMTPConfig:
    host = "smtp.example.com"
    port = 587
    user = "smtp-user"
    password = "smtp-pass"
    from_address = "notifications@eventflow.commerce"
    use_tls = True


class SucceedingSender:
    async def send(self, notification):
        return None


class FailingSender:
    async def send(self, notification):
        raise RuntimeError("smtp timeout")


@pytest.mark.asyncio
async def test_email_sender_sends_via_aiosmtplib(monkeypatch):
    calls = []

    async def fake_send(message, **kwargs):
        calls.append((message, kwargs))

    monkeypatch.setattr("notification.senders.email.aiosmtplib.send", fake_send)

    notification = make_notification()
    sender = EmailSender(FakeSMTPConfig())

    await sender.send(notification)

    assert len(calls) == 1
    message, kwargs = calls[0]
    assert message["To"] == notification.recipient_address
    assert message["From"] == "notifications@eventflow.commerce"
    assert message["Subject"] == notification.subject
    assert message.get_content().strip() == notification.body
    assert kwargs["hostname"] == "smtp.example.com"
    assert kwargs["port"] == 587
    assert kwargs["use_tls"] is True


@pytest.mark.asyncio
async def test_sms_sender_logs_and_succeeds(caplog):
    notification = make_notification(type="sms", recipient_address="+15551234567")
    sender = SMSSender()

    with caplog.at_level("INFO"):
        await sender.send(notification)

    assert "sms delivery" in caplog.text


@pytest.mark.asyncio
async def test_deliver_marks_notification_sent(monkeypatch):
    marked = {}

    async def fake_mark_sent(pool, notification_id):
        marked["sent_id"] = notification_id

    monkeypatch.setattr(storage, "mark_sent", fake_mark_sent)

    notification = make_notification()
    result = await deliver(SucceedingSender(), pool=object(), notification=notification)

    assert result is True
    assert marked["sent_id"] == notification.id


@pytest.mark.asyncio
async def test_deliver_marks_notification_failed(monkeypatch):
    marked = {}

    async def fake_mark_failed(pool, notification_id, error_message):
        marked["failed_id"] = notification_id
        marked["error_message"] = error_message

    monkeypatch.setattr(storage, "mark_failed", fake_mark_failed)

    notification = make_notification()
    result = await deliver(FailingSender(), pool=object(), notification=notification)

    assert result is False
    assert marked["failed_id"] == notification.id
    assert marked["error_message"] == "smtp timeout"
