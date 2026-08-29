"""Tests for the SQLite database layer."""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from src.web.database import (
    FILE_STATUS_COMPLETED,
    FILE_STATUS_DOWNLOADING,
    FILE_STATUS_FAILED,
    FILE_STATUS_PENDING,
    TASK_STATUS_PENDING,
    TASK_STATUS_RUNNING,
    Database,
    get_default_db_path,
    init_db,
)


@pytest.fixture
def db_path(tmp_path: Path) -> Path:
    """提供临时数据库路径。"""
    return tmp_path / "test.db"


@pytest.fixture
def db(db_path: Path) -> Database:
    """提供已初始化的 Database 实例。"""
    database = init_db(db_path)
    yield database
    database.close()


# ============== Schema ==============
class TestSchema:
    def test_init_creates_tables(self, db: Database) -> None:
        """Schema 初始化后所有表存在。"""
        with db._lock:  # type: ignore[attr-defined]
            rows = db._conn.execute(  # type: ignore[attr-defined]
                "SELECT name FROM sqlite_master WHERE type='table' ORDER BY name"
            ).fetchall()
        names = {r["name"] for r in rows}
        assert "tasks" in names
        assert "files" in names
        assert "events" in names

    def test_init_idempotent(self, db_path: Path) -> None:
        """多次调用 init_schema 不应出错。"""
        db1 = init_db(db_path)
        db1.init_schema()
        db1.init_schema()
        db1.close()


# ============== Task CRUD ==============
class TestTaskCRUD:
    def test_create_task(self, db: Database) -> None:
        """创建任务返回有效 id。"""
        task_id = db.create_task(
            "https://bunkr.si/a/ABC",
            options={"max_retries": 3},
        )
        assert task_id > 0
        task = db.get_task(task_id)
        assert task is not None
        assert task["url"] == "https://bunkr.si/a/ABC"
        assert task["status"] == TASK_STATUS_PENDING

    def test_options_persisted(self, db: Database) -> None:
        """options JSON 应正确持久化。"""
        opts = {"max_retries": 7, "custom_path": "/tmp/x"}
        task_id = db.create_task("https://x", options=opts)
        task = db.get_task(task_id)
        assert task is not None
        loaded = json.loads(task["options_json"])
        assert loaded["max_retries"] == 7
        assert loaded["custom_path"] == "/tmp/x"

    def test_list_tasks(self, db: Database) -> None:
        """列出任务按时间倒序。"""
        for i in range(3):
            db.create_task(f"https://x/{i}")
        tasks = db.list_tasks()
        assert len(tasks) == 3
        # 倒序：最后创建在最前
        assert tasks[0]["url"] == "https://x/2"

    def test_list_tasks_filter_by_status(self, db: Database) -> None:
        """按状态过滤。"""
        for i in range(2):
            db.create_task(f"https://x/{i}")
        db.update_task(1, status=TASK_STATUS_RUNNING)
        running = db.list_tasks(status=TASK_STATUS_RUNNING)
        assert len(running) == 1
        assert running[0]["id"] == 1

    def test_update_task(self, db: Database) -> None:
        """更新任务字段。"""
        task_id = db.create_task("https://x")
        db.update_task(
            task_id,
            status=TASK_STATUS_RUNNING,
            album_id="ALB1",
            album_name="My Album",
            total_files=10,
        )
        task = db.get_task(task_id)
        assert task is not None
        assert task["status"] == TASK_STATUS_RUNNING
        assert task["album_id"] == "ALB1"
        assert task["album_name"] == "My Album"
        assert task["total_files"] == 10

    def test_update_task_ignores_none(self, db: Database) -> None:
        """update_task 不应覆盖未提供的字段。"""
        task_id = db.create_task("https://x", options={"k": "v"})
        db.update_task(task_id, status=TASK_STATUS_RUNNING)
        task = db.get_task(task_id)
        assert task is not None
        # options_json 不应被清空
        assert "k" in task["options_json"]

    def test_delete_task_cascades(self, db: Database) -> None:
        """删除任务级联删除其文件与事件。"""
        task_id = db.create_task("https://x")
        file_id = db.upsert_file(task_id, "https://file/1", filename="a.jpg")
        db.add_event("test", task_id=task_id)
        db.delete_task(task_id)
        assert db.get_task(task_id) is None
        assert db.get_file(file_id) is None


# ============== File CRUD ==============
class TestFileCRUD:
    def test_upsert_new_file(self, db: Database) -> None:
        """插入新文件。"""
        task_id = db.create_task("https://x")
        file_id = db.upsert_file(
            task_id, "https://file/1",
            filename="a.jpg", file_size=1024,
        )
        assert file_id > 0
        record = db.get_file(file_id)
        assert record is not None
        assert record["filename"] == "a.jpg"
        assert record["file_size"] == 1024
        assert record["status"] == FILE_STATUS_PENDING

    def test_upsert_existing_file(self, db: Database) -> None:
        """同一 task+item_url 重复 upsert 应更新而非插入。"""
        task_id = db.create_task("https://x")
        first = db.upsert_file(task_id, "https://file/1", filename="a.jpg")
        second = db.upsert_file(
            task_id, "https://file/1",
            filename="a.jpg", file_size=2048,
        )
        assert first == second
        record = db.get_file(first)
        assert record is not None
        assert record["file_size"] == 2048

    def test_list_files(self, db: Database) -> None:
        """列出任务下的文件。"""
        task_id = db.create_task("https://x")
        for i in range(3):
            db.upsert_file(task_id, f"https://file/{i}")
        files = db.list_files(task_id)
        assert len(files) == 3

    def test_count_by_status(self, db: Database) -> None:
        """按状态统计文件数。"""
        task_id = db.create_task("https://x")
        ids = [db.upsert_file(task_id, f"https://file/{i}") for i in range(5)]
        db.update_file(ids[0], status=FILE_STATUS_COMPLETED)
        db.update_file(ids[1], status=FILE_STATUS_COMPLETED)
        db.update_file(ids[2], status=FILE_STATUS_FAILED)
        counts = db.count_files_by_status(task_id)
        assert counts[FILE_STATUS_COMPLETED] == 2
        assert counts[FILE_STATUS_FAILED] == 1
        assert counts[FILE_STATUS_PENDING] == 2

    def test_get_resume_files(self, db: Database) -> None:
        """get_resume_files 仅返回 pending + failed。"""
        task_id = db.create_task("https://x")
        ids = [db.upsert_file(task_id, f"https://file/{i}") for i in range(4)]
        db.update_file(ids[0], status=FILE_STATUS_COMPLETED)
        db.update_file(ids[1], status=FILE_STATUS_FAILED)
        db.update_file(ids[2], status=FILE_STATUS_DOWNLOADING)
        # ids[3] 保持 pending
        resume = db.get_resume_files(task_id)
        assert len(resume) == 2
        urls = {f["item_url"] for f in resume}
        assert "https://file/1" in urls
        assert "https://file/3" in urls

    def test_reset_file_for_retry(self, db: Database) -> None:
        """重置文件状态。"""
        task_id = db.create_task("https://x")
        file_id = db.upsert_file(task_id, "https://file/1")
        db.update_file(file_id, status=FILE_STATUS_FAILED, error_message="boom")
        db.reset_file_for_retry(file_id)
        record = db.get_file(file_id)
        assert record is not None
        assert record["status"] == FILE_STATUS_PENDING
        assert record["error_message"] is None

    def test_increment_retry(self, db: Database) -> None:
        """重试计数。"""
        task_id = db.create_task("https://x")
        file_id = db.upsert_file(task_id, "https://file/1")
        c1 = db.increment_retry(file_id)
        c2 = db.increment_retry(file_id)
        assert c1 == 1
        assert c2 == 2

    def test_get_task_stats(self, db: Database) -> None:
        """统计任务进度。"""
        task_id = db.create_task("https://x")
        ids = [db.upsert_file(task_id, f"https://file/{i}", file_size=1000) for i in range(3)]
        db.update_file(ids[0], status=FILE_STATUS_COMPLETED, downloaded_bytes=1000)
        db.update_file(ids[1], status=FILE_STATUS_FAILED)
        stats = db.get_task_stats(task_id)
        assert stats["total_files"] == 3
        assert stats["completed_files"] == 1
        assert stats["failed_files"] == 1
        assert stats["pending_files"] == 1
        assert stats["total_bytes"] == 3000
        assert stats["downloaded_bytes"] == 1000


# ============== Events ==============
class TestEvents:
    def test_add_event(self, db: Database) -> None:
        """添加事件返回 id。"""
        task_id = db.create_task("https://x")
        event_id = db.add_event(
            "Task started", task_id=task_id, details="details",
        )
        assert event_id > 0
        events = db.list_events(task_id=task_id)
        assert len(events) == 1
        assert events[0]["event"] == "Task started"
        assert events[0]["details"] == "details"

    def test_list_events_paginated(self, db: Database) -> None:
        """事件分页（按 id 倒序）。"""
        task_id = db.create_task("https://x")
        for i in range(5):
            db.add_event(f"event-{i}", task_id=task_id)
        # 最近 3 条
        events = db.list_events(task_id=task_id, limit=3)
        assert len(events) == 3
        # 第一条是最后插入的
        assert events[0]["event"] == "event-4"
        assert events[2]["event"] == "event-2"

    def test_trim_events(self, db: Database) -> None:
        """裁剪旧事件。"""
        for i in range(10):
            db.add_event(f"event-{i}")
        db.trim_events(keep_last=3)
        events = db.list_events(limit=100)
        assert len(events) == 3


# ============== Bulk ==============
class TestBulk:
    def test_bulk_upsert_files(self, db: Database) -> None:
        """批量 upsert。"""
        task_id = db.create_task("https://x")
        items = [
            {"item_url": f"https://file/{i}", "filename": f"f{i}.jpg"}
            for i in range(5)
        ]
        count = db.bulk_upsert_files(task_id, items)
        assert count == 5
        files = db.list_files(task_id)
        assert len(files) == 5

    def test_bulk_upsert_skips_existing(self, db: Database) -> None:
        """已存在的 item_url 不会重复插入。"""
        task_id = db.create_task("https://x")
        db.upsert_file(task_id, "https://file/1", filename="a.jpg")
        items = [
            {"item_url": "https://file/1", "filename": "a.jpg"},
            {"item_url": "https://file/2", "filename": "b.jpg"},
        ]
        count = db.bulk_upsert_files(task_id, items)
        assert count == 1  # 只有 file/2 是新的
        files = db.list_files(task_id)
        assert len(files) == 2


# ============== Default path ==============
class TestDefaultPath:
    def test_get_default_db_path_creates_dir(self, tmp_path: Path, monkeypatch) -> None:
        """默认路径应在 $HOME/.bunkr_downloader/state.db。"""
        monkeypatch.setattr("pathlib.Path.home", lambda: tmp_path)
        path = get_default_db_path()
        assert path == tmp_path / ".bunkr_downloader" / "state.db"
        assert path.parent.exists()


# ============== Concurrency ==============
class TestConcurrency:
    def test_concurrent_writes(self, db: Database) -> None:
        """并发写不丢失数据。"""
        import threading

        task_id = db.create_task("https://x")
        errors = []

        def writer(i: int) -> None:
            try:
                db.upsert_file(task_id, f"https://file/{i}", filename=f"f{i}.jpg")
            except Exception as e:
                errors.append(e)

        threads = [threading.Thread(target=writer, args=(i,)) for i in range(20)]
        for t in threads:
            t.start()
        for t in threads:
            t.join()

        assert not errors
        files = db.list_files(task_id)
        assert len(files) == 20
