"""BunkrDownloader Web 控制面板启动入口。

用法:
    python3 web_main.py
    python3 web_main.py --host 0.0.0.0 --port 8765
    python3 web_main.py --db /path/to/state.db
"""

from __future__ import annotations

import argparse
import asyncio
import logging
import signal
from pathlib import Path

# 顶层 import（pylint C0415：避免在函数内 import）
from aiohttp import web

from src.web.database import get_default_db_path, init_db
from src.web.progress_tracker import ProgressTracker
from src.web.server import DEFAULT_STATIC_DIR, create_app
from src.web.task_manager import TaskManager


def parse_args() -> argparse.Namespace:
    """解析命令行参数。"""
    parser = argparse.ArgumentParser(
        description="BunkrDownloader Web Control Panel",
    )
    parser.add_argument(
        "--host", type=str, default="0.0.0.0",
        help="监听地址（默认 0.0.0.0）",
    )
    parser.add_argument(
        "--port", type=int, default=8765,
        help="监听端口（默认 8765）",
    )
    parser.add_argument(
        "--db", type=str, default=None,
        help="SQLite 数据库路径（默认 ~/.bunkr_downloader/state.db）",
    )
    parser.add_argument(
        "--static-dir", type=str, default=None,
        help=f"前端静态文件目录（默认 {DEFAULT_STATIC_DIR}）",
    )
    parser.add_argument(
        "--log-level", type=str, default="INFO",
        choices=["DEBUG", "INFO", "WARNING", "ERROR"],
    )
    return parser.parse_args()


def _build_banner(host: str, port: int, db_path: str, static_dir: Path | None) -> str:
    """构建启动横幅。"""
    static_label = str(static_dir or DEFAULT_STATIC_DIR)[:40]
    return (
        "\n"
        "╔════════════════════════════════════════════════════╗\n"
        "║  BunkrDownloader Web Control Panel                ║\n"
        "╠════════════════════════════════════════════════════╣\n"
        f"║  URL:    http://{host}:{port:<24}          ║\n"
        f"║  DB:     {db_path[:40]:40s}  ║\n"
        f"║  Static: {static_label:40s}  ║\n"
        "╚════════════════════════════════════════════════════╝\n"
    )


def main() -> None:
    """主入口。"""
    args = parse_args()
    logging.basicConfig(
        level=getattr(logging, args.log_level),
        format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    )

    db_path = args.db or str(get_default_db_path())
    static_dir = Path(args.static_dir) if args.static_dir else None

    print(_build_banner(args.host, args.port, db_path, static_dir))

    # 优雅退出
    loop = asyncio.new_event_loop()
    asyncio.set_event_loop(loop)

    stop_event = asyncio.Event()

    def _signal_handler() -> None:
        print("\nShutting down...")
        stop_event.set()

    for sig in (signal.SIGINT, signal.SIGTERM):
        try:
            loop.add_signal_handler(sig, _signal_handler)
        except NotImplementedError:
            # Windows 不支持 add_signal_handler
            pass

    async def _run() -> None:
        db = init_db(db_path)
        tracker = ProgressTracker(db)
        task_manager = TaskManager(db, tracker)
        await task_manager.initialize()
        tracker.set_event_loop(asyncio.get_running_loop())

        app = create_app(db, task_manager, tracker, static_dir=static_dir)
        runner = web.AppRunner(app)
        await runner.setup()
        site = web.TCPSite(runner, args.host, args.port)
        await site.start()
        print(f"Web UI available at: http://localhost:{args.port}")

        try:
            await stop_event.wait()
        finally:
            print("Cleaning up...")
            await task_manager.shutdown()
            await runner.cleanup()
            db.close()

    try:
        loop.run_until_complete(_run())
    except KeyboardInterrupt:
        pass
    finally:
        loop.close()


if __name__ == "__main__":
    main()
