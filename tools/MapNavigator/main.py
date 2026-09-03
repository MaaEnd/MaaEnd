# /// script
# requires-python = ">=3.10"
# dependencies = [
#   "fastapi>=0.129,<1.0",
#   "maafw>=5.13.0b4,<6.0",
#   "numpy",
#   "pydantic",
#   "pynput>=1.7.0",
#   "pyperclip",
#   "starlette",
#   "uvicorn>=0.41,<1.0",
#   "websockets",
# ]
# ///
"""MapNavigator 入口：拉起 Web 后端（web/serve.py）。

从项目根目录运行 `uv run map-navigator` 时使用 pyproject.toml 中的依赖；兼容的
`uv run main.py` 脚本模式只读取本文件的 PEP 723 声明。两处依赖须与
web/serve.py 保持一致。

本入口只解析启动参数并写入服务使用的环境配置；端口绑定与浏览器打开仍由
serve.py 负责，端口被占用时只有绑定方知道最终端口，这里不能提前猜测。
"""

from __future__ import annotations

import argparse
import os
import runpy
from pathlib import Path
from typing import Sequence

SERVE_PY = Path(__file__).resolve().parent / "web" / "serve.py"


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
        prog="map-navigator",
        description="启动 MaaEnd MapNavigator 本地 Web 工具",
    )
    parser.add_argument(
        "--port",
        type=_port,
        metavar="PORT",
        help="首选监听端口（默认读取 MAPNAV_PORT，未设置时为 8770）",
    )
    parser.add_argument(
        "--no-browser",
        action="store_true",
        help="启动服务但不自动打开浏览器",
    )
    return parser


def main(argv: Sequence[str] | None = None) -> None:
    args = build_parser().parse_args(argv)
    if args.port is not None:
        os.environ["MAPNAV_PORT"] = str(args.port)
    if args.no_browser:
        os.environ["MAPNAV_NO_BROWSER"] = "1"

    # run_name="__main__" 触发 serve.py 的启动块 (选端口 + 开浏览器 + uvicorn, 仅监听 127.0.0.1)
    runpy.run_path(str(SERVE_PY), run_name="__main__")


if __name__ == "__main__":
    main()
