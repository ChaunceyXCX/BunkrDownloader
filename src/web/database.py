"""SQLite 持久化层。

为下载任务、文件状态、事件日志提供持久化存储，用于：
    - 进程被中断 / 重启后从失败的文件续传
    - Web UI 实时展示历史与当前进度
    - 任务队列持久化

所有方法都是线程安全的（使用 check_same_thread=False + 自带锁）。
"""

from __future__ import annotations

import json
import sqlite3
import threading
from contextlib import contextmanager
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Iterable


# ============================
# 状态枚举（与 enums.py 对齐，但为了解耦独立维护字符串值）
# ============================
TASK_STATUS_PENDING = "pending"
TASK_STATUS_RUNNING = "running"
TASK_STATUS_PAUSED = "paused"
TASK_STATUS_COMPLETED = "completed"
TASK_STATUS_FAILED = "failed"
TASK_STATUS_CANCELED = "canceled"

FILE_STATUS_PENDING = "pending"
FILE_STATUS_DOWNLOADING = "downloading"
FILE_STATUS_COMPLETED = "completed"
FILE_STATUS_FAILED = "failed"
FILE_STATUS_SKIPPED = "skipped"

EVENT_LEVEL_INFO = "info"
EVENT_LEVEL_WARN = "warn"
EVENT_LEVEL_ERROR = "error"


# ============================
# Schema
# ============================
SCHEMA = """
CREATE TABLE IF NOT EXISTS tasks (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    url             TEXT    NOT NULL,
    album_id        TEXT,
    album_name      TEXT,
    status          TEXT    NOT NULL DEFAULT 'pending',
    total_files     INTEGER NOT NULL DEFAULT 0,
    completed_files INTEGER NOT NULL DEFAULT 0,
    failed_files    INTEGER NOT NULL DEFAULT 0,
    skipped_files   INTEGER NOT NULL DEFAULT 0,
    total_bytes     INTEGER NOT NULL DEFAULT 0,
    downloaded_bytes INTEGER NOT NULL DEFAULT 0,
    download_path   TEXT,
    options_json    TEXT,
    error_message   TEXT,
    created_at      TIMESTAMP NOT NULL,
    started_at      TIMESTAMP,
    finished_at     TIMESTAMP,
    updated_at      TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_created ON tasks(created_at DESC);

CREATE TABLE IF NOT EXISTS files (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id          INTEGER NOT NULL,
    item_url         TEXT    NOT NULL,
    filename         TEXT,
    download_link    TEXT,
    file_size        INTEGER NOT NULL DEFAULT 0,
    downloaded_bytes INTEGER NOT NULL DEFAULT 0,
    status           TEXT    NOT NULL DEFAULT 'pending',
    retry_count      INTEGER NOT NULL DEFAULT 0,
    error_message    TEXT,
    item_date        TEXT,
    started_at       TIMESTAMP,
    finished_at      TIMESTAMP,
    updated_at       TIMESTAMP NOT NULL,
    UNIQUE(task_id, item_url),
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_files_task ON files(task_id);
CREATE INDEX IF NOT EXISTS idx_files_status ON files(status);

CREATE TABLE IF NOT EXISTS events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id    INTEGER,
    file_id    INTEGER,
    level      TEXT    NOT NULL DEFAULT 'info',
    event      TEXT    NOT NULL,
    details    TEXT,
    created_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_task ON events(task_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_events_created ON events(created_at DESC);
"""


# ============================
# 工具函数
# ============================
def _now_iso() -> str:
    """返回当前 UTC 时间的 ISO 字符串。"""
    return datetime.now(timezone.utc).isoformat()


def _parse_iso(value: str | None) -> datetime | None:
    """安全解析 ISO 时间字符串。"""
    if not value:
        return None
    try:
        return datetime.fromisoformat(value)
    except (ValueError, TypeError):
        return None


def get_default_db_path() -> Path:
    """返回默认 SQLite 数据库路径：~/.bunkr_downloader/state.db。"""
    home = Path.home()
    config_dir = home / ".bunkr_downloader"
    config_dir.mkdir(parents=True, exist_ok=True)
    return config_dir / "state.db"


# ============================
# Database 主类
# ============================
class Database:
    """SQLite 持久化管理器。

    使用示例：
        db = Database("state.db")
        db.init_schema()
        task_id = db.create_task(url="https://bunkr.si/a/XXX", options={...})
    """

    def __init__(self, db_path: str | Path) -> None:
        """初始化数据库连接。"""
        self.db_path = Path(db_path)
        self.db_path.parent.mkdir(parents=True, exist_ok=True)
        # check_same_thread=False 让 aiohttp 跨线程访问时更灵活；锁保证写串行化
        self._conn = sqlite3.connect(
            str(self.db_path),
            check_same_thread=False,
            isolation_level=None,  # 我们手动管理事务
            timeout=30.0,
        )
        self._conn.row_factory = sqlite3.Row
        self._lock = threading.RLock()
        # 启用外键
        self._conn.execute("PRAGMA foreign_keys = ON")
        # WAL 模式：读写并发更友好
        self._conn.execute("PRAGMA journal_mode = WAL")

    def close(self) -> None:
        """关闭数据库连接。"""
        with self._lock:
            self._conn.close()

    def __enter__(self) -> "Database":
        return self

    def __exit__(self, *_args: Any) -> None:
        self.close()

    @contextmanager
    def transaction(self):
        """事务上下文管理器。"""
        with self._lock:
            try:
                self._conn.execute("BEGIN")
                yield self._conn
                self._conn.execute("COMMIT")
            except Exception:
                self._conn.execute("ROLLBACK")
                raise

    # ----- Schema -----
    def init_schema(self) -> None:
        """初始化所有表结构。"""
        with self._lock:
            self._conn.executescript(SCHEMA)

    # ----- Task CRUD -----
    def create_task(
        self,
        url: str,
        *,
        options: dict[str, Any] | None = None,
        download_path: str | None = None,
    ) -> int:
        """创建下载任务，返回 task_id。"""
        now = _now_iso()
        with self.transaction() as conn:
            cursor = conn.execute(
                """
                INSERT INTO tasks (
                    url, status, options_json, download_path,
                    created_at, updated_at
                ) VALUES (?, ?, ?, ?, ?, ?)
                """,
                (
                    url,
                    TASK_STATUS_PENDING,
                    json.dumps(options or {}, ensure_ascii=False),
                    download_path,
                    now,
                    now,
                ),
            )
            return int(cursor.lastrowid)

    def get_task(self, task_id: int) -> dict | None:
        """获取任务详情。"""
        with self._lock:
            row = self._conn.execute(
                "SELECT * FROM tasks WHERE id = ?", (task_id,),
            ).fetchone()
        return dict(row) if row else None

    def list_tasks(self, *, limit: int = 100, status: str | None = None) -> list[dict]:
        """列出任务，可按状态过滤。"""
        query = "SELECT * FROM tasks"
        params: list[Any] = []
        if status:
            query += " WHERE status = ?"
            params.append(status)
        query += " ORDER BY created_at DESC LIMIT ?"
        params.append(limit)
        with self._lock:
            rows = self._conn.execute(query, params).fetchall()
        return [dict(row) for row in rows]

    def update_task(
        self,
        task_id: int,
        *,
        status: str | None = None,
        album_id: str | None = None,
        album_name: str | None = None,
        total_files: int | None = None,
        completed_files: int | None = None,
        failed_files: int | None = None,
        skipped_files: int | None = None,
        total_bytes: int | None = None,
        downloaded_bytes: int | None = None,
        download_path: str | None = None,
        error_message: str | None = None,
        started_at: str | None = None,
        finished_at: str | None = None,
    ) -> None:
        """更新任务字段（只更新提供的字段）。"""
        updates: list[str] = []
        values: list[Any] = []
        fields = {
            "status": status,
            "album_id": album_id,
            "album_name": album_name,
            "total_files": total_files,
            "completed_files": completed_files,
            "failed_files": failed_files,
            "skipped_files": skipped_files,
            "total_bytes": total_bytes,
            "downloaded_bytes": downloaded_bytes,
            "download_path": download_path,
            "error_message": error_message,
            "started_at": started_at,
            "finished_at": finished_at,
        }
        for key, value in fields.items():
            if value is not None:
                updates.append(f"{key} = ?")
                values.append(value)
        if not updates:
            return
        updates.append("updated_at = ?")
        values.append(_now_iso())
        values.append(task_id)
        with self.transaction() as conn:
            conn.execute(
                f"UPDATE tasks SET {', '.join(updates)} WHERE id = ?",
                values,
            )

    def delete_task(self, task_id: int) -> None:
        """删除任务及其所有文件和事件（CASCADE）。"""
        with self.transaction() as conn:
            conn.execute("DELETE FROM tasks WHERE id = ?", (task_id,))

    # ----- File CRUD -----
    def upsert_file(
        self,
        task_id: int,
        item_url: str,
        *,
        filename: str | None = None,
        download_link: str | None = None,
        file_size: int | None = None,
        item_date: str | None = None,
        status: str = FILE_STATUS_PENDING,
    ) -> int:
        """插入或更新一个文件记录，返回 file_id。"""
        now = _now_iso()
        with self.transaction() as conn:
            # 查找是否已存在
            existing = conn.execute(
                "SELECT id FROM files WHERE task_id = ? AND item_url = ?",
                (task_id, item_url),
            ).fetchone()
            if existing:
                file_id = existing["id"]
                # 只更新非空字段
                updates: list[str] = []
                values: list[Any] = []
                for key, value in {
                    "filename": filename,
                    "download_link": download_link,
                    "file_size": file_size,
                    "item_date": item_date,
                    "status": status,
                }.items():
                    if value is not None:
                        updates.append(f"{key} = ?")
                        values.append(value)
                if updates:
                    updates.append("updated_at = ?")
                    values.append(now)
                    values.append(file_id)
                    conn.execute(
                        f"UPDATE files SET {', '.join(updates)} WHERE id = ?",
                        values,
                    )
                return file_id

            cursor = conn.execute(
                """
                INSERT INTO files (
                    task_id, item_url, filename, download_link, file_size,
                    item_date, status, updated_at
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    task_id,
                    item_url,
                    filename,
                    download_link,
                    file_size if file_size is not None else 0,
                    item_date,
                    status,
                    now,
                ),
            )
            return int(cursor.lastrowid)

    def get_file(self, file_id: int) -> dict | None:
        """获取文件详情。"""
        with self._lock:
            row = self._conn.execute(
                "SELECT * FROM files WHERE id = ?", (file_id,),
            ).fetchone()
        return dict(row) if row else None

    def list_files(
        self,
        task_id: int,
        *,
        status: str | None = None,
        limit: int = 1000,
    ) -> list[dict]:
        """列出任务下的文件，可按状态过滤。"""
        query = "SELECT * FROM files WHERE task_id = ?"
        params: list[Any] = [task_id]
        if status:
            query += " AND status = ?"
            params.append(status)
        query += " ORDER BY id ASC LIMIT ?"
        params.append(limit)
        with self._lock:
            rows = self._conn.execute(query, params).fetchall()
        return [dict(row) for row in rows]

    def update_file(
        self,
        file_id: int,
        *,
        status: str | None = None,
        downloaded_bytes: int | None = None,
        file_size: int | None = None,
        retry_count: int | None = None,
        error_message: str | None = None,
        started_at: str | None = None,
        finished_at: str | None = None,
    ) -> None:
        """更新文件字段。"""
        updates: list[str] = []
        values: list[Any] = []
        fields = {
            "status": status,
            "downloaded_bytes": downloaded_bytes,
            "file_size": file_size,
            "retry_count": retry_count,
            "error_message": error_message,
            "started_at": started_at,
            "finished_at": finished_at,
        }
        for key, value in fields.items():
            if value is not None:
                updates.append(f"{key} = ?")
                values.append(value)
        if not updates:
            return
        updates.append("updated_at = ?")
        values.append(_now_iso())
        values.append(file_id)
        with self.transaction() as conn:
            conn.execute(
                f"UPDATE files SET {', '.join(updates)} WHERE id = ?",
                values,
            )

    def count_files_by_status(self, task_id: int) -> dict[str, int]:
        """按状态统计文件数量。"""
        with self._lock:
            rows = self._conn.execute(
                "SELECT status, COUNT(*) as cnt FROM files "
                "WHERE task_id = ? GROUP BY status",
                (task_id,),
            ).fetchall()
        return {row["status"]: row["cnt"] for row in rows}

    def get_resume_files(self, task_id: int) -> list[dict]:
        """获取需要重新下载的文件（pending + failed）。"""
        with self._lock:
            rows = self._conn.execute(
                "SELECT * FROM files WHERE task_id = ? "
                "AND status IN (?, ?) ORDER BY id ASC",
                (task_id, FILE_STATUS_PENDING, FILE_STATUS_FAILED),
            ).fetchall()
        return [dict(row) for row in rows]

    def reset_file_for_retry(self, file_id: int) -> None:
        """重置文件状态以便重试。"""
        now = _now_iso()
        with self.transaction() as conn:
            conn.execute(
                """
                UPDATE files SET
                    status = ?,
                    error_message = NULL,
                    started_at = NULL,
                    finished_at = NULL,
                    updated_at = ?
                WHERE id = ?
                """,
                (FILE_STATUS_PENDING, now, file_id),
            )

    def increment_retry(self, file_id: int) -> int:
        """增加并返回文件重试次数。"""
        with self.transaction() as conn:
            conn.execute(
                "UPDATE files SET retry_count = retry_count + 1, updated_at = ? "
                "WHERE id = ?",
                (_now_iso(), file_id),
            )
            row = conn.execute(
                "SELECT retry_count FROM files WHERE id = ?", (file_id,),
            ).fetchone()
            return int(row["retry_count"]) if row else 0

    # ----- Events -----
    def add_event(
        self,
        event: str,
        *,
        task_id: int | None = None,
        file_id: int | None = None,
        level: str = EVENT_LEVEL_INFO,
        details: str | None = None,
    ) -> int:
        """添加事件日志，返回 event_id。"""
        with self.transaction() as conn:
            cursor = conn.execute(
                """
                INSERT INTO events (task_id, file_id, level, event, details, created_at)
                VALUES (?, ?, ?, ?, ?, ?)
                """,
                (task_id, file_id, level, event, details, _now_iso()),
            )
            return int(cursor.lastrowid)

    def list_events(
        self,
        *,
        task_id: int | None = None,
        limit: int = 200,
        before_id: int | None = None,
    ) -> list[dict]:
        """列出事件日志（按时间倒序）。"""
        query = "SELECT * FROM events"
        params: list[Any] = []
        clauses: list[str] = []
        if task_id is not None:
            clauses.append("task_id = ?")
            params.append(task_id)
        if before_id is not None:
            clauses.append("id < ?")
            params.append(before_id)
        if clauses:
            query += " WHERE " + " AND ".join(clauses)
        query += " ORDER BY id DESC LIMIT ?"
        params.append(limit)
        with self._lock:
            rows = self._conn.execute(query, params).fetchall()
        return [dict(row) for row in rows]

    def trim_events(self, *, keep_last: int = 1000) -> None:
        """裁剪旧事件，只保留最近 N 条。"""
        with self.transaction() as conn:
            conn.execute(
                """
                DELETE FROM events WHERE id NOT IN (
                    SELECT id FROM events ORDER BY id DESC LIMIT ?
                )
                """,
                (keep_last,),
            )

    # ----- 统计 -----
    def get_task_stats(self, task_id: int) -> dict[str, int]:
        """获取任务的统计信息（按文件状态分组）。"""
        with self._lock:
            row = self._conn.execute(
                """
                SELECT
                    COUNT(*) AS total,
                    SUM(CASE WHEN status = ? THEN 1 ELSE 0 END) AS completed,
                    SUM(CASE WHEN status = ? THEN 1 ELSE 0 END) AS failed,
                    SUM(CASE WHEN status = ? THEN 1 ELSE 0 END) AS pending,
                    SUM(CASE WHEN status = ? THEN 1 ELSE 0 END) AS skipped,
                    SUM(CASE WHEN status = ? THEN 1 ELSE 0 END) AS downloading,
                    COALESCE(SUM(file_size), 0) AS total_bytes,
                    COALESCE(SUM(downloaded_bytes), 0) AS downloaded_bytes
                FROM files WHERE task_id = ?
                """,
                (
                    FILE_STATUS_COMPLETED,
                    FILE_STATUS_FAILED,
                    FILE_STATUS_PENDING,
                    FILE_STATUS_SKIPPED,
                    FILE_STATUS_DOWNLOADING,
                    task_id,
                ),
            ).fetchone()
        return {
            "total_files": int(row["total"] or 0),
            "completed_files": int(row["completed"] or 0),
            "failed_files": int(row["failed"] or 0),
            "pending_files": int(row["pending"] or 0),
            "skipped_files": int(row["skipped"] or 0),
            "downloading_files": int(row["downloading"] or 0),
            "total_bytes": int(row["total_bytes"] or 0),
            "downloaded_bytes": int(row["downloaded_bytes"] or 0),
        }

    def get_running_tasks(self) -> list[dict]:
        """获取所有正在运行的任务（用于启动时恢复）。"""
        return self.list_tasks(status=TASK_STATUS_RUNNING)

    def get_resumable_tasks(self) -> list[dict]:
        """获取所有可恢复的任务（pending、paused、failed、running）。"""
        with self._lock:
            rows = self._conn.execute(
                "SELECT * FROM tasks WHERE status IN (?, ?, ?, ?) "
                "ORDER BY created_at DESC",
                (
                    TASK_STATUS_PENDING,
                    TASK_STATUS_PAUSED,
                    TASK_STATUS_FAILED,
                    TASK_STATUS_RUNNING,
                ),
            ).fetchall()
        return [dict(row) for row in rows]

    # ----- 批量 -----
    def bulk_upsert_files(
        self,
        task_id: int,
        items: Iterable[dict],
    ) -> int:
        """批量插入/更新文件。items 中每项至少包含 item_url。"""
        count = 0
        with self.transaction() as conn:
            for item in items:
                item_url = item["item_url"]
                existing = conn.execute(
                    "SELECT id FROM files WHERE task_id = ? AND item_url = ?",
                    (task_id, item_url),
                ).fetchone()
                if existing:
                    continue
                conn.execute(
                    """
                    INSERT INTO files (
                        task_id, item_url, filename, file_size, item_date,
                        status, updated_at
                    ) VALUES (?, ?, ?, ?, ?, ?, ?)
                    """,
                    (
                        task_id,
                        item_url,
                        item.get("filename"),
                        item.get("file_size", 0) or 0,
                        item.get("item_date"),
                        item.get("status", FILE_STATUS_PENDING),
                        _now_iso(),
                    ),
                )
                count += 1
        return count


def init_db(db_path: str | Path | None = None) -> Database:
    """便捷函数：初始化数据库并返回 Database 实例。"""
    path = Path(db_path) if db_path else get_default_db_path()
    db = Database(path)
    db.init_schema()
    return db
