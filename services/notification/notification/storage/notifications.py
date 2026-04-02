import json
import uuid
from dataclasses import dataclass
from datetime import datetime

import asyncpg

_NOTIFICATION_COLUMNS = (
    "id, recipient_id, recipient_address, type, template_id, subject, body, "
    "status, reference_id, reference_type, retry_count, error_message, "
    "created_at, sent_at, failed_at"
)


@dataclass
class Notification:
    id: uuid.UUID
    recipient_id: uuid.UUID
    recipient_address: str
    type: str
    template_id: uuid.UUID | None
    subject: str | None
    body: str
    status: str
    reference_id: uuid.UUID | None
    reference_type: str | None
    retry_count: int
    error_message: str | None
    created_at: datetime
    sent_at: datetime | None
    failed_at: datetime | None


def _to_notification(row: asyncpg.Record) -> Notification:
    return Notification(**dict(row))


async def create_pool(database_url: str) -> asyncpg.Pool:
    return await asyncpg.create_pool(dsn=database_url)


async def create_notification(
    pool: asyncpg.Pool,
    *,
    recipient_id: uuid.UUID,
    recipient_address: str,
    notification_type: str,
    body: str,
    template_id: uuid.UUID | None = None,
    subject: str | None = None,
    reference_id: uuid.UUID | None = None,
    reference_type: str | None = None,
) -> Notification:
    """Insert a pending notification and return the stored row."""
    query = f"""
        INSERT INTO notifications (
            recipient_id, recipient_address, type, template_id, subject, body,
            reference_id, reference_type
        )
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
        RETURNING {_NOTIFICATION_COLUMNS}
    """
    async with pool.acquire() as conn:
        row = await conn.fetchrow(
            query,
            recipient_id,
            recipient_address,
            notification_type,
            template_id,
            subject,
            body,
            reference_id,
            reference_type,
        )
    return _to_notification(row)


async def mark_sent(pool: asyncpg.Pool, notification_id: uuid.UUID) -> None:
    query = "UPDATE notifications SET status = 'sent', sent_at = NOW() WHERE id = $1"
    async with pool.acquire() as conn:
        await conn.execute(query, notification_id)


async def mark_failed(pool: asyncpg.Pool, notification_id: uuid.UUID, error_message: str) -> None:
    """Mark a notification failed and bump its retry count."""
    query = """
        UPDATE notifications
        SET status = 'failed',
            failed_at = NOW(),
            error_message = $2,
            retry_count = retry_count + 1
        WHERE id = $1
    """
    async with pool.acquire() as conn:
        await conn.execute(query, notification_id, error_message)


async def record_event(
    pool: asyncpg.Pool,
    notification_id: uuid.UUID,
    event_type: str,
    event_data: dict | None = None,
) -> None:
    """Append a delivery event for a notification."""
    query = """
        INSERT INTO notification_events (notification_id, event_type, event_data)
        VALUES ($1, $2, $3::jsonb)
    """
    payload = json.dumps(event_data) if event_data is not None else None
    async with pool.acquire() as conn:
        await conn.execute(query, notification_id, event_type, payload)


async def get_notification(pool: asyncpg.Pool, notification_id: uuid.UUID) -> Notification | None:
    query = f"SELECT {_NOTIFICATION_COLUMNS} FROM notifications WHERE id = $1"
    async with pool.acquire() as conn:
        row = await conn.fetchrow(query, notification_id)
    return _to_notification(row) if row else None


async def list_by_recipient(
    pool: asyncpg.Pool, recipient_id: uuid.UUID, limit: int = 50
) -> list[Notification]:
    query = f"""
        SELECT {_NOTIFICATION_COLUMNS} FROM notifications
        WHERE recipient_id = $1
        ORDER BY created_at DESC
        LIMIT $2
    """
    async with pool.acquire() as conn:
        rows = await conn.fetch(query, recipient_id, limit)
    return [_to_notification(row) for row in rows]


async def was_event_processed(pool: asyncpg.Pool, event_id: str) -> bool:
    query = "SELECT EXISTS(SELECT 1 FROM processed_events WHERE event_id = $1)"
    async with pool.acquire() as conn:
        return await conn.fetchval(query, event_id)


async def mark_event_processed(pool: asyncpg.Pool, event_id: str, event_type: str) -> bool:
    """Record event_id as processed, returning False when it was already recorded."""
    query = """
        INSERT INTO processed_events (event_id, event_type)
        VALUES ($1, $2)
        ON CONFLICT (event_id) DO NOTHING
        RETURNING event_id
    """
    async with pool.acquire() as conn:
        row = await conn.fetchrow(query, event_id, event_type)
    return row is not None
