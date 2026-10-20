"""AutoCollect 采集路线生成器入口：从项目根目录运行 `uv run autocollect-route`。"""

from __future__ import annotations

import argparse
from typing import Sequence


def _port(value: str) -> int:
    try:
        port = int(value)
    except ValueError as exc:
        raise argparse.ArgumentTypeError("端口必须是整数") from exc
    if not 1 <= port <= 65535:
        raise argparse.ArgumentTypeError("端口必须在 1 到 65535 之间")
    return port


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="autocollect-route",
        description="启动 AutoCollect 采集路线生成器（本地 Web 页面）",
    )
    parser.add_argument("--port", type=_port, metavar="PORT", help="首选监听端口（默认 8780，被占用时顺延）")
    parser.add_argument("--no-browser", action="store_true", help="启动服务但不自动打开浏览器")
    return parser


def main(argv: Sequence[str] | None = None) -> None:
    args = build_parser().parse_args(argv)
    from tools.AutoCollectRoute import server

    server.run(port=args.port, open_browser=not args.no_browser)


if __name__ == "__main__":
    main()
