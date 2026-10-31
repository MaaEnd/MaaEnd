"""AutoCollect 采集路线生成器入口：从项目根目录运行 `uv run autocollect-route`。"""

import argparse
import sys
from collections.abc import Sequence


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
    commands = parser.add_subparsers(dest="command")
    commands.add_parser("generate", help="按路线登记表重新生成调度节点、任务选项与文案")
    return parser


def main(argv: Sequence[str] | None = None) -> None:
    args = build_parser().parse_args(argv)
    if args.command == "generate":
        from tools.AutoCollectRoute import registry

        try:
            written = registry.generate()
        except registry.RegistryError as exc:
            sys.exit(f"路线登记表有误：{exc}")
        for path in written:
            print(path.relative_to(registry.PROJECT_ROOT))
        return

    from tools.AutoCollectRoute import server

    server.run(port=args.port, open_browser=not args.no_browser)


if __name__ == "__main__":
    main()
