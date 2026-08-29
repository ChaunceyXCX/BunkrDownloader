"""Web 控制面板模块。

为 BunkrDownloader 提供 Web UI、SQLite 持久化、断点续传支持。

模块：
    database         - SQLite 持久化层（schema + DAO）
    progress_tracker - 进度回调 → 数据库 + WebSocket 广播
    task_manager     - 任务调度（包装现有 downloader）
    server           - aiohttp Web 服务器（REST API + WebSocket）
"""

from .database import Database, get_default_db_path, init_db
from .progress_tracker import ProgressTracker
from .task_manager import TaskManager, TaskOptions

__all__ = [
    "Database",
    "TaskManager",
    "TaskOptions",
    "ProgressTracker",
    "get_default_db_path",
    "init_db",
]
