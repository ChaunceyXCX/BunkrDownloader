"""End-to-end resume test.

Simulates a real crash-and-restart scenario:
    1. Start a task (mocked to avoid real network)
    2. Some files complete, some fail
    3. Process "crashes" (we kill the running task)
    4. Restart: verify only failed/pending files are downloaded
"""

from __future__ import annotations

from pathlib import Path
from unittest.mock import MagicMock

import pytest

from src.web.database import (
    FILE_STATUS_COMPLETED,
    FILE_STATUS_FAILED,
    TASK_STATUS_RUNNING,
    init_db,
)
from src.web.progress_tracker import ProgressTracker
from src.web.task_manager import TaskManager

# 每个测试场景用到的文件数（用于控制 too-many-locals）
_FILES_PER_TASK = 3


def _build_scene(tmp_path: Path) -> tuple[Path, int]:
    """构造 e2e 场景：1 个任务，3 个文件（1 完成 + 1 失败 + 1 pending）。

    Returns:
        (db_path, task_id)
    """
    db_path = tmp_path / "e2e.db"
    db = init_db(db_path)
    task_id = db.create_task("https://bunkr.si/a/FAKE")

    f1 = db.upsert_file(task_id, "https://file/1.jpg", filename="1.jpg")
    f2 = db.upsert_file(task_id, "https://file/2.jpg", filename="2.jpg")
    # pylint: disable=unused-variable  # 显式插入，用于后续 resume 计数校验
    f3 = db.upsert_file(task_id, "https://file/3.jpg", filename="3.jpg")

    db.update_file(f1, status=FILE_STATUS_COMPLETED, file_size=1000, downloaded_bytes=1000)
    db.update_file(f2, status=FILE_STATUS_FAILED, error_message="timeout")
    db.update_task(task_id, total_files=_FILES_PER_TASK, status=TASK_STATUS_RUNNING,
                   completed_files=1, failed_files=1)
    db.close()
    return db_path, task_id


@pytest.mark.asyncio
async def test_e2e_resume_skips_completed_files(tmp_path: Path) -> None:
    """端到端：模拟任务运行 → 部分完成 → 重启 → 验证跳过已完成。"""
    db_path, task_id = _build_scene(tmp_path)

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
    failed_files = db2.list_files(task_id, status=FILE_STATUS_FAILED)
    assert len(failed_files) == 0, "No files should remain in failed state"

    # 9. 验证：completed 文件仍保持 completed
    completed = db2.list_files(task_id, status=FILE_STATUS_COMPLETED)
    assert any(f["item_url"] == "https://file/1.jpg" for f in completed), (
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

    # 创建 3 个任务，状态各异
    t1 = db.create_task("https://bunkr.si/a/A")
    t2 = db.create_task("https://bunkr.si/a/B")
    t3 = db.create_task("https://bunkr.si/a/C")

    for i in range(_FILES_PER_TASK):
        f = db.upsert_file(t1, f"https://a/{i}", filename=f"a{i}.jpg")
        db.update_file(f, status=FILE_STATUS_COMPLETED, file_size=100)
    db.update_task(
        t1, status="completed",
        total_files=_FILES_PER_TASK, completed_files=_FILES_PER_TASK,
    )

    for i in range(_FILES_PER_TASK):
        f = db.upsert_file(t2, f"https://b/{i}", filename=f"b{i}.jpg")
        if i == 0:
            db.update_file(f, status=FILE_STATUS_COMPLETED, file_size=100)
        elif i == 1:
            db.update_file(f, status=FILE_STATUS_FAILED, error_message="err")
    db.update_task(t2, status="running", total_files=_FILES_PER_TASK,
                   completed_files=1, failed_files=1)

    for i in range(_FILES_PER_TASK):
        db.upsert_file(t3, f"https://c/{i}", filename=f"c{i}.jpg")
    db.update_task(t3, status="running", total_files=_FILES_PER_TASK)
    db.close()

    # 模拟进程崩溃后重启
    db2 = init_db(db_path)
    tracker2 = ProgressTracker(db2)
    tracker2.broadcast = MagicMock()
    tm2 = TaskManager(db2, tracker2)
    await tm2.initialize()

    # 所有任务应为 paused（除已 completed 的）
    task1 = db2.get_task(t1)
    task2 = db2.get_task(t2)
    task3 = db2.get_task(t3)
    assert task1["status"] == "completed"
    assert task2["status"] == "paused"
    assert task3["status"] == "paused"

    # t1: 无需 resume（全部完成）
    assert len(db2.get_resume_files(t1)) == 0
    # t2: 1 failed + 1 pending = 2 个待恢复
    assert len(db2.get_resume_files(t2)) == 2
    # t3: 3 pending = 3 个待恢复
    assert len(db2.get_resume_files(t3)) == 3

    await tm2.shutdown()
    db2.close()
