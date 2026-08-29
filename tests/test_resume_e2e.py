"""End-to-end resume test.

Simulates a real crash-and-restart scenario:
    1. Start a task (mocked to avoid real network)
    2. Some files complete, some fail
    3. Process "crashes" (we kill the running task)
    4. Restart: verify only failed/pending files are downloaded
"""

from __future__ import annotations

import asyncio
from pathlib import Path
from unittest.mock import MagicMock

import pytest

from src.web.database import (
    FILE_STATUS_COMPLETED,
    FILE_STATUS_FAILED,
    FILE_STATUS_PENDING,
    TASK_STATUS_RUNNING,
    init_db,
)
from src.web.progress_tracker import ProgressTracker
from src.web.task_manager import TaskManager


@pytest.mark.asyncio
async def test_e2e_resume_skips_completed_files(tmp_path: Path) -> None:
    """端到端：模拟任务运行 → 部分完成 → 重启 → 验证跳过已完成。"""
    db_path = tmp_path / "e2e.db"
    db = init_db(db_path)
    tracker = ProgressTracker(db)
    tracker.broadcast = MagicMock()  # 不发 WS

    tm = TaskManager(db, tracker)
    await tm.initialize()

    # 1. 创建一个任务
    task_id = await tm.create_task("https://bunkr.si/a/FAKE")

    # 2. 直接通过 DB 模拟文件注册与状态
    f1 = db.upsert_file(task_id, "https://file/1.jpg", filename="1.jpg")
    f2 = db.upsert_file(task_id, "https://file/2.jpg", filename="2.jpg")
    f3 = db.upsert_file(task_id, "https://file/3.jpg", filename="3.jpg")
    db.update_task(task_id, total_files=3)

    # 3. 模拟运行中：f1 完成，f2 失败，f3 仍 pending
    db.update_file(f1, status=FILE_STATUS_COMPLETED, file_size=1000, downloaded_bytes=1000)
    db.update_file(f2, status=FILE_STATUS_FAILED, error_message="timeout")
    db.update_task(task_id, status=TASK_STATUS_RUNNING, completed_files=1, failed_files=1)

    # 4. 模拟崩溃：进程退出
    await tm.shutdown()
    db.close()

    # 5. 重启：创建新实例（模拟新进程）
    db2 = init_db(db_path)
    tracker2 = ProgressTracker(db2)
    tracker2.broadcast = MagicMock()
    tm2 = TaskManager(db2, tracker2)
    await tm2.initialize()

    # 6. 验证：之前的 running 任务被自动标记为 paused
    task = db2.get_task(task_id)
    assert task is not None
    assert task["status"] == "paused", (
        f"Expected paused after restart, got {task['status']}"
    )

    # 7. 验证 get_resume_files 只返回 failed + pending
    resume = db2.get_resume_files(task_id)
    resume_urls = {f["item_url"] for f in resume}
    assert "https://file/1.jpg" not in resume_urls, "Completed file should NOT be in resume list"
    assert "https://file/2.jpg" in resume_urls, "Failed file should be in resume list"
    assert "https://file/3.jpg" in resume_urls, "Pending file should be in resume list"

    # 8. 验证：调用 retry_failed 会重置 failed 文件
    await tm2.retry_failed(task_id)
    f2_after = db2.get_file(f2)
    assert f2_after is not None
    assert f2_after["status"] == FILE_STATUS_PENDING, (
        "Failed file should be reset to pending"
    )

    # 9. 验证：completed 文件仍保持 completed
    f1_after = db2.get_file(f1)
    assert f1_after is not None
    assert f1_after["status"] == FILE_STATUS_COMPLETED, (
        "Completed file should remain completed"
    )

    # 清理
    await tm2.shutdown()
    db2.close()


@pytest.mark.asyncio
async def test_e2e_multiple_tasks_resume_independently(tmp_path: Path) -> None:
    """多个任务的 resume 互不影响。"""
    db_path = tmp_path / "multi.db"
    db = init_db(db_path)
    tracker = ProgressTracker(db)
    tracker.broadcast = MagicMock()

    tm = TaskManager(db, tracker)
    await tm.initialize()

    # 创建 3 个任务
    t1 = await tm.create_task("https://bunkr.si/a/A")
    t2 = await tm.create_task("https://bunkr.si/a/B")
    t3 = await tm.create_task("https://bunkr.si/a/C")

    # t1 全部完成
    for i in range(3):
        f = db.upsert_file(t1, f"https://a/{i}", filename=f"a{i}.jpg")
        db.update_file(f, status=FILE_STATUS_COMPLETED, file_size=100)
    db.update_task(t1, status="running", total_files=3, completed_files=3)

    # t2 部分完成，部分失败
    for i in range(3):
        f = db.upsert_file(t2, f"https://b/{i}", filename=f"b{i}.jpg")
        if i == 0:
            db.update_file(f, status=FILE_STATUS_COMPLETED, file_size=100)
        elif i == 1:
            db.update_file(f, status=FILE_STATUS_FAILED, error_message="err")
    db.update_task(t2, status="running", total_files=3, completed_files=1, failed_files=1)

    # t3 全部 pending
    for i in range(3):
        db.upsert_file(t3, f"https://c/{i}", filename=f"c{i}.jpg")
    db.update_task(t3, status="running", total_files=3)

    # 模拟崩溃
    await tm.shutdown()
    db.close()

    # 重启
    db2 = init_db(db_path)
    tracker2 = ProgressTracker(db2)
    tracker2.broadcast = MagicMock()
    tm2 = TaskManager(db2, tracker2)
    await tm2.initialize()

    # 所有任务应为 paused
    for tid in (t1, t2, t3):
        task = db2.get_task(tid)
        assert task is not None
        assert task["status"] == "paused"

    # t1: 无需 resume（全部完成）
    assert len(db2.get_resume_files(t1)) == 0

    # t2: 1 failed + 1 pending = 2 个待恢复
    # （1 个 completed 已跳过）
    t2_resume = db2.get_resume_files(t2)
    assert len(t2_resume) == 2

    # t3: 3 pending = 3 个待恢复
    t3_resume = db2.get_resume_files(t3)
    assert len(t3_resume) == 3

    await tm2.shutdown()
    db2.close()
