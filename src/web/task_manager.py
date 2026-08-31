"""任务调度器。

包装现有 BunkrDownloader 的 download 流程，添加：
    - 任务队列与并发控制
    - 进度同步到 SQLite + WebSocket（通过 ProgressTracker）
    - 断点续传：扫描 SQLite 中 pending/failed 的 files，仅重新下载这些
"""

from __future__ import annotations

import asyncio
import json
import logging
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import TYPE_CHECKING, Any

from src.crawlers.crawler_utils import get_download_info
from src.downloaders.album_downloader import AlbumDownloader
from src.downloaders.media_downloader import MediaDownloader
from src.managers.live_manager import initialize_managers
from src.misc.file_utils import create_download_directory
from src.misc.general_utils import fetch_page
from src.misc.url_utils import (
    get_album_id,
    get_album_name,
    get_identifier,
    get_host_page,
    normalize_url,
    resolve_url_type,
)
from src.models import (
    AlbumInfo,
    DownloadInfo,
    RetryConfig,
    SessionInfo,
    UrlInfo,
)

from .database import (
    EVENT_LEVEL_ERROR,
    EVENT_LEVEL_INFO,
    FILE_STATUS_COMPLETED,
    FILE_STATUS_FAILED,
    FILE_STATUS_PENDING,
    TASK_STATUS_CANCELED,
    TASK_STATUS_FAILED,
    TASK_STATUS_PAUSED,
    TASK_STATUS_PENDING,
    TASK_STATUS_RUNNING,
    Database,
)
from .progress_tracker import ProgressTracker, _WrappedLiveManager

if TYPE_CHECKING:
    from src.managers.live_manager import LiveManager

logger = logging.getLogger(__name__)


# ============================
# 配置数据类
# ============================
@dataclass
class TaskOptions:
    """任务的下载选项。"""

    custom_path: str | None = None
    no_download_folder: bool = True
    no_album_folder: bool = False
    clean_name: bool = False
    max_retries: int = 5
    connections: int = 4
    rate_limit: float | None = None
    ignore: list[str] = field(default_factory=list)
    include: list[str] = field(default_factory=list)
    disable_server_check: bool = False

    def to_dict(self) -> dict[str, Any]:
        """转为 dict（用于 JSON 持久化）。"""
        return {
            "custom_path": self.custom_path,
            "no_download_folder": self.no_download_folder,
            "no_album_folder": self.no_album_folder,
            "clean_name": self.clean_name,
            "max_retries": self.max_retries,
            "connections": self.connections,
            "rate_limit": self.rate_limit,
            "ignore": self.ignore,
            "include": self.include,
            "disable_server_check": self.disable_server_check,
        }

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "TaskOptions":
        """从 dict 构造。"""
        return cls(
            custom_path=data.get("custom_path"),
            no_download_folder=data.get("no_download_folder", True),
            no_album_folder=data.get("no_album_folder", False),
            clean_name=data.get("clean_name", False),
            max_retries=data.get("max_retries", 5),
            connections=data.get("connections", 4),
            rate_limit=data.get("rate_limit"),
            ignore=data.get("ignore") or [],
            include=data.get("include") or [],
            disable_server_check=data.get("disable_server_check", False),
        )

    def to_namespace(self) -> Any:
        """转为类似 argparse.Namespace 的对象，供现有 downloader 使用。"""
        import argparse

        return argparse.Namespace(
            custom_path=self.custom_path,
            no_download_folder=self.no_download_folder,
            no_album_folder=self.no_album_folder,
            clean_name=self.clean_name,
            max_retries=self.max_retries,
            connections=self.connections,
            rate_limit=self.rate_limit,
            ignore=self.ignore or None,
            include=self.include or None,
            disable_server_check=self.disable_server_check,
            disable_ui=True,           # Web 模式下总是禁用终端 UI
            disable_disk_check=False,
            dry_run=False,
        )


# ============================
# TaskManager
# ============================
class TaskManager:
    """管理下载任务的生命周期。

    职责：
        - 任务创建 / 启动 / 暂停 / 恢复 / 取消
        - 包装现有 downloader，将进度同步到 DB + WS
        - 维护每个 task 的内部状态机
        - 进程退出后能从 DB 恢复运行中/失败的任务
    """

    def __init__(self, db: Database, tracker: ProgressTracker) -> None:
        """初始化任务管理器。"""
        self.db = db
        self.tracker = tracker
        # 正在运行的任务（task_id -> asyncio.Task）
        self._running_tasks: dict[int, asyncio.Task] = {}
        # 取消信号
        self._cancel_flags: dict[int, asyncio.Event] = {}
        # 暂停信号
        self._pause_flags: dict[int, asyncio.Event] = {}
        # 串行任务队列：等待启动的 task_id 列表
        self._serial_queue: list[int] = []
        self._lock = asyncio.Lock()
        # 启动时恢复上次未完成的任务
        self._initialized = False

    # ============================
    # 公开 API
    # ============================
    async def initialize(self) -> None:
        """启动时调用：把上次未完成的任务标为 paused（等待用户手动恢复）。"""
        async with self._lock:
            if self._initialized:
                return
            # 任何处于 running 的任务在重启后都视为需要手动恢复
            running = self.db.get_running_tasks()
            for task in running:
                self.db.update_task(task["id"], status=TASK_STATUS_PAUSED)
                self.db.add_event(
                    "Task paused on startup",
                    task_id=task["id"],
                    level=EVENT_LEVEL_INFO,
                    details="Process restarted, task requires manual resume",
                )
            self._initialized = True

    async def create_task(self, url: str, options: dict[str, Any] | None = None) -> int:
        """创建新任务。"""
        url = normalize_url(url)
        opts = TaskOptions.from_dict(options or {})
        task_id = self.db.create_task(
            url, options=opts.to_dict(), download_path=opts.custom_path,
        )
        self.tracker.emit_log(
            task_id, "Task created", f"URL: {url}", level=EVENT_LEVEL_INFO,
        )
        self.tracker.emit_task_created(task_id)
        return task_id

    async def start_task(self, task_id: int) -> bool:
        """启动任务（异步执行，不阻塞）。

        为避免多个下载任务同时竞争带宽/连接/磁盘，启用串行队列：
        - 已有任务在跑时，新任务进入 _serial_queue 等待
        - 当前任务结束后，_run_task 的 finally 会从队列取出下一个任务启动
        """
        async with self._lock:
            if task_id in self._running_tasks or task_id in self._serial_queue:
                logger.warning("Task %d is already running or queued", task_id)
                return False
            task_record = self.db.get_task(task_id)
            if not task_record:
                return False
            if task_record["status"] == TASK_STATUS_RUNNING:
                return False

            # 已有任务在跑，排队等待
            if self._running_tasks:
                self._serial_queue.append(task_id)
                self.db.update_task(task_id, status=TASK_STATUS_PENDING)
                self.db.add_event(
                    "Task queued",
                    task_id=task_id,
                    level=EVENT_LEVEL_INFO,
                    details=f"Waiting for {len(self._running_tasks)} active task(s)",
                )
                logger.info("Task %d queued (waiting for %d active task(s))",
                            task_id, len(self._running_tasks))
                return True

            cancel_event = asyncio.Event()
            pause_event = asyncio.Event()
            self._cancel_flags[task_id] = cancel_event
            self._pause_flags[task_id] = pause_event

            coro = self._run_task(task_id, task_record, cancel_event, pause_event)
            self._running_tasks[task_id] = asyncio.create_task(coro)
            return True

    async def pause_task(self, task_id: int) -> bool:
        """请求暂停任务（运行中的下一步循环会检查）。"""
        async with self._lock:
            if task_id not in self._running_tasks:
                # 任务没在运行，直接改 DB 状态
                task = self.db.get_task(task_id)
                if task and task["status"] in (TASK_STATUS_PENDING, TASK_STATUS_RUNNING):
                    self.db.update_task(task_id, status=TASK_STATUS_PAUSED)
                    self.tracker.emit_task_paused(task_id, reason="user_request")
                return True
            self._pause_flags[task_id].set()
            return True

    async def resume_task(self, task_id: int) -> bool:
        """恢复已暂停的任务（重新启动下载流程）。"""
        return await self.start_task(task_id)

    async def cancel_task(self, task_id: int) -> bool:
        """取消任务。"""
        async with self._lock:
            running = self._running_tasks.get(task_id)
            if running is not None:
                self._cancel_flags[task_id].set()
                self._pause_flags[task_id].set()  # 唤醒等待
                try:
                    await asyncio.wait_for(running, timeout=10.0)
                except (asyncio.TimeoutError, asyncio.CancelledError):
                    running.cancel()
            else:
                # 没在运行，直接改状态
                self.db.update_task(
                    task_id, status=TASK_STATUS_CANCELED,
                    finished_at=datetime.now(timezone.utc).isoformat(),
                )
            self._cancel_flags.pop(task_id, None)
            self._pause_flags.pop(task_id, None)
            self._running_tasks.pop(task_id, None)
            return True

    async def delete_task(self, task_id: int) -> bool:
        """删除任务（先取消再删除）。"""
        await self.cancel_task(task_id)
        self.db.delete_task(task_id)
        return True

    async def retry_failed(self, task_id: int) -> bool:
        """把任务的所有 failed 文件重置为 pending，然后启动。"""
        files = self.db.list_files(task_id, status=FILE_STATUS_FAILED)
        for f in files:
            self.db.reset_file_for_retry(f["id"])
        return await self.start_task(task_id)

    async def retry_file(self, file_id: int) -> bool:
        """重试单个文件。"""
        file_record = self.db.get_file(file_id)
        if not file_record:
            return False
        self.db.reset_file_for_retry(file_id)
        return await self.start_task(file_record["task_id"])

    async def wait_task(self, task_id: int) -> None:
        """等待任务完成（用于测试）。"""
        running = self._running_tasks.get(task_id)
        if running is not None:
            try:
                await running
            except asyncio.CancelledError:
                pass

    async def shutdown(self) -> None:
        """关闭所有运行中的任务。"""
        for task_id in list(self._running_tasks.keys()):
            await self.cancel_task(task_id)

    # ============================
    # 内部：实际下载流程
    # ============================
    async def _run_task(
        self,
        task_id: int,
        task_record: dict[str, Any],
        cancel_event: asyncio.Event,
        pause_event: asyncio.Event,
    ) -> None:
        """任务的实际执行流程。"""
        try:
            options = TaskOptions.from_dict(
                json.loads(task_record.get("options_json") or "{}"),
            )
            url = task_record["url"]
            self.tracker.emit_task_started(task_id)

            # 1. 解析 URL
            self.tracker.emit_log(
                task_id, "Fetching URL", f"Resolving {url}", level=EVENT_LEVEL_INFO,
            )
            normalized_url = normalize_url(url)
            soup = await fetch_page(normalized_url)
            if soup is None:
                error = f"Could not fetch {normalized_url}"
                self.db.update_task(
                    task_id, status=TASK_STATUS_FAILED, error_message=error,
                    finished_at=datetime.now(timezone.utc).isoformat(),
                )
                self.tracker.emit_log(
                    task_id, "Fetch failed", error, level=EVENT_LEVEL_ERROR,
                )
                self.tracker.emit_task_completed(task_id)
                return

            url_type = resolve_url_type(normalized_url)
            url_info = UrlInfo(url=normalized_url, url_type=url_type, soup=soup)

            album_id = get_album_id(normalized_url) if url_info.is_album else None
            album_name = get_album_name(soup)

            # 2. 准备下载目录
            from src.misc.file_utils import format_directory_name

            directory_name = (
                None
                if options.no_album_folder
                else format_directory_name(album_name, album_id)
            )
            download_path = create_download_directory(
                directory_name,
                custom_path=options.custom_path,
                no_download_folder=options.no_download_folder,
            )
            if download_path is None:
                error = f"Could not create download directory: {directory_name}"
                self.db.update_task(
                    task_id, status=TASK_STATUS_FAILED, error_message=error,
                    finished_at=datetime.now(timezone.utc).isoformat(),
                )
                self.tracker.emit_log(
                    task_id, "Download dir error", error, level=EVENT_LEVEL_ERROR,
                )
                self.tracker.emit_task_completed(task_id)
                return

            # 更新 task 的 album 信息与路径
            self.db.update_task(
                task_id,
                album_id=album_id,
                album_name=album_name,
                download_path=download_path,
            )

            # 3. 根据 URL 类型分流
            args = options.to_namespace()
            session_info = SessionInfo(
                args=args, bunkr_status={}, download_path=download_path,
            )

            # 包装 LiveManager
            inner_manager = initialize_managers(disable_ui=True)
            wrapped_manager = self.tracker.wrap(inner_manager, task_id)
            live_manager = wrapped_manager

            if url_info.is_album:
                await self._run_album(
                    task_id, url_info, session_info, live_manager,
                    cancel_event, pause_event, album_id,
                )
            else:
                await self._run_single(
                    task_id, url_info, session_info, live_manager,
                    cancel_event, pause_event,
                )

            # 4. 完成
            self.tracker.emit_task_completed(task_id)

        except asyncio.CancelledError:
            logger.info("Task %d cancelled", task_id)
            self.db.update_task(
                task_id, status=TASK_STATUS_CANCELED,
                finished_at=datetime.now(timezone.utc).isoformat(),
            )
            raise
        except Exception as exc:  # noqa: BLE001
            logger.exception("Task %d failed", task_id)
            self.db.update_task(
                task_id, status=TASK_STATUS_FAILED,
                error_message=str(exc),
                finished_at=datetime.now(timezone.utc).isoformat(),
            )
            self.tracker.emit_log(
                task_id, "Task error", str(exc), level=EVENT_LEVEL_ERROR,
            )
            self.tracker.emit_task_completed(task_id)
        finally:
            await self._on_task_finished(task_id)

    async def _on_task_finished(self, task_id: int) -> None:
        """任务结束后的清理与队列调度。"""
        async with self._lock:
            self._running_tasks.pop(task_id, None)
            self._cancel_flags.pop(task_id, None)
            self._pause_flags.pop(task_id, None)
            next_id = self._serial_queue.pop(0) if self._serial_queue else None

        if next_id is None:
            return

        # 取出下一个排队任务启动（锁外启动以避免长事务）
        next_record = self.db.get_task(next_id)
        if not next_record:
            # 任务记录已被删除，递归处理下一个
            await self._on_task_finished(task_id)
            return
        if next_record["status"] in (TASK_STATUS_CANCELED, TASK_STATUS_FAILED):
            # 已被取消或失败，跳过继续下一个
            self.db.add_event(
                "Task skipped",
                task_id=next_id,
                level=EVENT_LEVEL_INFO,
                details=f"Status: {next_record['status']}",
            )
            await self._on_task_finished(task_id)
            return

        async with self._lock:
            cancel_event = asyncio.Event()
            pause_event = asyncio.Event()
            self._cancel_flags[next_id] = cancel_event
            self._pause_flags[next_id] = pause_event
            coro = self._run_task(next_id, next_record, cancel_event, pause_event)
            self._running_tasks[next_id] = asyncio.create_task(coro)
            self.db.add_event(
                "Task started from queue",
                task_id=next_id,
                level=EVENT_LEVEL_INFO,
            )
            logger.info("Task %d started from serial queue", next_id)

    async def _check_cancel_pause(
        self,
        task_id: int,
        cancel_event: asyncio.Event,
        pause_event: asyncio.Event,
    ) -> bool:
        """检查取消/暂停信号。返回 True 表示应中止。"""
        if cancel_event.is_set():
            return True
        if pause_event.is_set():
            # 暂停：更新 DB 状态并等待取消或重启
            self.db.update_task(task_id, status=TASK_STATUS_PAUSED)
            self.tracker.emit_task_paused(task_id, reason="user_request")
            # 等待直到取消
            await cancel_event.wait()
            return True
        return False

    async def _run_album(
        self,
        task_id: int,
        url_info: UrlInfo,
        session_info: SessionInfo,
        live_manager: Any,
        cancel_event: asyncio.Event,
        pause_event: asyncio.Event,
        album_id: str,
    ) -> None:
        """处理 album 下载（带断点续传）。"""
        identifier = get_identifier(url_info.url, soup=url_info.soup)
        host_page = get_host_page(url_info.url)

        # 1. 提取所有 item 页面
        from src.crawlers.crawler_utils import (
            extract_all_album_item_pages,
            has_cached_item_pages,
        )
        from src.managers.state_manager import load_album_state, save_album_state

        cached_state = load_album_state(session_info.download_path)
        if has_cached_item_pages(cached_state, identifier):
            item_pages = cached_state["item_pages"]
            self.tracker.emit_log(
                task_id, "Using cached album state",
                f"Reusing {len(item_pages)} item page(s)",
                level=EVENT_LEVEL_INFO,
            )
        else:
            if await self._check_cancel_pause(task_id, cancel_event, pause_event):
                return
            item_pages, item_dates = await extract_all_album_item_pages(
                url_info.soup, host_page, url_info.url,
            )
            save_album_state(
                session_info.download_path, identifier, item_pages, {},
            )
            self.tracker.emit_log(
                task_id, "Album crawled",
                f"Found {len(item_pages)} item(s) in album",
                level=EVENT_LEVEL_INFO,
            )

        # 2. 注册所有文件到 DB（pending）
        # 如果 DB 中已有这些文件记录，保持原状（保留之前的 download_link 等）
        existing_files = {
            f["item_url"]: f
            for f in self.db.list_files(task_id, limit=100000)
        }
        for item_url in item_pages:
            if item_url not in existing_files:
                self.tracker.register_file(task_id, item_url)
            else:
                # 已存在：检查是否完成，如果完成则跳过
                pass

        # 3. 决定下载哪些
        # 跳过 completed 的文件
        all_files = self.db.list_files(task_id, limit=100000)
        pending_files = [
            f for f in all_files
            if f["status"] in (FILE_STATUS_PENDING, FILE_STATUS_FAILED)
        ]
        completed_files = [
            f for f in all_files if f["status"] == FILE_STATUS_COMPLETED
        ]
        if completed_files:
            self.tracker.emit_log(
                task_id, "Resume from cache",
                f"{len(completed_files)} file(s) already completed, "
                f"resuming {len(pending_files)} pending",
                level=EVENT_LEVEL_INFO,
            )

        # 4. 构造 AlbumInfo 并执行
        album_info = AlbumInfo(
            album_id=identifier, item_pages=item_pages,
        )
        album_downloader = AlbumDownloader(
            session_info=session_info,
            album_info=album_info,
            live_manager=live_manager,
        )

        # 复用 AlbumDownloader 的内部循环需要改 hooks 才能精准控制，
        # 简化做法：手动循环每个 file，使用 MediaDownloader 下载
        # 这样可以精确控制 file_id 映射
        semaphore = asyncio.Semaphore(session_info.args.connections)
        tasks = []
        for idx, file_record in enumerate(pending_files):
            if cancel_event.is_set():
                break
            tasks.append(self._download_album_file(
                task_id, file_record, session_info, live_manager,
                semaphore, cancel_event, pause_event, idx,
            ))
        if tasks:
            await asyncio.gather(*tasks, return_exceptions=True)

    async def _download_album_file(
        self,
        task_id: int,
        file_record: dict[str, Any],
        session_info: SessionInfo,
        live_manager: Any,
        semaphore: asyncio.Semaphore,
        cancel_event: asyncio.Event,
        pause_event: asyncio.Event,
        index: int,
    ) -> None:
        """下载 album 中的一个文件。"""
        async with semaphore:
            if cancel_event.is_set():
                return
            if await self._check_cancel_pause(task_id, cancel_event, pause_event):
                return
            file_id = file_record["id"]
            item_url = file_record["item_url"]
            self.db.increment_retry(file_id)
            self.tracker.emit_file_started(file_id)

            try:
                # 获取下载信息
                item_soup = await fetch_page(item_url)
                if item_soup is None:
                    raise RuntimeError(f"Failed to fetch {item_url}")

                download_link, filename, item_date = await get_download_info(
                    item_url,
                    item_soup,
                    clean_name=session_info.args.clean_name,
                )
                if not download_link:
                    raise RuntimeError("Could not resolve download URL")

                # 更新 DB 文件名 + 直链
                self.db.upsert_file(
                    task_id, item_url, filename=filename,
                    download_link=download_link,
                    item_date=item_date.isoformat() if item_date else None,
                )
                self.tracker.emit_log(
                    task_id, "Download URL resolved",
                    download_link,
                    level=EVENT_LEVEL_INFO,
                )

                # 跳过规则
                if session_info.args.ignore and any(
                    w in filename for w in session_info.args.ignore
                ):
                    self.tracker.emit_file_skipped(file_id, "matches ignore list")
                    return
                if session_info.args.include and all(
                    w not in filename for w in session_info.args.include
                ):
                    self.tracker.emit_file_skipped(file_id, "no include match")
                    return

                # 检查文件是否已存在
                from src.misc.file_utils import truncate_filename
                from pathlib import Path

                formatted = truncate_filename(filename)
                final_path = Path(session_info.download_path) / formatted
                if final_path.exists():
                    self.tracker.emit_file_skipped(
                        file_id, "file already exists on disk",
                    )
                    return

                # 构造 DownloadInfo + MediaDownloader
                internal_task = live_manager.add_task(current_task=index)
                if isinstance(live_manager, _WrappedLiveManager):
                    live_manager.register_file_id(internal_task, file_id)

                download_info = DownloadInfo(
                    item_url=item_url,
                    download_link=download_link,
                    filename=filename,
                    task=internal_task,
                    item_date=item_date,
                )
                retry_config = RetryConfig(
                    retries=session_info.args.max_retries,
                    has_external_retry=True,
                )
                media_downloader = MediaDownloader(
                    session_info=session_info,
                    download_info=download_info,
                    live_manager=live_manager,
                    retry_config=retry_config,
                )
                failed = await asyncio.to_thread(media_downloader.download)
                if failed:
                    self.tracker.emit_file_failed(
                        file_id, "Max retries reached",
                    )
                else:
                    # 检查磁盘上文件确实存在
                    if final_path.exists():
                        self.db.update_file(
                            file_id, file_size=final_path.stat().st_size,
                        )
                    self.tracker.emit_file_completed(file_id)

            except Exception as exc:  # noqa: BLE001
                logger.exception("File download failed: %s", item_url)
                self.tracker.emit_file_failed(file_id, str(exc))

    async def _run_single(
        self,
        task_id: int,
        url_info: UrlInfo,
        session_info: SessionInfo,
        live_manager: Any,
        cancel_event: asyncio.Event,
        pause_event: asyncio.Event,
    ) -> None:
        """处理单文件下载。"""
        if await self._check_cancel_pause(task_id, cancel_event, pause_event):
            return
        item_url = url_info.url
        file_id = self.tracker.register_file(task_id, item_url)
        self.tracker.emit_file_started(file_id)
        self.db.increment_retry(file_id)

        try:
            download_link, filename, item_date = await get_download_info(
                item_url,
                url_info.soup,
                clean_name=session_info.args.clean_name,
            )
            if not download_link:
                raise RuntimeError("Could not resolve download URL")
            self.db.upsert_file(
                task_id, item_url, filename=filename,
                download_link=download_link,
                item_date=item_date.isoformat() if item_date else None,
            )
            self.tracker.emit_log(
                task_id, "Download URL resolved",
                download_link,
                level=EVENT_LEVEL_INFO,
            )

            from src.misc.file_utils import truncate_filename
            from pathlib import Path

            formatted = truncate_filename(filename)
            final_path = Path(session_info.download_path) / formatted
            if final_path.exists():
                self.tracker.emit_file_skipped(file_id, "file already exists on disk")
                return

            internal_task = live_manager.add_task(current_task=0)
            if isinstance(live_manager, _WrappedLiveManager):
                live_manager.register_file_id(internal_task, file_id)

            download_info = DownloadInfo(
                item_url=item_url,
                download_link=download_link,
                filename=filename,
                task=internal_task,
                item_date=item_date,
            )
            retry_config = RetryConfig(
                retries=session_info.args.max_retries,
                has_external_retry=False,
            )
            media_downloader = MediaDownloader(
                session_info=session_info,
                download_info=download_info,
                live_manager=live_manager,
                retry_config=retry_config,
            )
            failed = await asyncio.to_thread(media_downloader.download)
            if failed:
                self.tracker.emit_file_failed(file_id, "Max retries reached")
            else:
                if final_path.exists():
                    self.db.update_file(
                        file_id, file_size=final_path.stat().st_size,
                    )
                self.tracker.emit_file_completed(file_id)
        except Exception as exc:  # noqa: BLE001
            logger.exception("Single download failed: %s", item_url)
            self.tracker.emit_file_failed(file_id, str(exc))
