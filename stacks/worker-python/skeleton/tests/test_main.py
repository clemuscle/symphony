"""Tests pour {{SERVICE_NAME}}"""
import pytest
from fastapi.testclient import TestClient

from main import app

client = TestClient(app)


def test_healthcheck():
    """Le healthcheck doit retourner 200 avec status ok"""
    response = client.get("/healthz")
    assert response.status_code == 200
    data = response.json()
    assert data["status"] == "ok"
    assert "service" in data
    assert "version" in data


def test_root():
    """La route racine doit retourner le nom du service"""
    response = client.get("/")
    assert response.status_code == 200
    data = response.json()
    assert data["status"] == "running"


def test_metrics():
    """Les métriques Prometheus doivent être exposées"""
    response = client.get("/metrics")
    assert response.status_code == 200
    assert "worker_runs_total" in response.text


def test_work():
    """Un passage du worker est compté"""
    from main import state, work
    before = state["runs"]
    work()
    assert state["runs"] == before + 1

