from types import SimpleNamespace

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient

from notification import main as main_module
from notification.main import app

client = TestClient(app)


def test_health_check_endpoint():
    """
    Tests that the /health endpoint returns a 200 OK status
    and the expected response body.
    """
    response = client.get("/health")
    assert response.status_code == 200
    assert response.json() == {"status": "healthy"}


def test_root_endpoint():
    response = client.get("/")

    assert response.status_code == 200
    assert response.json() == {"service": "notification", "version": "1.0.0"}


@pytest.mark.asyncio
async def test_lifespan_starts_and_stops_dependencies(monkeypatch):
    class FakePool:
        def __init__(self):
            self.closed = False

        async def close(self):
            self.closed = True

    class FakeConsumer:
        def __init__(self):
            self.started = False
            self.stopped = False

        async def start(self):
            self.started = True

        async def stop(self):
            self.stopped = True

    fake_pool = FakePool()
    fake_consumer = FakeConsumer()
    tracing_calls = []

    fake_config = SimpleNamespace(
        otlp_endpoint="http://otel-collector:4318",
        database=SimpleNamespace(url="postgres://example"),
        kafka=object(),
        smtp=object(),
    )

    async def fake_create_pool(url):
        assert url == "postgres://example"
        return fake_pool

    def fake_create_consumer(kafka_config, pool, sender):
        assert pool is fake_pool
        return fake_consumer

    monkeypatch.setattr(main_module, "load_config", lambda: fake_config)
    monkeypatch.setattr(
        main_module, "setup_tracing", lambda app, endpoint: tracing_calls.append(endpoint)
    )
    monkeypatch.setattr(main_module, "create_pool", fake_create_pool)
    monkeypatch.setattr(main_module, "create_consumer", fake_create_consumer)

    test_app = FastAPI()

    async with main_module.lifespan(test_app):
        assert test_app.state.config is fake_config
        assert test_app.state.pool is fake_pool
        assert test_app.state.consumer is fake_consumer
        assert fake_consumer.started is True

    assert tracing_calls == ["http://otel-collector:4318"]
    assert fake_consumer.stopped is True
    assert fake_pool.closed is True
