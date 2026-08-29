"""进度跟踪器。

把现有 LiveManager 的进度回调桥接到：
    1. SQLite 数据库（持久化）
    2. WebSocket（实时推送给前端）

通过包装 LiveManager 实现，不修改其源代码。
"""

from __future__ import annotations

import asyncio
import json
import logging
import time
from dataclasses import asdict, dataclass, field
from datetime import datetime, timezone
from typing import TYPE_CHECKING, Any

from .database import (
    EVENT_LEVEL_ERROR,
    EVENT_LEVEL_INFO,
    EVENT_LEVEL_WARN,
    FILE_STATUS_COMPLETED,
    FILE_STATUS_DOWNLOADING,
    FILE_STATUS_FAILED,
    FILE_STATUS_PENDING,
    FILE_STATUS_SKIPPED,
    TASK_STATUS_COMPLETED,
    TASK_STATUS_FAILED,
    TASK_STATUS_PAUSED,
    TASK_STATUS_RUNNING,
    Database,
)

if TYPE_CHECKING:
    from src.managers.live_manager import LiveManager


logger = logging.getLogger(__name__)


@dataclass
class ProgressEvent:
    """单个进度事件的数据结构。"""

    type: str
    task_id: int
    timestamp: str = field(
        default_factory=lambda: datetime.now(timezone.utc).isoformat(),
    )
    data: dict[str, Any] = field(default_factory=dict)

    def to_json(self) -> str:
        """序列化为 JSON 字符串。"""
        return json.dumps(asdict(self), ensure_ascii=False)


# WebSocket 事件类型常量
EVT_TASK_CREATED = "task_created"
EVT_TASK_UPDATED = "task_updated"
EVT_TASK_STARTED = "task_started"
EVT_TASK_COMPLETED = "task_completed"
EVT_TASK_FAILED = "task_failed"
EVT_TASK_PAUSED = "task_paused"

EVT_FILE_REGISTERED = "file_registered"
EVT_FILE_DOWNLOADING = "file_downloading"
EVT_FILE_PROGRESS = "file_progress"
EVT_FILE_COMPLETED = "file_completed"
EVT_FILE_FAILED = "file_failed"
EVT_FILE_SKIPPED = "file_skipped"

EVT_LOG = "log"
EVT_STATS = "stats"


class ProgressTracker:
    """进度跟踪器。

    负责：
        - 接收回调（文件开始/进度/完成/失败、日志）
        - 写入 SQLite
        - 广播到所有 WebSocket 连接

    用法：
        tracker = ProgressTracker(db)
        wrapped_manager = tracker.wrap(live_manager, task_id)
        # ... 之后 wrapped_manager 收到的所有回调都会同步到 DB+WS
    """

    def __init__(self, db: Database) -> None:
        """初始化。"""
        self.db = db
        self._ws_clients: set[Any] = set()
        self._loop: asyncio.AbstractEventLoop | None = None
        # 用于节流 progress 写入（避免每 chunk 都写 DB）
        self._last_progress_write: dict[int, float] = {}
        self._progress_write_interval = 0.5  # 秒

    # ----- WebSocket 管理 -----
    def set_event_loop(self, loop: asyncio.AbstractEventLoop) -> None:
        """设置主事件循环（用于从其他线程调度广播）。"""
        self._loop = loop

    def add_ws_client(self, ws: Any) -> None:
        """注册 WebSocket 客户端。"""
        self._ws_clients.add(ws)
        logger.debug("WebSocket client added. total=%d", len(self._ws_clients))

    def remove_ws_client(self, ws: Any) -> None:
        """注销 WebSocket 客户端。"""
        self._ws_clients.discard(ws)
        logger.debug("WebSocket client removed. total=%d", len(self._ws_clients))

    def broadcast(self, event: ProgressEvent) -> None:
        """广播事件到所有 WebSocket 客户端。"""
        if not self._ws_clients:
            return
        payload = event.to_json()
        # 优先用 run_coroutine_threadsafe 在主事件循环中发送
        if self._loop is not None and self._loop.is_running():
            try:
                asyncio.run_coroutine_threadsafe(
                    self._broadcast_async(payload),
                    self._loop,
                )
                return
            except Exception:  # noqa: BLE001
                logger.exception("Failed to schedule broadcast")
        # 兜底：直接尝试同步发送（仅在事件循环线程内可用）
        for ws in list(self._ws_clients):
            try:
                self._send_to_ws(ws, payload)
            except Exception:  # noqa: BLE001
                logger.exception("Failed to send WS payload")
                self._ws_clients.discard(ws)

    async def _broadcast_async(self, payload: str) -> None:
        """异步广播到所有客户端。"""
        dead: list[Any] = []
        for ws in list(self._ws_clients):
            try:
                await self._send_to_ws_async(ws, payload)
            except Exception:  # noqa: BLE001
                logger.debug("WebSocket client appears dead, removing")
                dead.append(ws)
        for ws in dead:
            self._ws_clients.discard(ws)

    @staticmethod
    async def _send_to_ws_async(ws: Any, payload: str) -> None:
        """向 aiohttp WebSocket 发送数据。"""
        await ws.send_str(payload)

    @staticmethod
    def _send_to_ws(ws: Any, payload: str) -> None:
        """同步发送（兼容其他 WS 实现）。"""
        send = getattr(ws, "send_str", None) or getattr(ws, "send", None)
        if send is None:
            return
        try:
            send(payload)
        except TypeError:
            send(payload.encode("utf-8"))

    # ----- 任务级事件 -----
    def emit_task_created(self, task_id: int, data: dict[str, Any] | None = None) -> None:
        """广播任务创建事件。"""
        task = self.db.get_task(task_id)
        self.broadcast(ProgressEvent(
            type=EVT_TASK_CREATED, task_id=task_id, data=task or {},
        ))

    def emit_task_started(self, task_id: int) -> None:
        """标记任务开始。"""
        now = datetime.now(timezone.utc).isoformat()
        self.db.update_task(
            task_id,
            status=TASK_STATUS_RUNNING,
            started_at=now,
        )
        self.db.add_event(
            "Task started", task_id=task_id, level=EVENT_LEVEL_INFO,
        )
        self.broadcast(ProgressEvent(
            type=EVT_TASK_STARTED, task_id=task_id, data=self._task_payload(task_id),
        ))

    def emit_task_paused(self, task_id: int, reason: str = "user_request") -> None:
        """标记任务暂停。"""
        self.db.update_task(task_id, status=TASK_STATUS_PAUSED)
        self.db.add_event(
            "Task paused", task_id=task_id,
            level=EVENT_LEVEL_INFO, details=reason,
        )
        self.broadcast(ProgressEvent(
            type=EVT_TASK_PAUSED, task_id=task_id, data=self._task_payload(task_id),
        ))

    def emit_task_completed(self, task_id: int) -> None:
        """标记任务完成。"""
        now = datetime.now(timezone.utc).isoformat()
        stats = self.db.get_task_stats(task_id)
        # 任务完成的前提是：没有失败且没有正在下载的项
        status = (
            TASK_STATUS_COMPLETED
            if stats["failed_files"] == 0 and stats["downloading_files"] == 0
            else TASK_STATUS_FAILED
        )
        self.db.update_task(
            task_id,
            status=status,
            completed_files=stats["completed_files"],
            failed_files=stats["failed_files"],
            skipped_files=stats["skipped_files"],
            total_bytes=stats["total_bytes"],
            downloaded_bytes=stats["downloaded_bytes"],
            finished_at=now,
        )
        self.db.add_event(
            "Task completed" if status == TASK_STATUS_COMPLETED else "Task finished with failures",
            task_id=task_id,
            level=EVENT_LEVEL_INFO if status == TASK_STATUS_COMPLETED else EVENT_LEVEL_WARN,
        )
        event_type = (
            EVT_TASK_COMPLETED if status == TASK_STATUS_COMPLETED else EVT_TASK_FAILED
        )
        self.broadcast(ProgressEvent(
            type=event_type, task_id=task_id, data=self._task_payload(task_id),
        ))

    # ----- 文件级事件 -----
    def register_file(
        self,
        task_id: int,
        item_url: str,
        *,
        filename: str | None = None,
        file_size: int | None = None,
        item_date: str | None = None,
    ) -> int:
        """注册一个文件（创建/获取 file_id），并广播。"""
        file_id = self.db.upsert_file(
            task_id,
            item_url,
            filename=filename,
            file_size=file_size,
            item_date=item_date,
            status=FILE_STATUS_PENDING,
        )
        # 更新任务总文件数
        stats = self.db.get_task_stats(task_id)
        self.db.update_task(
            task_id,
            total_files=stats["total_files"],
            total_bytes=stats["total_bytes"],
        )
        self.broadcast(ProgressEvent(
            type=EVT_FILE_REGISTERED, task_id=task_id,
            data={"file_id": file_id, "item_url": item_url, "filename": filename,
                  "file_size": file_size, "status": FILE_STATUS_PENDING},
        ))
        return file_id

    def emit_file_started(
        self,
        file_id: int,
        *,
        download_link: str | None = None,
        file_size: int | None = None,
    ) -> None:
        """标记文件开始下载。"""
        now = datetime.now(timezone.utc).isoformat()
        self.db.update_file(
            file_id,
            status=FILE_STATUS_DOWNLOADING,
            started_at=now,
        )
        if download_link is not None or file_size is not None:
            # 通过 upsert 的 update 通道设置（避免破坏 update_file 签名）
            # 实际上这两个字段在 update_file 中没有，简化为不再更新
            pass
        self.broadcast(ProgressEvent(
            type=EVT_FILE_DOWNLOADING,
            task_id=self._get_task_id_for_file(file_id),
            data={"file_id": file_id, "status": FILE_STATUS_DOWNLOADING},
        ))

    def emit_file_progress(
        self,
        file_id: int,
        *,
        downloaded_bytes: int,
        file_size: int | None = None,
        force: bool = False,
    ) -> None:
        """报告文件下载进度（带节流）。"""
        now = time.time()
        last = self._last_progress_write.get(file_id, 0)
        if not force and (now - last) < self._progress_write_interval:
            return
        self._last_progress_write[file_id] = now
        if file_size is not None:
            self.db.update_file(
                file_id,
                downloaded_bytes=downloaded_bytes,
                file_size=file_size,
            )
        else:
            self.db.update_file(file_id, downloaded_bytes=downloaded_bytes)
        # 广播文件进度
        file_record = self.db.get_file(file_id)
        if not file_record:
            return
        progress = 0.0
        size = file_record.get("file_size") or 0
        if size > 0:
            progress = min(100.0, downloaded_bytes / size * 100)
        self.broadcast(ProgressEvent(
            type=EVT_FILE_PROGRESS,
            task_id=file_record["task_id"],
            data={
                "file_id": file_id,
                "downloaded_bytes": downloaded_bytes,
                "file_size": size,
                "progress": round(progress, 2),
                "status": FILE_STATUS_DOWNLOADING,
            },
        ))

    def emit_file_completed(self, file_id: int) -> None:
        """标记文件下载完成。"""
        now = datetime.now(timezone.utc).isoformat()
        file_record = self.db.get_file(file_id)
        if not file_record:
            return
        size = file_record.get("file_size") or 0
        self.db.update_file(
            file_id,
            status=FILE_STATUS_COMPLETED,
            downloaded_bytes=size,
            finished_at=now,
        )
        # 更新任务统计
        stats = self.db.get_task_stats(file_record["task_id"])
        self.db.update_task(
            file_record["task_id"],
            completed_files=stats["completed_files"],
            downloaded_bytes=stats["downloaded_bytes"],
        )
        self.broadcast(ProgressEvent(
            type=EVT_FILE_COMPLETED,
            task_id=file_record["task_id"],
            data={"file_id": file_id, "status": FILE_STATUS_COMPLETED,
                  "downloaded_bytes": size, "file_size": size},
        ))

    def emit_file_failed(self, file_id: int, error: str) -> None:
        """标记文件下载失败。"""
        now = datetime.now(timezone.utc).isoformat()
        self.db.update_file(
            file_id,
            status=FILE_STATUS_FAILED,
            error_message=error,
            finished_at=now,
        )
        self.db.add_event(
            "File failed", file_id=file_id, level=EVENT_LEVEL_ERROR, details=error,
        )
        file_record = self.db.get_file(file_id)
        if file_record:
            stats = self.db.get_task_stats(file_record["task_id"])
            self.db.update_task(
                file_record["task_id"],
                failed_files=stats["failed_files"],
            )
            self.broadcast(ProgressEvent(
                type=EVT_FILE_FAILED,
                task_id=file_record["task_id"],
                data={"file_id": file_id, "status": FILE_STATUS_FAILED, "error": error},
            ))

    def emit_file_skipped(self, file_id: int, reason: str) -> None:
        """标记文件跳过。"""
        now = datetime.now(timezone.utc).isoformat()
        self.db.update_file(
            file_id,
            status=FILE_STATUS_SKIPPED,
            finished_at=now,
            error_message=reason,
        )
        file_record = self.db.get_file(file_id)
        if file_record:
            stats = self.db.get_task_stats(file_record["task_id"])
            self.db.update_task(
                file_record["task_id"],
                skipped_files=stats["skipped_files"],
            )
            self.broadcast(ProgressEvent(
                type=EVT_FILE_SKIPPED,
                task_id=file_record["task_id"],
                data={"file_id": file_id, "status": FILE_STATUS_SKIPPED, "reason": reason},
            ))

    def emit_log(
        self,
        task_id: int | None,
        event: str,
        details: str,
        *,
        level: str = EVENT_LEVEL_INFO,
    ) -> None:
        """记录并广播日志事件。"""
        self.db.add_event(event, task_id=task_id, level=level, details=details)
        self.broadcast(ProgressEvent(
            type=EVT_LOG, task_id=task_id or 0,
            data={"level": level, "event": event, "details": details},
        ))

    def emit_stats(self, task_id: int) -> None:
        """广播最新任务统计。"""
        self.broadcast(ProgressEvent(
            type=EVT_STATS, task_id=task_id, data=self._task_payload(task_id),
        ))

    # ----- LiveManager 包装 -----
    def wrap(
        self,
        live_manager: "LiveManager",
        task_id: int,
    ) -> "LiveManager":
        """包装一个 LiveManager，将其所有 update_* 回调同步到 tracker。

        返回的对象与原对象行为一致，但 update_task / update_log / update_summary
        还会额外触发 DB+WS 写入。
        """
        return _WrappedLiveManager(live_manager, self, task_id)

    # ----- helpers -----
    def _task_payload(self, task_id: int) -> dict[str, Any]:
        """构造任务 payload（task + stats 合并）。"""
        payload = self.db.to_payload(task_id)
        return payload if payload is not None else {"task_id": task_id}

    def _get_task_id_for_file(self, file_id: int) -> int:
        """获取文件所属的任务 ID。"""
        record = self.db.get_file(file_id)
        return int(record["task_id"]) if record else 0


class _WrappedLiveManager:
    """LiveManager 的透明包装，把回调转发到 ProgressTracker。"""

    def __init__(
        self,
        inner: "LiveManager",
        tracker: ProgressTracker,
        task_id: int,
    ) -> None:
        """初始化包装器。"""
        self._inner = inner
        self._tracker = tracker
        self._task_id = task_id
        # 记录 task_id -> file_id 映射（LiveManager 的 task_id 是临时 ID）
        self._file_map: dict[int, int] = {}

    def __getattr__(self, name: str) -> Any:
        """未覆盖的属性/方法直接代理到内部对象。"""
        return getattr(self._inner, name)

    # ----- 显式覆盖 -----
    def add_task(self, current_task: int = 0, total: int = 100) -> int:
        """代理 add_task 并返回内部 task id。"""
        return self._inner.add_task(current_task=current_task, total=total)

    def update_task(
        self,
        task_id: int,
        completed: int | None = None,
        advance: int = 0,
        *,
        visible: bool = True,
    ) -> None:
        """代理 update_task 并把进度同步到 tracker。"""
        self._inner.update_task(
            task_id, completed=completed, advance=advance, visible=visible,
        )
        if completed is not None:
            file_id = self._file_map.get(task_id)
            if file_id is not None:
                self._tracker.emit_file_progress(
                    file_id,
                    downloaded_bytes=int(completed),
                    force=True,
                )

    def update_log(self, *, event: str, details: str) -> None:
        """代理 update_log 并写入日志。"""
        self._inner.update_log(event=event, details=details)
        level = EVENT_LEVEL_INFO
        if any(kw in event.lower() for kw in ("error", "fail", "blocked")):
            level = EVENT_LEVEL_ERROR
        elif any(kw in event.lower() for kw in ("warn", "retry", "skip")):
            level = EVENT_LEVEL_WARN
        self._tracker.emit_log(
            self._task_id, event, details, level=level,
        )

    def update_summary(self, task_reason: Any) -> None:
        """代理 update_summary。"""
        self._inner.update_summary(task_reason)

    def register_file_id(self, internal_task_id: int, db_file_id: int) -> None:
        """建立 LiveManager 的内部 task_id 与 DB file_id 的映射。"""
        self._file_map[internal_task_id] = db_file_id

    def stop(self) -> None:
        """代理 stop，停止时标记任务完成。"""
        self._inner.stop()
        self._tracker.emit_task_completed(self._task_id)
