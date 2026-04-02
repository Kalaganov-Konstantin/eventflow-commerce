from typing import Protocol

import asyncpg

from notification.storage import notifications as storage
from notification.storage.notifications import Notification


class Sender(Protocol):
    async def send(self, notification: Notification) -> None: ...


async def deliver(sender: Sender, pool: asyncpg.Pool, notification: Notification) -> bool:
    """Send notification through sender and store the resulting status, reporting success."""
    try:
        await sender.send(notification)
    except Exception as exc:
        await storage.mark_failed(pool, notification.id, str(exc))
        return False

    await storage.mark_sent(pool, notification.id)
    return True
