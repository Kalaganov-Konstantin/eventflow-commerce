from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI
from prometheus_client import make_asgi_app

from .config.config import load_config
from .consumer import create_consumer
from .senders.email import EmailSender
from .storage.notifications import create_pool


@asynccontextmanager
async def lifespan(app: FastAPI):
    config = load_config()
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

metrics_app = make_asgi_app()
app.mount("/metrics", metrics_app)


@app.get("/health")
async def health_check():
    return {"status": "healthy"}


@app.get("/")
async def root():
    return {"service": "notification", "version": "1.0.0"}


if __name__ == "__main__":
    config = load_config()
    print(f"Starting Notification service on {config.server.host}:{config.server.port}")
    uvicorn.run(app, host=config.server.host, port=config.server.port)
