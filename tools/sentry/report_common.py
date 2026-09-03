"""Sentry 报告脚本共用的查询和终端输出工具。"""

from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
import sys
import unicodedata
from datetime import datetime
from typing import Any, Sequence, TextIO
from urllib.parse import quote


EXPLORE_LIMIT = 1_000
DEFAULT_SENTRY_TIMEOUT_SECONDS = 120.0
DEFAULT_RELEASE_DISCOVERY_PERIOD = "90d"
MIN_RELEASE_UNIQUE_USERS = 10
SENTRY_RELEASE_API_LIMIT = 100
SUPPORTED_SENTRY_ENVIRONMENTS = frozenset(("beta", "stable"))
MAAEND_RELEASE_PATTERN = re.compile(
    r"(?:^|\+)MaaEnd@v(\d+)\.(\d+)\.(\d+)(?:-(beta|rc)\.(\d+))?$"
)


def normalize_sentry_environment(environment: str) -> str:
    """规范化报告支持的 Sentry environment 名称。"""
    normalized = environment.strip().lower()
    if normalized in SUPPORTED_SENTRY_ENVIRONMENTS:
        return normalized
    raise ValueError(
        f"不支持的 Sentry environment：{environment!r}，"
        "应为 beta 或 stable。"
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


def run_sentry_json_value(
    sentry_command: str,
    arguments: Sequence[str],
    *,
    verbose: bool = False,
    timeout_seconds: float = DEFAULT_SENTRY_TIMEOUT_SECONDS,
) -> Any:
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
    return result


def run_sentry_json(
    sentry_command: str,
    arguments: Sequence[str],
    *,
    verbose: bool = False,
    timeout_seconds: float = DEFAULT_SENTRY_TIMEOUT_SECONDS,
) -> dict[str, Any]:
    """执行 Sentry CLI，并要求其返回 JSON 对象。"""
    result = run_sentry_json_value(
        sentry_command,
        arguments,
        verbose=verbose,
        timeout_seconds=timeout_seconds,
    )
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


def maaend_release_version_key(
    release: str, *, environment: str | None = None
) -> tuple[int, int, int, int, int] | None:
    """解析 MaaEnd 版本排序键，并按需限制发布通道。"""
    if environment is not None:
        environment = normalize_sentry_environment(environment)
    match = MAAEND_RELEASE_PATTERN.search(release)
    if match is None:
        return None

    major, minor, patch, prerelease, prerelease_number = match.groups()
    if environment == "stable":
        if prerelease is not None:
            return None
        prerelease_rank = 2
        prerelease_number = "0"
    elif environment == "beta":
        if prerelease is None or prerelease_number is None:
            return None
        prerelease_rank = {"beta": 0, "rc": 1}[prerelease]
    elif prerelease is None:
        prerelease_rank = 2
        prerelease_number = "0"
    else:
        prerelease_rank = {"beta": 0, "rc": 1}[prerelease]

    return (
        int(major),
        int(minor),
        int(patch),
        prerelease_rank,
        int(prerelease_number),
    )


def select_latest_reported_maaend_release(
    rows: Sequence[dict[str, Any]], *, environment: str | None = None
) -> str | None:
    """选择发布流程已 finalize 并记录 deploy 的最新 MaaEnd release。"""
    if environment is not None:
        environment = normalize_sentry_environment(environment)
    candidates: list[
        tuple[datetime, tuple[int, int, int, int, int], str]
    ] = []
    for row in rows:
        release = row.get("version")
        released_at = row.get("dateReleased")
        deploy_count = row.get("deployCount")
        if (
            not isinstance(release, str)
            or not isinstance(released_at, str)
            or not isinstance(deploy_count, int)
            or deploy_count < 1
        ):
            continue

        version = maaend_release_version_key(release, environment=environment)
        if version is None:
            continue
        try:
            release_time = datetime.fromisoformat(
                released_at.replace("Z", "+00:00")
            )
        except ValueError:
            continue
        candidates.append((release_time, version, release))

    return max(candidates)[2] if candidates else None


def select_latest_maaend_release(
    rows: Sequence[dict[str, Any]], *, environment: str | None = None
) -> str:
    """从 spans 中选择用户数达标的最新 MaaEnd release。"""
    if environment is not None:
        environment = normalize_sentry_environment(environment)
    candidates: list[
        tuple[tuple[int, int, int, int, int], int, int, str]
    ] = []
    for row in rows:
        release = row.get("release")
        user_count = row.get("count_unique(user)")
        trace_count = row.get("count_unique(trace)")
        if (
            not isinstance(release, str)
            or not isinstance(user_count, int)
            or not isinstance(trace_count, int)
            or user_count < MIN_RELEASE_UNIQUE_USERS
        ):
            continue

        version = maaend_release_version_key(release, environment=environment)
        if version is None:
            continue
        candidates.append((version, user_count, trace_count, release))

    if not candidates:
        expected = (
            "MaaEnd@vX.Y.Z"
            if environment == "stable"
            else (
                "MaaEnd@vX.Y.Z-beta.N 或 MaaEnd@vX.Y.Z-rc.N"
                if environment == "beta"
                else "MaaEnd@vX.Y.Z、MaaEnd@vX.Y.Z-beta.N 或 MaaEnd@vX.Y.Z-rc.N"
            )
        )
        selection_scope = (
            f"Sentry 的 {environment} 环境"
            if environment is not None
            else "Sentry"
        )
        raise RuntimeError(
            f"{selection_scope} 中找不到格式为 {expected}、"
            f"且至少有 {MIN_RELEASE_UNIQUE_USERS} 位独立用户的 release，"
            "请通过 --release 显式指定。"
        )
    return max(candidates)[3]


def query_sentry_releases(
    sentry_command: str,
    *,
    target: str,
    verbose: bool = False,
    timeout_seconds: float = DEFAULT_SENTRY_TIMEOUT_SECONDS,
) -> list[dict[str, Any]]:
    """查询项目最新一页的 Sentry release 元数据。"""
    organization, separator, project = target.partition("/")
    if not separator or not organization or not project or "/" in project:
        raise ValueError(f"无效的 Sentry target：{target!r}，应为 <org>/<project>。")

    endpoint = (
        f"projects/{quote(organization, safe='')}/{quote(project, safe='')}/"
        f"releases/?per_page={SENTRY_RELEASE_API_LIMIT}"
    )
    result = run_sentry_json_value(
        sentry_command,
        ("api", endpoint, "--json"),
        verbose=verbose,
        timeout_seconds=timeout_seconds,
    )
    if not isinstance(result, list) or not all(
        isinstance(row, dict) for row in result
    ):
        raise RuntimeError("Sentry release API 返回的 JSON 不是对象数组。")
    return result


def resolve_latest_maaend_release(
    sentry_command: str,
    *,
    target: str,
    environment: str | None = None,
    verbose: bool = False,
    timeout_seconds: float = DEFAULT_SENTRY_TIMEOUT_SECONDS,
) -> str:
    """优先使用发布流程上报的 release，旧版本回退到 spans 样本。"""
    if environment is not None:
        environment = normalize_sentry_environment(environment)
    release_rows = query_sentry_releases(
        sentry_command,
        target=target,
        verbose=verbose,
        timeout_seconds=timeout_seconds,
    )
    reported_release = select_latest_reported_maaend_release(
        release_rows,
        environment=environment,
    )
    if reported_release is not None:
        return reported_release

    query = ""
    if environment is not None:
        escaped_environment = environment.replace('"', '\\"')
        query = f'environment:"{escaped_environment}"'
    rows = explore(
        sentry_command,
        target=target,
        period=DEFAULT_RELEASE_DISCOVERY_PERIOD,
        fields=("release", "count_unique(user)", "count_unique(trace)"),
        query=query,
        sort="-count_unique(user)",
        verbose=verbose,
        timeout_seconds=timeout_seconds,
    )
    return select_latest_maaend_release(rows, environment=environment)


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
