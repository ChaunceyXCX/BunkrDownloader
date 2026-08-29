"""Tests for resume / failure recovery flow.

These tests verify the core resume contract: after a process crash or restart,
only the failed/pending files are re-downloaded — completed files are skipped.
"""

from __future__ import annotations

from pathlib import Path
from unittest.mock import MagicMock

import pytest

from src.web.database import (
    FILE_STATUS_COMPLETED,
    FILE_STATUS_FAILED,
    TASK_STATUS_COMPLETED,
    TASK_STATUS_FAILED,
    TASK_STATUS_PAUSED,
    TASK_STATUS_PENDING,
    TASK_STATUS_RUNNING,
    Database,
    init_db,
)
from src.web.task_manager import TaskManager, TaskOptions


@pytest.fixture
def db_path(tmp_path: Path) -> Path:
    """临时数据库路径。"""
    return tmp_path / "resume.db"


@pytest.fixture
def db(db_path: Path) -> Database:
    """已初始化的数据库。"""
    database = init_db(db_path)
    yield database
    database.close()


@pytest.fixture
def tracker(db: Database) -> MagicMock:
    """Mock 的 ProgressTracker（不实际广播 WS）。"""
    t = MagicMock()
    t.wrap = MagicMock(side_effect=lambda mgr, task_id: mgr)
    t.broadcast = MagicMock()
    t.set_event_loop = MagicMock()
    t.add_ws_client = MagicMock()
    t.remove_ws_client = MagicMock()
    return t


@pytest.fixture
def task_manager(db: Database, tracker: MagicMock) -> TaskManager:
    """TaskManager 实例。"""
    return TaskManager(db, tracker)


# ============== Resume contract ==============
class TestResumeContract:
    """核心 resume 行为：失败后只重试失败/未完成项。"""

    @pytest.mark.asyncio
    async def test_completed_files_skipped_on_resume(
        self, task_manager: TaskManager, db: Database,
    ) -> None:
        """已完成的文件不应被重新下载。"""
        task_id = db.create_task("https://bunkr.si/a/TEST")
        # 模拟 3 个文件，1 完成、1 失败、1 pending
        f1 = db.upsert_file(task_id, "https://file/1.jpg", filename="1.jpg")
        f2 = db.upsert_file(task_id, "https://file/2.jpg", filename="2.jpg")
        f3 = db.upsert_file(task_id, "https://file/3.jpg", filename="3.jpg")
        db.update_file(f1, status=FILE_STATUS_COMPLETED, file_size=1000)
        db.update_file(f2, status=FILE_STATUS_FAILED, error_message="timeout")
        # f3 保持 pending

        # 验证 get_resume_files 只返回 failed + pending
        resume = db.get_resume_files(task_id)
        assert len(resume) == 2
        urls = {f["item_url"] for f in resume}
        assert "https://file/1.jpg" not in urls
        assert "https://file/2.jpg" in urls
        assert "https://file/3.jpg" in urls

    @pytest.mark.asyncio
    async def test_reset_failed_for_retry(
        self, task_manager: TaskManager, db: Database,
    ) -> None:
        """retry_failed 应把所有 failed 文件重置为 pending。"""
        task_id = db.create_task("https://bunkr.si/a/TEST")
        ids = [db.upsert_file(task_id, f"https://file/{i}.jpg") for i in range(3)]
        db.update_file(ids[0], status=FILE_STATUS_COMPLETED)
        db.update_file(ids[1], status=FILE_STATUS_FAILED, error_message="err1")
        db.update_file(ids[2], status=FILE_STATUS_FAILED, error_message="err2")

        # 调用 reset_file_for_retry 模拟 retry
        for file_id in [ids[1], ids[2]]:
            db.reset_file_for_retry(file_id)

        # 重新检查
        resume = db.get_resume_files(task_id)
        assert len(resume) == 2
        # f1 仍是 completed
        record = db.get_file(ids[0])
        assert record is not None
        assert record["status"] == FILE_STATUS_COMPLETED

    @pytest.mark.asyncio
    async def test_resume_after_crash_marks_paused(
        self, task_manager: TaskManager, db: Database,
    ) -> None:
        """进程崩溃后：上次标记为 running 的任务应被标记为 paused。"""
        task_id = db.create_task("https://bunkr.si/a/TEST")
        db.update_task(task_id, status=TASK_STATUS_RUNNING)

        # 模拟新进程启动
        await task_manager.initialize()

        task = db.get_task(task_id)
        assert task is not None
        assert task["status"] == TASK_STATUS_PAUSED


# ============== Task lifecycle ==============
class TestTaskLifecycle:
    @pytest.mark.asyncio
    async def test_create_task_assigns_pending(
        self, task_manager: TaskManager, db: Database,
    ) -> None:
        """新创建的任务应为 pending。"""
        task_id = await task_manager.create_task("https://bunkr.si/a/NEW")
        task = db.get_task(task_id)
        assert task is not None
        assert task["status"] == TASK_STATUS_PENDING

    @pytest.mark.asyncio
    async def test_pause_when_not_running(
        self, task_manager: TaskManager, db: Database,
    ) -> None:
        """暂停未运行的任务应直接改 DB 状态。"""
        task_id = await task_manager.create_task("https://x")
        await task_manager.pause_task(task_id)
        task = db.get_task(task_id)
        assert task is not None
        assert task["status"] == TASK_STATUS_PAUSED

    @pytest.mark.asyncio
    async def test_cancel_when_not_running(
        self, task_manager: TaskManager, db: Database,
    ) -> None:
        """取消未运行的任务应直接改 DB 状态。"""
        task_id = await task_manager.create_task("https://x")
        await task_manager.cancel_task(task_id)
        task = db.get_task(task_id)
        assert task is not None
        assert task["status"] == "canceled"

    @pytest.mark.asyncio
    async def test_delete_task_clears_running(
        self, task_manager: TaskManager, db: Database,
    ) -> None:
        """删除任务应级联清除所有数据。"""
        task_id = await task_manager.create_task("https://x")
        file_id = db.upsert_file(task_id, "https://file/1")
        db.add_event("test", task_id=task_id)
        await task_manager.delete_task(task_id)
        assert db.get_task(task_id) is None
        assert db.get_file(file_id) is None


# ============== TaskOptions ==============
class TestTaskOptions:
    def test_to_dict_round_trip(self) -> None:
        """options 与 dict 互转无损。"""
        opts = TaskOptions(
            custom_path="/tmp/dl",
            no_album_folder=True,
            clean_name=True,
            max_retries=7,
            connections=8,
            rate_limit=2000.0,
            ignore=[".zip"],
            include=["mp4"],
        )
        data = opts.to_dict()
        restored = TaskOptions.from_dict(data)
        assert restored.custom_path == "/tmp/dl"
        assert restored.no_album_folder is True
        assert restored.clean_name is True
        assert restored.max_retries == 7
        assert restored.connections == 8
        assert restored.rate_limit == 2000.0
        assert restored.ignore == [".zip"]
        assert restored.include == ["mp4"]

    def test_from_dict_uses_defaults(self) -> None:
        """空 dict 应使用默认值。"""
        opts = TaskOptions.from_dict({})
        assert opts.custom_path is None
        assert opts.max_retries == 5
        assert opts.connections == 4
        assert not opts.ignore
        assert not opts.include

    def test_to_namespace_creates_args(self) -> None:
        """to_namespace 返回的对象应可供现有 downloader 使用。"""
        opts = TaskOptions(
            custom_path="/tmp",
            max_retries=3,
            connections=2,
            ignore=[".tmp"],
        )
        ns = opts.to_namespace()
        assert ns.custom_path == "/tmp"
        assert ns.max_retries == 3
        assert ns.connections == 2
        assert ns.ignore == [".tmp"]
        # Web 模式应总是禁用 UI
        assert ns.disable_ui is True


# ============== ProgressTracker ==============
class TestProgressTracker:
    """直接测试 ProgressTracker 行为（不发 WS）。"""

    def test_register_file_updates_total(self, db: Database) -> None:
        """注册文件应增加任务总文件数。"""
        from src.web.progress_tracker import ProgressTracker

        tracker = ProgressTracker(db)
        task_id = db.create_task("https://x")
        # tracker 不直接对外 broadcast，使用 mock
        tracker.broadcast = MagicMock()

        tracker.register_file(task_id, "https://file/1", filename="a.jpg")
        tracker.register_file(task_id, "https://file/2", filename="b.jpg")

        task = db.get_task(task_id)
        assert task is not None
        assert task["total_files"] == 2

    def test_emit_file_completed_increments_counter(
        self, db: Database,
    ) -> None:
        """完成事件应增加 completed_files 计数。"""
        from src.web.progress_tracker import ProgressTracker

        tracker = ProgressTracker(db)
        tracker.broadcast = MagicMock()
        task_id = db.create_task("https://x")
        file_id = tracker.register_file(task_id, "https://file/1", filename="a.jpg")

        tracker.emit_file_started(file_id)
        tracker.emit_file_completed(file_id)

        task = db.get_task(task_id)
        assert task is not None
        assert task["completed_files"] == 1

    def test_emit_file_failed_increments_counter(
        self, db: Database,
    ) -> None:
        """失败事件应增加 failed_files 计数。"""
        from src.web.progress_tracker import ProgressTracker

        tracker = ProgressTracker(db)
        tracker.broadcast = MagicMock()
        task_id = db.create_task("https://x")
        file_id = tracker.register_file(task_id, "https://file/1", filename="a.jpg")

        tracker.emit_file_started(file_id)
        tracker.emit_file_failed(file_id, "boom")

        task = db.get_task(task_id)
        assert task is not None
        assert task["failed_files"] == 1

    def test_emit_task_completed_when_all_done(
        self, db: Database,
    ) -> None:
        """所有文件完成时任务应为 completed。"""
        from src.web.progress_tracker import ProgressTracker

        tracker = ProgressTracker(db)
        tracker.broadcast = MagicMock()
        task_id = db.create_task("https://x")
        f1 = tracker.register_file(task_id, "https://file/1")
        f2 = tracker.register_file(task_id, "https://file/2")

        tracker.emit_file_started(f1)
        tracker.emit_file_completed(f1)
        tracker.emit_file_started(f2)
        tracker.emit_file_completed(f2)

        tracker.emit_task_completed(task_id)

        task = db.get_task(task_id)
        assert task is not None
        assert task["status"] == TASK_STATUS_COMPLETED

    def test_emit_task_completed_with_failures(
        self, db: Database,
    ) -> None:
        """存在失败文件时任务应为 failed。"""
        from src.web.progress_tracker import ProgressTracker

        tracker = ProgressTracker(db)
        tracker.broadcast = MagicMock()
        task_id = db.create_task("https://x")
        f1 = tracker.register_file(task_id, "https://file/1")
        f2 = tracker.register_file(task_id, "https://file/2")

        tracker.emit_file_started(f1)
        tracker.emit_file_completed(f1)
        tracker.emit_file_started(f2)
        tracker.emit_file_failed(f2, "boom")

        tracker.emit_task_completed(task_id)

        task = db.get_task(task_id)
        assert task is not None
        assert task["status"] == TASK_STATUS_FAILED
