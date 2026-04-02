import uuid
from typing import Annotated

from fastapi import APIRouter, Header, HTTPException, Request

from notification.storage import notifications as storage
from notification.storage.notifications import Notification

router = APIRouter(prefix="/api/v1/notifications", tags=["notifications"])


@router.get("")
async def list_notifications(request: Request, x_user_id: Annotated[str, Header()]):
    recipient_id = _parse_uuid(x_user_id, "X-User-ID")
    notifications = await storage.list_by_recipient(request.app.state.pool, recipient_id)
    return [_to_response(n) for n in notifications]


@router.get("/{notification_id}")
async def get_notification(notification_id: uuid.UUID, request: Request):
    notification = await storage.get_notification(request.app.state.pool, notification_id)
    if notification is None:
        raise HTTPException(status_code=404, detail="notification not found")
    return _to_response(notification)


def _parse_uuid(value: str, field: str) -> uuid.UUID:
    try:
        return uuid.UUID(value)
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=f"{field} must be a valid uuid") from exc


def _to_response(notification: Notification) -> dict:
    return {
        "id": str(notification.id),
        "recipient_id": str(notification.recipient_id),
        "recipient_address": notification.recipient_address,
        "type": notification.type,
        "subject": notification.subject,
        "body": notification.body,
        "status": notification.status,
        "reference_id": str(notification.reference_id) if notification.reference_id else None,
        "reference_type": notification.reference_type,
        "retry_count": notification.retry_count,
        "error_message": notification.error_message,
        "created_at": _isoformat(notification.created_at),
        "sent_at": _isoformat(notification.sent_at),
        "failed_at": _isoformat(notification.failed_at),
    }


def _isoformat(value) -> str | None:
    return value.isoformat() if value is not None else None
