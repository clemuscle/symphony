"""{{SERVICE_NAME}} — API REST Python avec FastAPI"""
import logging
import os
from contextlib import asynccontextmanager

from fastapi import FastAPI
from fastapi.responses import PlainTextResponse
from pydantic import BaseModel

# Logger structuré ; LOG_LEVEL : debug, info, warning ou error
logging.basicConfig(
    level=os.getenv("LOG_LEVEL", "{{params.log_level}}").upper(),
    format='{"time": "%(asctime)s", "level": "%(levelname)s", "msg": "%(message)s"}',
)
logger = logging.getLogger(__name__)

SERVICE_NAME = "{{SERVICE_NAME}}"
VERSION = os.getenv("VERSION", "dev")


@asynccontextmanager
async def lifespan(app: FastAPI):
    logger.info(f"Starting {SERVICE_NAME} v{VERSION}")
    yield
    logger.info(f"Shutting down {SERVICE_NAME}")


app = FastAPI(
    title=SERVICE_NAME,
    version=VERSION,
    lifespan=lifespan,
)


class HealthResponse(BaseModel):
    status: str
    service: str
    version: str


@app.get("/healthz", response_model=HealthResponse)
async def healthcheck():
    return HealthResponse(status="ok", service=SERVICE_NAME, version=VERSION)


@app.get("/metrics", response_class=PlainTextResponse)
async def metrics():
    """Endpoint Prometheus"""
    return (
        "# HELP http_requests_total Total HTTP requests\n"
        "# TYPE http_requests_total counter\n"
        "http_requests_total 0\n"
    )


@app.get("/")
async def root():
    logger.info("root endpoint called")
    return {"service": SERVICE_NAME, "status": "running", "version": VERSION}

