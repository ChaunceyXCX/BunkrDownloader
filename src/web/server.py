"""aiohttp Web 服务器。

提供：
    - REST API（/api/...）
    - WebSocket（/ws）
    - 静态文件（/ -> web/）
"""

from __future__ import annotations

import asyncio
import json
import logging
import mimetypes
from pathlib import Path

from aiohttp import WSMsgType, web

from .database import TASK_STATUS_RUNNING, Database, init_db
from .progress_tracker import ProgressTracker
from .task_manager import TaskManager

logger = logging.getLogger(__name__)

# 默认静态文件目录
DEFAULT_STATIC_DIR = Path(__file__).resolve().parent.parent.parent / "web"


# ============================
# Application factory
# ============================
def create_app(
    db: Database,
    task_manager: TaskManager,
    tracker: ProgressTracker,
    static_dir: Path | None = None,
) -> web.Application:
    """构造 aiohttp Application。"""
    app = web.Application()
    app["db"] = db
    app["task_manager"] = task_manager
    app["tracker"] = tracker
    app["static_dir"] = Path(static_dir) if static_dir else DEFAULT_STATIC_DIR

    # REST API
    app.router.add_get("/api/health", handle_health)
    app.router.add_get("/api/tasks", list_tasks)
    app.router.add_post("/api/tasks", create_task)
    app.router.add_get("/api/tasks/{task_id}", get_task)
    app.router.add_delete("/api/tasks/{task_id}", delete_task)
    app.router.add_post("/api/tasks/{task_id}/start", start_task)
    app.router.add_post("/api/tasks/{task_id}/pause", pause_task)
    app.router.add_post("/api/tasks/{task_id}/resume", resume_task)
    app.router.add_post("/api/tasks/{task_id}/cancel", cancel_task)
    app.router.add_post("/api/tasks/{task_id}/retry", retry_failed)
    app.router.add_get("/api/tasks/{task_id}/files", list_files)
    app.router.add_get("/api/tasks/{task_id}/events", list_events)
    app.router.add_post("/api/files/{file_id}/retry", retry_file)
    app.router.add_get("/api/events", list_all_events)
    app.router.add_get("/api/stats", handle_stats)

    # WebSocket
    app.router.add_get("/ws", ws_handler)

    # 静态文件
    static_dir_resolved = app["static_dir"]
    if static_dir_resolved.exists():
        app.router.add_get("/", handle_index)
        app.router.add_get("/index.html", handle_index)
        # 通用静态资源
        app.router.add_get("/{filename:.+\\.(css|js|html|png|jpg|jpeg|gif|svg|ico|woff2?)$}",
                          handle_static)

    return app


# ============================
# 处理器
# ============================
async def handle_health(request: web.Request) -> web.Response:
    """健康检查。"""
    return web.json_response({"status": "ok", "service": "bunkr-web"})


async def handle_stats(request: web.Request) -> web.Response:
    """全局统计。"""
    db: Database = request.app["db"]
    all_tasks = db.list_tasks(limit=1000)
    stats = {
        "total_tasks": len(all_tasks),
        "running": sum(1 for t in all_tasks if t["status"] == TASK_STATUS_RUNNING),
        "pending": sum(1 for t in all_tasks if t["status"] == "pending"),
        "paused": sum(1 for t in all_tasks if t["status"] == "paused"),
        "completed": sum(1 for t in all_tasks if t["status"] == "completed"),
        "failed": sum(1 for t in all_tasks if t["status"] == "failed"),
    }
    return web.json_response(stats)


async def list_tasks(request: web.Request) -> web.Response:
    """列出任务。"""
    db: Database = request.app["db"]
    status = request.query.get("status")
    limit = int(request.query.get("limit", "100"))
    tasks = db.list_tasks(limit=limit, status=status)
    # 附加统计与解析 options
    for task in tasks:
        db.enrich_task_dict(task)
    return web.json_response({"tasks": tasks})


async def create_task(request: web.Request) -> web.Response:
    """创建新任务（支持单个 URL 或批量 URLs）。"""
    data = await request.json()
    raw_url = data.get("url", "")
    options = data.get("options", {})

    # 支持单字符串或字符串数组
    if isinstance(raw_url, str):
        urls = [u.strip() for u in raw_url.splitlines() if u.strip()]
    elif isinstance(raw_url, list):
        urls = [str(u).strip() for u in raw_url if str(u).strip()]
    else:
        urls = []

    if not urls:
        return web.json_response(
            {"error": "url is required"}, status=400,
        )

    task_manager: TaskManager = request.app["task_manager"]
    auto_start = data.get("auto_start", True)
    task_ids = []
    for url in urls:
        task_id = await task_manager.create_task(url, options)
        if auto_start:
            await task_manager.start_task(task_id)
        task_ids.append(task_id)

    return web.json_response(
        {
            "task_ids": task_ids if len(task_ids) > 1 else task_ids[0],
            "count": len(task_ids),
            "status": "pending",
        },
        status=201,
    )


async def get_task(request: web.Request) -> web.Response:
    """获取任务详情。"""
    db: Database = request.app["db"]
    task_id = int(request.match_info["task_id"])
    task = db.to_payload(task_id)
    if not task:
        return web.json_response({"error": "not found"}, status=404)
    return web.json_response(task)


async def delete_task(request: web.Request) -> web.Response:
    """删除任务。"""
    task_manager: TaskManager = request.app["task_manager"]
    task_id = int(request.match_info["task_id"])
    await task_manager.delete_task(task_id)
    return web.json_response({"deleted": task_id})


async def start_task(request: web.Request) -> web.Response:
    """启动任务。"""
    task_manager: TaskManager = request.app["task_manager"]
    task_id = int(request.match_info["task_id"])
    started = await task_manager.start_task(task_id)
    if not started:
        return web.json_response(
            {"error": "task could not be started"}, status=400,
        )
    return web.json_response({"task_id": task_id, "status": "running"})


async def pause_task(request: web.Request) -> web.Response:
    """暂停任务。"""
    task_manager: TaskManager = request.app["task_manager"]
    task_id = int(request.match_info["task_id"])
    await task_manager.pause_task(task_id)
    return web.json_response({"task_id": task_id, "status": "paused"})


async def resume_task(request: web.Request) -> web.Response:
    """恢复任务。"""
    task_manager: TaskManager = request.app["task_manager"]
    task_id = int(request.match_info["task_id"])
    resumed = await task_manager.resume_task(task_id)
    if not resumed:
        return web.json_response(
            {"error": "task could not be resumed"}, status=400,
        )
    return web.json_response({"task_id": task_id, "status": "running"})


async def cancel_task(request: web.Request) -> web.Response:
    """取消任务。"""
    task_manager: TaskManager = request.app["task_manager"]
    task_id = int(request.match_info["task_id"])
    await task_manager.cancel_task(task_id)
    return web.json_response({"task_id": task_id, "status": "canceled"})


async def retry_failed(request: web.Request) -> web.Response:
    """重试任务的所有失败文件。"""
    task_manager: TaskManager = request.app["task_manager"]
    task_id = int(request.match_info["task_id"])
    ok = await task_manager.retry_failed(task_id)
    return web.json_response({"task_id": task_id, "retried": ok})


async def retry_file(request: web.Request) -> web.Response:
    """重试单个文件。"""
    task_manager: TaskManager = request.app["task_manager"]
    file_id = int(request.match_info["file_id"])
    ok = await task_manager.retry_file(file_id)
    return web.json_response({"file_id": file_id, "retried": ok})


async def list_files(request: web.Request) -> web.Response:
    """列出任务下的文件。"""
    db: Database = request.app["db"]
    task_id = int(request.match_info["task_id"])
    status = request.query.get("status")
    limit = int(request.query.get("limit", "1000"))
    files = db.list_files(task_id, status=status, limit=limit)
    return web.json_response({"files": files})


async def list_events(request: web.Request) -> web.Response:
    """列出任务的事件日志。"""
    db: Database = request.app["db"]
    task_id = int(request.match_info["task_id"])
    limit = int(request.query.get("limit", "200"))
    before_id = request.query.get("before_id")
    before_id_int = int(before_id) if before_id else None
    events = db.list_events(task_id=task_id, limit=limit, before_id=before_id_int)
    return web.json_response({"events": events})


async def list_all_events(request: web.Request) -> web.Response:
    """列出全局事件日志。"""
    db: Database = request.app["db"]
    limit = int(request.query.get("limit", "200"))
    before_id = request.query.get("before_id")
    before_id_int = int(before_id) if before_id else None
    events = db.list_events(limit=limit, before_id=before_id_int)
    return web.json_response({"events": events})


# ============================
# WebSocket
# ============================
async def ws_handler(request: web.Request) -> web.WebSocketResponse:
    """WebSocket 入口：客户端连接后实时接收进度事件。"""
    tracker: ProgressTracker = request.app["tracker"]
    db: Database = request.app["db"]

    ws = web.WebSocketResponse(heartbeat=30)
    await ws.prepare(request)
    tracker.add_ws_client(ws)
    logger.info("WebSocket connected: %s", request.remote)

    # 连接后立即发送一份当前状态
    try:
        tasks = db.list_tasks(limit=50)
        await ws.send_str(json.dumps({
            "type": "snapshot",
            "tasks": tasks,
        }, ensure_ascii=False))
    except Exception:  # noqa: BLE001
        logger.exception("Failed to send initial snapshot")

    try:
        async for msg in ws:
            if msg.type == WSMsgType.TEXT:
                # 客户端可以发送 {"action": "ping"} 等命令
                try:
                    payload = json.loads(msg.data)
                    if payload.get("action") == "ping":
                        await ws.send_str(json.dumps({"type": "pong"}))
                except json.JSONDecodeError:
                    await ws.send_str(json.dumps({"type": "error", "message": "invalid json"}))

            elif msg.type == WSMsgType.ERROR:
                logger.warning("WebSocket error: %s", ws.exception())
                break
    finally:
        tracker.remove_ws_client(ws)
        logger.info("WebSocket disconnected: %s", request.remote)

    return ws


# ============================
# 静态资源
# ============================
async def handle_index(request: web.Request) -> web.Response:
    """返回 index.html。"""
    static_dir: Path = request.app["static_dir"]
    index_path = static_dir / "index.html"
    if not index_path.exists():
        return web.Response(
            text="index.html not found. Please ensure the web/ directory is populated.",
            status=404,
            content_type="text/plain",
        )
    return web.Response(
        body=index_path.read_bytes(),
        content_type="text/html",
    )


async def handle_static(request: web.Request) -> web.Response:
    """通用静态资源处理。"""
    static_dir: Path = request.app["static_dir"]
    filename = request.match_info["filename"]
    # 安全检查：禁止路径穿越
    if ".." in filename or filename.startswith("/"):
        return web.Response(status=403)
    file_path = (static_dir / filename).resolve()
    if not str(file_path).startswith(str(static_dir.resolve())):
        return web.Response(status=403)
    if not file_path.exists() or not file_path.is_file():
        return web.Response(status=404)
    content_type, _ = mimetypes.guess_type(str(file_path))
    return web.Response(
        body=file_path.read_bytes(),
        content_type=content_type or "application/octet-stream",
    )


# ============================
# 入口
# ============================
async def run_server(
    host: str = "0.0.0.0",
    port: int = 8765,
    db_path: str | None = None,
    static_dir: Path | None = None,
) -> None:
    """启动 Web 服务器。"""
    db = init_db(db_path)
    tracker = ProgressTracker(db)
    task_manager = TaskManager(db, tracker)
    await task_manager.initialize()

    # 把主事件循环注册到 tracker
    loop = asyncio.get_running_loop()
    tracker.set_event_loop(loop)

    app = create_app(db, task_manager, tracker, static_dir=static_dir)
    runner = web.AppRunner(app)
    await runner.setup()
    site = web.TCPSite(runner, host=host, port=port)
    logger.info("Web server starting on http://%s:%d", host, port)
    await site.start()

    # 永久运行直到取消
    try:
        await asyncio.Event().wait()
    finally:
        await runner.cleanup()
        await task_manager.shutdown()
        db.close()
