"""Sentry 报告脚本共用的查询和终端输出工具。"""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
import unicodedata
from typing import Any, Sequence, TextIO


EXPLORE_LIMIT = 1_000


def resolve_sentry_command() -> str:
    """查找当前系统中可执行的 sentry 命令。"""
    candidates = (
        ("sentry.cmd", "sentry.exe", "sentry")
        if os.name == "nt"
        else ("sentry",)
    )
    for candidate in candidates:
        command = shutil.which(candidate)
        if command:
            return command
    raise RuntimeError(
        "未找到 sentry 命令。请先安装 Sentry CLI，并确认 sentry --version 可运行。"
    )


def run_sentry_json(
    sentry_command: str,
    arguments: Sequence[str],
    *,
    verbose: bool = False,
) -> dict[str, Any]:
    """执行 Sentry CLI 并解析其 JSON 输出。"""
    if verbose:
        print(f"+ sentry {' '.join(arguments)}", file=sys.stderr)

    process = subprocess.run(
        [sentry_command, *arguments],
        check=False,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    if process.returncode != 0:
        diagnostic = process.stderr.strip() or process.stdout.strip()
        raise RuntimeError(
            f"sentry {' '.join(arguments)} 执行失败"
            f"（退出码 {process.returncode}）：\n{diagnostic}"
        )

    try:
        result = json.loads(process.stdout)
    except json.JSONDecodeError as error:
        raise RuntimeError(
            "sentry CLI 未返回有效 JSON：\n"
            f"stdout:\n{process.stdout}\n"
            f"stderr:\n{process.stderr}"
        ) from error
    if not isinstance(result, dict):
        raise RuntimeError("sentry CLI 返回了非对象 JSON。")
    return result


def explore(
    sentry_command: str,
    *,
    target: str,
    period: str,
    fields: Sequence[str],
    query: str,
    sort: str | None = None,
    verbose: bool = False,
) -> list[dict[str, Any]]:
    """查询 Sentry spans，并拒绝静默截断超过行数限制的结果。"""
    arguments = ["explore", target, "--dataset", "spans"]
    for field in fields:
        arguments.extend(("--field", field))
    arguments.extend(("--query", query))
    if sort:
        arguments.extend(("--sort", sort))
    arguments.extend(
        (
            "--period",
            period,
            "--limit",
            str(EXPLORE_LIMIT),
            "--fresh",
            "--json",
        )
    )

    result = run_sentry_json(sentry_command, arguments, verbose=verbose)
    if result.get("hasMore"):
        raise RuntimeError(
            f"查询结果超过 sentry explore 单次 {EXPLORE_LIMIT} 行限制。"
            f"请缩短 --period 后重试。查询：{query}"
        )
    data = result.get("data", [])
    if not isinstance(data, list):
        raise RuntimeError("sentry explore 返回的 data 不是数组。")
    return data


def format_rate(rate: float | None) -> str:
    """把小数失败率格式化为百分比。"""
    return "暂无样本" if rate is None else f"{rate:.1%}"


def show_progress(message: str, *, quiet: bool) -> None:
    """向标准错误输出阶段进度，不污染报告正文。"""
    if not quiet:
        print(message, file=sys.stderr, flush=True)


def display_width(value: str) -> int:
    """计算终端中的 Unicode 显示宽度，中文等宽字符按两列计算。"""
    width = 0
    for character in value:
        if unicodedata.combining(character):
            continue
        width += 2 if unicodedata.east_asian_width(character) in {"F", "W"} else 1
    return width


def pad_display(value: str, width: int, *, align_right: bool = False) -> str:
    """按照终端显示宽度填充文本。"""
    padding = " " * (width - display_width(value))
    return f"{padding}{value}" if align_right else f"{value}{padding}"


def write_console_table(
    headers: Sequence[str],
    values: Sequence[Sequence[str]],
    output: TextIO,
    *,
    right_aligned: set[int] | None = None,
) -> None:
    """输出处理中日韩宽字符的 Unicode 框线表格。"""
    alignments = right_aligned or set()
    widths = [
        max(display_width(value) for value in (header, *(row[index] for row in values)))
        for index, header in enumerate(headers)
    ]

    def border(left: str, middle: str, right: str) -> str:
        return left + middle.join("─" * (width + 2) for width in widths) + right

    def table_row(row: Sequence[str], *, header: bool = False) -> str:
        cells = [
            pad_display(
                value,
                widths[index],
                align_right=index in alignments and not header,
            )
            for index, value in enumerate(row)
        ]
        return "│ " + " │ ".join(cells) + " │"

    print(border("┌", "┬", "┐"), file=output)
    print(table_row(headers, header=True), file=output)
    print(border("├", "┼", "┤"), file=output)
    for row in values:
        print(table_row(row), file=output)
    print(border("└", "┴", "┘"), file=output)
