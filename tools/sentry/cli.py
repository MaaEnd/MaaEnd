"""统一运行 MaaEnd Sentry 分析报告。"""

from __future__ import annotations

import argparse
import sys
from collections.abc import Callable, Sequence

from . import auto_delivery_report, environment_monitoring_report


ReportMain = Callable[[Sequence[str] | None, str | None], int]


REPORTS: dict[str, tuple[str, ReportMain]] = {
    "auto-delivery": (
        "生成自动送货任务报告",
        auto_delivery_report.main,
    ),
    "environment-monitoring": (
        "生成环境监测任务报告",
        environment_monitoring_report.main,
    ),
}


def create_argument_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="sentry-report",
        description="生成 MaaEnd Sentry 分析报告。",
    )
    subparsers = parser.add_subparsers(dest="report", metavar="REPORT")
    for name, (description, _main) in REPORTS.items():
        subparsers.add_parser(name, help=description, description=description)
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8")

    arguments = list(sys.argv[1:] if argv is None else argv)
    parser = create_argument_parser()
    if not arguments or arguments[0] in {"-h", "--help"}:
        parser.print_help()
        return 0

    report = REPORTS.get(arguments[0])
    if report is None:
        parser.error(f"未知报告类型：{arguments[0]}")

    _description, report_main = report
    try:
        return report_main(
            arguments[1:],
            f"{parser.prog} {arguments[0]}",
        )
    except (FileNotFoundError, RuntimeError, ValueError) as error:
        print(f"错误：{error}", file=sys.stderr)
        return 1
