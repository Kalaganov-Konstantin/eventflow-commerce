import logging

from notification.storage.notifications import Notification

logger = logging.getLogger(__name__)


class SMSSender:
    """No sms provider is configured; delivery is logged and reported as sent."""

    async def send(self, notification: Notification) -> None:
        logger.info(
            "sms delivery (stub)",
            extra={
                "notification_id": str(notification.id),
                "recipient": notification.recipient_address,
            },
        )
