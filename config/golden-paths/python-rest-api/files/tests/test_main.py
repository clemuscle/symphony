"""Tests pour {{.ServiceName}}"""
from fastapi.testclient import TestClient
from main import app

client = TestClient(app)

def test_healthcheck():
    r = client.get("/healthz")
    assert r.status_code == 200
    assert r.json()["status"] == "ok"

def test_root():
    r = client.get("/")
    assert r.status_code == 200
    assert r.json()["status"] == "running"

def test_metrics():
    r = client.get("/metrics")
    assert r.status_code == 200
    assert "http_requests_total" in r.text
