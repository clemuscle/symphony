"""{{SERVICE_NAME}} — worker Python : une tâche répétée en arrière-plan, et une
petite API pour le healthcheck et les métriques"""
import asyncio
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
# Intervalle entre deux passages, en secondes
INTERVAL = float(os.getenv("WORKER_INTERVAL", "{{params.interval}}"))

state = {"runs": 0, "errors": 0}


def work():
    """Un passage du worker : à remplacer par le vrai travail"""
    state["runs"] += 1
    logger.info(f"passage {state['runs']}")


async def loop():
    while True:
        try:
            work()
        except Exception:  # un passage raté ne doit pas arrêter le worker
            state["errors"] += 1
            logger.exception("passage en erreur")
        await asyncio.sleep(INTERVAL)


@asynccontextmanager
async def lifespan(app: FastAPI):
    logger.info(f"Starting {SERVICE_NAME} v{VERSION}, toutes les {INTERVAL} s")
    task = asyncio.create_task(loop())
    yield
    task.cancel()
    logger.info(f"Shutting down {SERVICE_NAME}")


app = FastAPI(title=SERVICE_NAME, version=VERSION, lifespan=lifespan)


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
        "# HELP worker_runs_total Passages du worker\n"
        "# TYPE worker_runs_total counter\n"
        f"worker_runs_total {state['runs']}\n"
        "# HELP worker_errors_total Passages en erreur\n"
        "# TYPE worker_errors_total counter\n"
        f"worker_errors_total {state['errors']}\n"
    )


@app.get("/")
async def root():
    return {"service": SERVICE_NAME, "status": "running", "version": VERSION, "runs": state["runs"]}
