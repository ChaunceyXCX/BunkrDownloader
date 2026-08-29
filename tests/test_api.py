"""Integration tests for the Web server (REST API).

Uses one event loop per test fixture (pytest-asyncio 1.4 + aiohttp pattern).
"""

from __future__ import annotations

import asyncio
from pathlib import Path
from typing import Any
from unittest.mock import MagicMock

import pytest
import pytest_asyncio
from aiohttp.test_utils import TestClient, TestServer

from src.web.database import FILE_STATUS_FAILED, init_db
from src.web.progress_tracker import ProgressTracker
from src.web.server import create_app
from src.web.task_manager import TaskManager


@pytest.fixture
def event_loop():
    """Per-test event loop (so aiohttp can pick it up)."""
    loop = asyncio.new_event_loop()
    yield loop
    loop.close()


@pytest_asyncio.fixture
async def client(tmp_path: Path) -> Any:
    """提供已初始化的 aiohttp TestClient。"""
    db_path = tmp_path / "api.db"
    db = init_db(db_path)
    tracker = ProgressTracker(db)
    tracker.broadcast = MagicMock()
    task_manager = TaskManager(db, tracker)
    await task_manager.initialize()

    static_dir = tmp_path / "static"
    static_dir.mkdir()
    (static_dir / "index.html").write_text("<html></html>")

    app = create_app(db, task_manager, tracker, static_dir=static_dir)
    server = TestServer(app)
    cli = TestClient(server)
    await cli.start_server()
    yield cli
    await cli.close()
    db.close()


# ============== Health & Stats ==============
class TestHealth:
    async def test_health(self, client: TestClient) -> None:
        resp = await client.get("/api/health")
        assert resp.status == 200
        data = await resp.json()
        assert data["status"] == "ok"

    async def test_stats_empty(self, client: TestClient) -> None:
        resp = await client.get("/api/stats")
        data = await resp.json()
        assert data["total_tasks"] == 0
        assert data["running"] == 0


# ============== Tasks ==============
class TestTaskAPI:
    async def test_create_task(self, client: TestClient) -> None:
        resp = await client.post(
            "/api/tasks",
            json={"url": "https://bunkr.si/a/TEST"},
        )
        assert resp.status == 201
        data = await resp.json()
        assert "task_id" in data
        assert data["status"] == "pending"

    async def test_create_task_without_url(self, client: TestClient) -> None:
        resp = await client.post("/api/tasks", json={})
        assert resp.status == 400

    async def test_create_task_with_options(self, client: TestClient) -> None:
        resp = await client.post(
            "/api/tasks",
            json={
                "url": "https://bunkr.si/a/X",
                "options": {
                    "max_retries": 3,
                    "connections": 2,
                    "clean_name": True,
                },
            },
        )
        assert resp.status == 201
        task_id = (await resp.json())["task_id"]

        resp = await client.get(f"/api/tasks/{task_id}")
        data = await resp.json()
        assert data["options"]["max_retries"] == 3
        assert data["options"]["connections"] == 2
        assert data["options"]["clean_name"] is True

    async def test_list_tasks(self, client: TestClient) -> None:
        for i in range(3):
            await client.post("/api/tasks", json={"url": f"https://x/{i}"})
        resp = await client.get("/api/tasks")
        data = await resp.json()
        assert len(data["tasks"]) == 3

    async def test_get_task(self, client: TestClient) -> None:
        resp = await client.post("/api/tasks", json={"url": "https://x"})
        task_id = (await resp.json())["task_id"]
        resp = await client.get(f"/api/tasks/{task_id}")
        assert resp.status == 200
        data = await resp.json()
        assert data["id"] == task_id

    async def test_get_task_not_found(self, client: TestClient) -> None:
        resp = await client.get("/api/tasks/99999")
        assert resp.status == 404

    async def test_start_task(self, client: TestClient) -> None:
        resp = await client.post("/api/tasks", json={"url": "https://x"})
        task_id = (await resp.json())["task_id"]
        resp = await client.post(f"/api/tasks/{task_id}/start")
        # 由于 URL 是假地址，会快速失败但 API 应正常响应
        assert resp.status == 200

    async def test_pause(self, client: TestClient) -> None:
        resp = await client.post("/api/tasks", json={"url": "https://x"})
        task_id = (await resp.json())["task_id"]
        await client.post(f"/api/tasks/{task_id}/pause")
        resp = await client.get(f"/api/tasks/{task_id}")
        assert (await resp.json())["status"] == "paused"

    async def test_cancel(self, client: TestClient) -> None:
        resp = await client.post("/api/tasks", json={"url": "https://x"})
        task_id = (await resp.json())["task_id"]
        resp = await client.post(f"/api/tasks/{task_id}/cancel")
        assert resp.status == 200
        resp = await client.get(f"/api/tasks/{task_id}")
        assert (await resp.json())["status"] == "canceled"

    async def test_delete_task(self, client: TestClient) -> None:
        resp = await client.post("/api/tasks", json={"url": "https://x"})
        task_id = (await resp.json())["task_id"]
        resp = await client.delete(f"/api/tasks/{task_id}")
        assert resp.status == 200
        resp = await client.get(f"/api/tasks/{task_id}")
        assert resp.status == 404

    async def test_retry_failed(self, client: TestClient) -> None:
        resp = await client.post("/api/tasks", json={"url": "https://x"})
        task_id = (await resp.json())["task_id"]
        resp = await client.post(f"/api/tasks/{task_id}/retry")
        assert resp.status == 200


# ============== Files ==============
class TestFileAPI:
    async def test_list_files_empty(self, client: TestClient) -> None:
        resp = await client.post("/api/tasks", json={"url": "https://x"})
        task_id = (await resp.json())["task_id"]
        resp = await client.get(f"/api/tasks/{task_id}/files")
        data = await resp.json()
        assert data["files"] == []

    async def test_list_files_with_data(self, client: TestClient) -> None:
        db_path = client.server.app["db"].db_path
        db = init_db(db_path)
        task_id = db.create_task("https://x")
        db.upsert_file(task_id, "https://file/1", filename="a.jpg")
        db.upsert_file(task_id, "https://file/2", filename="b.jpg")
        db.close()

        resp = await client.get(f"/api/tasks/{task_id}/files")
        data = await resp.json()
        assert len(data["files"]) == 2

    async def test_retry_file(self, client: TestClient) -> None:
        db_path = client.server.app["db"].db_path
        db = init_db(db_path)
        task_id = db.create_task("https://x")
        file_id = db.upsert_file(task_id, "https://file/1")
        db.update_file(file_id, status=FILE_STATUS_FAILED, error_message="err")
        db.close()

        resp = await client.post(f"/api/files/{file_id}/retry")
        assert resp.status == 200


# ============== Events ==============
class TestEventAPI:
    async def test_list_events(self, client: TestClient) -> None:
        resp = await client.post("/api/tasks", json={"url": "https://x"})
        task_id = (await resp.json())["task_id"]
        resp = await client.get(f"/api/tasks/{task_id}/events")
        data = await resp.json()
        assert "events" in data

    async def test_list_all_events(self, client: TestClient) -> None:
        resp = await client.get("/api/events")
        data = await resp.json()
        assert "events" in data


# ============== Static ==============
class TestStatic:
    async def test_index(self, client: TestClient) -> None:
        resp = await client.get("/")
        assert resp.status == 200

    async def test_index_html(self, client: TestClient) -> None:
        resp = await client.get("/index.html")
        assert resp.status == 200
