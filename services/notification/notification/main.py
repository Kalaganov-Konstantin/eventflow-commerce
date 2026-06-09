from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse
from prometheus_client import make_asgi_app

from .api.notifications import router as notifications_router
from .config.config import load_config
from .consumer import create_consumer
from .logging import configure_logging
from .senders.email import EmailSender
from .storage.notifications import create_pool
from .tracing import setup_tracing

configure_logging()


@asynccontextmanager
async def lifespan(app: FastAPI):
    config = load_config()
    setup_tracing(app, config.otlp_endpoint)
    pool = await create_pool(config.database.url)
    consumer = create_consumer(config.kafka, pool, EmailSender(config.smtp))
    await consumer.start()

    app.state.config = config
    app.state.pool = pool
    app.state.consumer = consumer

    yield

    await consumer.stop()
    await pool.close()


app = FastAPI(title="Notification Service", lifespan=lifespan)
app.include_router(notifications_router)

metrics_app = make_asgi_app()
app.mount("/metrics", metrics_app)


@app.get("/health")
async def health_check():
    return {"status": "healthy"}


@app.get("/health/live")
async def liveness_check():
    return {"status": "alive"}


@app.get("/health/ready")
async def readiness_check(request: Request):
    checks = {"database": await _database_ready(request), "kafka": _kafka_ready(request)}

    if all(checks.values()):
        return {"status": "ready", "checks": checks}
    return JSONResponse(status_code=503, content={"status": "not_ready", "checks": checks})


async def _database_ready(request: Request) -> bool:
    pool = getattr(request.app.state, "pool", None)
    if pool is None:
        return False
    try:
        async with pool.acquire() as conn:
            await conn.fetchval("SELECT 1")
    except Exception:
        return False
    return True


def _kafka_ready(request: Request) -> bool:
    consumer = getattr(request.app.state, "consumer", None)
    return consumer is not None and consumer.is_running()


@app.get("/")
async def root():
    return {"service": "notification", "version": "1.0.0"}


if __name__ == "__main__":
    config = load_config()
    print(f"Starting Notification service on {config.server.host}:{config.server.port}")
    # log_config=None keeps configure_logging()'s formatter instead of uvicorn's default one.
    uvicorn.run(app, host=config.server.host, port=config.server.port, log_config=None)
