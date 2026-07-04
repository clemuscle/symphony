"""{{.ServiceName}} — API REST Python"""
import logging, os
from fastapi import FastAPI
from fastapi.responses import PlainTextResponse

logging.basicConfig(level=logging.INFO, format='{"time": "%(asctime)s", "msg": "%(message)s"}')
logger = logging.getLogger(__name__)

app = FastAPI(title="{{.ServiceName}}", version=os.getenv("VERSION", "dev"))

@app.get("/healthz")
async def healthz():
    return {"status": "ok", "service": "{{.ServiceName}}"}

@app.get("/metrics", response_class=PlainTextResponse)
async def metrics():
    return "# HELP http_requests_total Total requests\nhttp_requests_total 0\n"

@app.get("/")
async def root():
    logger.info("root called")
    return {"service": "{{.ServiceName}}", "status": "running"}
