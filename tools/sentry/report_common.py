"""Sentry 报告脚本共用的查询和终端输出工具。"""

from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
import sys
import unicodedata
from typing import Any, Sequence, TextIO


EXPLORE_LIMIT = 1_000
DEFAULT_SENTRY_TIMEOUT_SECONDS = 120.0
DEFAULT_RELEASE_DISCOVERY_PERIOD = "90d"
MAAEND_BETA_RELEASE_PATTERN = re.compile(
    r"(?:^|\+)MaaEnd@v(\d+)\.(\d+)\.(\d+)-beta\.(\d+)$"
)


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
    timeout_seconds: float = DEFAULT_SENTRY_TIMEOUT_SECONDS,
) -> dict[str, Any]:
    """执行 Sentry CLI 并解析其 JSON 输出。"""
    if verbose:
        print(f"+ sentry {' '.join(arguments)}", file=sys.stderr)

    try:
        process = subprocess.run(
            [sentry_command, *arguments],
            check=False,
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=timeout_seconds,
        )
    except subprocess.TimeoutExpired as error:
        raise RuntimeError(
            f"sentry {' '.join(arguments)} 执行超过 {timeout_seconds:g} 秒，"
            "请检查网络、认证状态或缩短查询范围。"
        ) from error
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
    timeout_seconds: float = DEFAULT_SENTRY_TIMEOUT_SECONDS,
) -> list[dict[str, Any]]:
    """查询 Sentry spans，并跟随游标返回全部分页结果。"""
    base_arguments = ["explore", target, "--dataset", "spans"]
    for field in fields:
        base_arguments.extend(("--field", field))
    base_arguments.extend(("--query", query))
    if sort:
        base_arguments.extend(("--sort", sort))
    base_arguments.extend(
        (
            "--period",
            period,
            "--limit",
            str(EXPLORE_LIMIT),
            "--fresh",
            "--json",
        )
    )

    rows: list[dict[str, Any]] = []
    cursor: str | None = None
    seen_cursors: set[str] = set()
    while True:
        arguments = [*base_arguments]
        if cursor:
            arguments.extend(("--cursor", cursor))
        result = run_sentry_json(
            sentry_command,
            arguments,
            verbose=verbose,
            timeout_seconds=timeout_seconds,
        )
        data = result.get("data", [])
        if not isinstance(data, list):
            raise RuntimeError("sentry explore 返回的 data 不是数组。")
        rows.extend(data)
        if not result.get("hasMore"):
            return rows

        next_cursor = result.get("nextCursor")
        if not isinstance(next_cursor, str) or not next_cursor:
            raise RuntimeError("sentry explore 声明存在下一页，但未返回有效游标。")
        if next_cursor in seen_cursors:
            raise RuntimeError(f"sentry explore 返回了重复分页游标：{next_cursor}")
        seen_cursors.add(next_cursor)
        cursor = next_cursor


def select_latest_maaend_beta_release(rows: Sequence[dict[str, Any]]) -> str:
    """从 release 聚合结果中选择最新且样本最多的 MaaEnd beta release。"""
    candidates: list[tuple[tuple[int, int, int, int], int, str]] = []
    for row in rows:
        release = row.get("release")
        trace_count = row.get("count_unique(trace)")
        if not isinstance(release, str) or not isinstance(trace_count, int):
            continue

        match = MAAEND_BETA_RELEASE_PATTERN.search(release)
        if match is None:
            continue
        version = tuple(int(part) for part in match.groups())
        candidates.append((version, trace_count, release))

    if not candidates:
        raise RuntimeError(
            "Sentry 中找不到格式为 MaaEnd@vX.Y.Z-beta.N 的 beta release，"
            "请通过 --release 显式指定。"
        )
    return max(candidates)[2]


def resolve_latest_maaend_beta_release(
    sentry_command: str,
    *,
    target: str,
    environment: str,
    verbose: bool = False,
    timeout_seconds: float = DEFAULT_SENTRY_TIMEOUT_SECONDS,
) -> str:
    """查询 Sentry，并解析指定环境中最新的 MaaEnd beta release。"""
    escaped_environment = environment.replace('"', '\\"')
    rows = explore(
        sentry_command,
        target=target,
        period=DEFAULT_RELEASE_DISCOVERY_PERIOD,
        fields=("release", "count_unique(trace)"),
        query=f'environment:"{escaped_environment}"',
        sort="-count_unique(trace)",
        verbose=verbose,
        timeout_seconds=timeout_seconds,
    )
    return select_latest_maaend_beta_release(rows)


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
