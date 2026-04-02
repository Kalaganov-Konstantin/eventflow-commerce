from email.message import EmailMessage

import aiosmtplib

from notification.config.config import SMTPConfig
from notification.storage.notifications import Notification


class EmailSender:
    def __init__(self, config: SMTPConfig):
        self._config = config

    async def send(self, notification: Notification) -> None:
        message = EmailMessage()
        message["From"] = self._config.from_address
        message["To"] = notification.recipient_address
        message["Subject"] = notification.subject or ""
        message.set_content(notification.body)

        await aiosmtplib.send(
            message,
            hostname=self._config.host,
            port=self._config.port,
            username=self._config.user or None,
            password=self._config.password or None,
            use_tls=self._config.use_tls,
        )
