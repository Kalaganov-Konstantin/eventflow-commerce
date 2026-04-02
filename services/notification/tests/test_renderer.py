import pytest

from notification.templates.renderer import TemplateNotFound, render_template


class FakeConnection:
    def __init__(self, fetchrow_result=None):
        self.fetchrow_result = fetchrow_result
        self.calls = []

    async def fetchrow(self, query, *args):
        self.calls.append((query, args))
        return self.fetchrow_result


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


@pytest.mark.asyncio
async def test_render_template_renders_subject_and_body():
    row = {
        "subject_template": "Order {{ order_id }} confirmed",
        "body_template": "Order {{ order_id }} for {{ total_amount }} {{ currency }} is confirmed.",
    }
    pool = FakePool(FakeConnection(fetchrow_result=row))

    subject, body = await render_template(
        pool,
        "order_confirmed",
        {"order_id": "42", "total_amount": "19.99", "currency": "USD"},
    )

    assert subject == "Order 42 confirmed"
    assert body == "Order 42 for 19.99 USD is confirmed."


@pytest.mark.asyncio
async def test_render_template_escapes_html_in_context():
    row = {
        "subject_template": None,
        "body_template": "Reason: {{ reason }}",
    }
    pool = FakePool(FakeConnection(fetchrow_result=row))

    subject, body = await render_template(
        pool, "payment_failed", {"reason": "<script>alert(1)</script>"}
    )

    assert subject is None
    assert body == "Reason: &lt;script&gt;alert(1)&lt;/script&gt;"


@pytest.mark.asyncio
async def test_render_template_raises_when_missing():
    pool = FakePool(FakeConnection(fetchrow_result=None))

    with pytest.raises(TemplateNotFound):
        await render_template(pool, "unknown", {})
