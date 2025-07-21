from fastapi import FastAPI
from prometheus_client import make_asgi_app
import uvicorn

from .config.config import load_config

app = FastAPI(title="Notification Service")

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
