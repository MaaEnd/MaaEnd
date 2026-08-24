"""根据 Sentry spans 生成环境监测任务失败情况报告。

执行次数按包含 ``GoTo*Move`` span 的唯一 trace 统计。传送失败取自路线专属的
``QuickTeleport`` span，移动失败取自失败的移动 span；扫描失败归属于同一 trace
中时间最近的前置 ``GoTo*Move`` span。同一观察点、同一 trace 中的同类重复 span
只计数一次，总失败按三类失败 trace 的并集统计。
"""

from __future__ import annotations

import argparse
import csv
import json
import os
import re
import shutil
import subprocess
import sys
import unicodedata
from collections import defaultdict
from dataclasses import asdict, dataclass
from datetime import datetime
from pathlib import Path
from typing import Any, Iterable, Iterator, Sequence, TextIO


MOVE_DESCRIPTION_PATTERN = re.compile(r"^GoTo(.+)Move$")
QUICK_TELEPORT_DESCRIPTION_PATTERN = re.compile(
    r"^(.+?)QuickTeleport(?:Select|Done)?$"
)
EXPLORE_LIMIT = 1_000
DEFAULT_ROUTES_PATH = (
    Path(__file__).resolve().parents[1]
    / "pipeline-generate"
    / "EnvironmentMonitoring"
    / "routes.json"
)

FailurePair = tuple[str, str]


@dataclass(frozen=True)
class TimedSpan:
    route_id: str
    trace: str
    timestamp: datetime


@dataclass(frozen=True)
class ScanFailure:
    trace: str
    timestamp: datetime


@dataclass(frozen=True)
class ReportRow:
    observation_point: str
    route_id: str
    executions: int
    teleport_failures: int
    move_failures: int
    scan_failures: int
    total_failures: int
    failure_rate: float | None


def parse_timestamp(value: str) -> datetime:
    """解析 Sentry 返回的 ISO 8601 时间戳。"""
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


def route_id_from_description(description: str) -> str | None:
    match = MOVE_DESCRIPTION_PATTERN.fullmatch(description)
    return match.group(1) if match else None


def route_id_from_quick_teleport_description(description: str) -> str | None:
    """从路线专属快捷传送节点名中提取观察点 ID。"""
    match = QUICK_TELEPORT_DESCRIPTION_PATTERN.fullmatch(description)
    return match.group(1) if match else None


def batched(values: Sequence[str], size: int) -> Iterator[Sequence[str]]:
    for start in range(0, len(values), size):
        yield values[start : start + size]


def extend_period_for_trace_lookup(period: str) -> str:
    """把相对统计窗口向前扩一小时，以覆盖位于窗口边界外的前置移动。"""
    match = re.fullmatch(r"(\d+)([hdw])", period)
    if not match:
        return period

    amount = int(match.group(1))
    unit = match.group(2)
    hours_per_unit = {"h": 1, "d": 24, "w": 24 * 7}
    return f"{amount * hours_per_unit[unit] + 1}h"


def resolve_sentry_command() -> str:
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


def load_route_names(path: Path) -> dict[str, str]:
    with path.open(encoding="utf-8") as file:
        routes = json.load(file)
    return {str(route["Id"]): str(route["Name"]) for route in routes}


def execution_counts_from_rows(rows: Iterable[dict[str, Any]]) -> dict[str, int]:
    counts: dict[str, int] = {}
    for row in rows:
        route_id = route_id_from_description(str(row["span.description"]))
        if route_id:
            counts[route_id] = int(row["count_unique(trace)"])
    return counts


def failure_pairs_from_rows(rows: Iterable[dict[str, Any]]) -> set[FailurePair]:
    pairs: set[FailurePair] = set()
    for row in rows:
        route_id = route_id_from_description(str(row["span.description"]))
        if route_id:
            pairs.add((route_id, str(row["trace"])))
    return pairs


def teleport_failure_pairs_from_rows(
    rows: Iterable[dict[str, Any]],
    route_ids: set[str],
) -> set[FailurePair]:
    """提取传送失败，并过滤不属于环境监测观察点的公共传送节点。"""
    pairs: set[FailurePair] = set()
    for row in rows:
        route_id = route_id_from_quick_teleport_description(
            str(row["span.description"])
        )
        if route_id in route_ids:
            pairs.add((route_id, str(row["trace"])))
    return pairs


def scan_failures_from_rows(rows: Iterable[dict[str, Any]]) -> list[ScanFailure]:
    return [
        ScanFailure(
            trace=str(row["trace"]),
            timestamp=parse_timestamp(str(row["timestamp"])),
        )
        for row in rows
    ]


def timed_moves_from_rows(rows: Iterable[dict[str, Any]]) -> list[TimedSpan]:
    moves: list[TimedSpan] = []
    for row in rows:
        route_id = route_id_from_description(str(row["span.description"]))
        if route_id:
            moves.append(
                TimedSpan(
                    route_id=route_id,
                    trace=str(row["trace"]),
                    timestamp=parse_timestamp(str(row["timestamp"])),
                )
            )
    return moves


def correlate_scan_failures(
    scan_failures: Iterable[ScanFailure],
    moves: Iterable[TimedSpan],
) -> tuple[set[FailurePair], int]:
    moves_by_trace: dict[str, list[TimedSpan]] = defaultdict(list)
    for move in moves:
        moves_by_trace[move.trace].append(move)
    for trace_moves in moves_by_trace.values():
        trace_moves.sort(key=lambda move: move.timestamp)

    pairs: set[FailurePair] = set()
    unmatched = 0
    for failure in scan_failures:
        preceding_move = next(
            (
                move
                for move in reversed(moves_by_trace.get(failure.trace, []))
                if move.timestamp <= failure.timestamp
            ),
            None,
        )
        if preceding_move is None:
            unmatched += 1
            continue
        pairs.add((preceding_move.route_id, failure.trace))
    return pairs, unmatched


def count_pairs_by_route(pairs: Iterable[FailurePair]) -> dict[str, int]:
    counts: dict[str, int] = defaultdict(int)
    for route_id, _trace in pairs:
        counts[route_id] += 1
    return dict(counts)


def build_report(
    route_names: dict[str, str],
    execution_counts: dict[str, int],
    teleport_failure_pairs: set[FailurePair],
    move_failure_pairs: set[FailurePair],
    scan_failure_pairs: set[FailurePair],
) -> list[ReportRow]:
    teleport_failure_counts = count_pairs_by_route(teleport_failure_pairs)
    move_failure_counts = count_pairs_by_route(move_failure_pairs)
    scan_failure_counts = count_pairs_by_route(scan_failure_pairs)
    all_failure_pairs = (
        teleport_failure_pairs | move_failure_pairs | scan_failure_pairs
    )
    total_failure_counts = count_pairs_by_route(all_failure_pairs)

    rows = []
    for route_id in route_names.keys() | execution_counts.keys():
        executions = execution_counts.get(route_id, 0)
        total_failures = total_failure_counts.get(route_id, 0)
        rows.append(
            ReportRow(
                observation_point=route_names.get(route_id, route_id),
                route_id=route_id,
                executions=executions,
                teleport_failures=teleport_failure_counts.get(route_id, 0),
                move_failures=move_failure_counts.get(route_id, 0),
                scan_failures=scan_failure_counts.get(route_id, 0),
                total_failures=total_failures,
                failure_rate=(total_failures / executions if executions else None),
            )
        )

    return sorted(
        rows,
        key=lambda row: (
            -(row.failure_rate if row.failure_rate is not None else -1),
            -row.executions,
            row.observation_point,
        ),
    )


def collect_report(
    *,
    sentry_command: str,
    release: str | None,
    environment: str,
    target: str,
    period: str,
    routes_path: Path,
    trace_batch_size: int,
    verbose: bool,
    quiet: bool,
) -> tuple[list[ReportRow], int]:
    if release:
        escaped_release = release.replace('"', '\\"')
        scope_filter = f'release:"{escaped_release}"'
    else:
        scope_filter = f"environment:{environment}"

    move_filter = f"{scope_filter} span.description:GoTo*Move"
    teleport_failure_filter = (
        f"{scope_filter} task:EnvironmentMonitoring "
        "span.description:*QuickTeleport* span.status:internal_error"
    )
    move_failure_filter = f"{move_filter} span.status:internal_error"
    scan_failure_filter = (
        f"{scope_filter} "
        "span.description:EnvironmentMonitoringCameraScan "
        "span.status:internal_error"
    )

    route_names = load_route_names(routes_path)

    show_progress("[1/5] 查询观察点执行次数", quiet=quiet)
    execution_rows = explore(
        sentry_command,
        target=target,
        period=period,
        fields=("span.description", "count_unique(trace)"),
        query=move_filter,
        sort="-count_unique(trace)",
        verbose=verbose,
    )
    show_progress("[2/5] 查询传送失败", quiet=quiet)
    teleport_failure_rows = explore(
        sentry_command,
        target=target,
        period=period,
        fields=("timestamp", "trace", "span.description"),
        query=teleport_failure_filter,
        sort="timestamp",
        verbose=verbose,
    )
    show_progress("[3/5] 查询移动失败", quiet=quiet)
    move_failure_rows = explore(
        sentry_command,
        target=target,
        period=period,
        fields=("timestamp", "trace", "span.description"),
        query=move_failure_filter,
        sort="timestamp",
        verbose=verbose,
    )
    show_progress("[4/5] 查询扫描失败", quiet=quiet)
    scan_failure_rows = explore(
        sentry_command,
        target=target,
        period=period,
        fields=("timestamp", "trace", "span.description"),
        query=scan_failure_filter,
        sort="timestamp",
        verbose=verbose,
    )

    scan_failures = scan_failures_from_rows(scan_failure_rows)
    trace_ids = sorted({failure.trace for failure in scan_failures})
    all_moves: list[TimedSpan] = []
    trace_lookup_period = extend_period_for_trace_lookup(period)
    trace_batches = list(batched(trace_ids, trace_batch_size))
    for index, trace_batch in enumerate(trace_batches, start=1):
        show_batch_progress(index, len(trace_batches), quiet=quiet)
        trace_filter = f"trace:[{','.join(trace_batch)}]"
        move_rows = explore(
            sentry_command,
            target=target,
            period=trace_lookup_period,
            fields=("timestamp", "trace", "span.description"),
            query=f"{move_filter} {trace_filter}",
            sort="timestamp",
            verbose=verbose,
        )
        all_moves.extend(timed_moves_from_rows(move_rows))
    finish_batch_progress(bool(trace_batches), quiet=quiet)

    scan_failure_pairs, unmatched = correlate_scan_failures(
        scan_failures,
        all_moves,
    )
    report = build_report(
        route_names,
        execution_counts_from_rows(execution_rows),
        teleport_failure_pairs_from_rows(
            teleport_failure_rows,
            set(route_names),
        ),
        failure_pairs_from_rows(move_failure_rows),
        scan_failure_pairs,
    )
    return report, unmatched


def format_rate(rate: float | None) -> str:
    return "暂无样本" if rate is None else f"{rate:.1%}"


def show_progress(message: str, *, quiet: bool) -> None:
    """向标准错误输出阶段进度，不污染报告正文。"""
    if not quiet:
        print(message, file=sys.stderr, flush=True)


def show_batch_progress(current: int, total: int, *, quiet: bool) -> None:
    """在终端同一行刷新 trace 批量查询进度。"""
    if quiet:
        return
    width = 24
    completed = round(width * current / total)
    bar = "█" * completed + "░" * (width - completed)
    print(
        f"\r[5/5] 关联扫描失败 trace [{bar}] {current}/{total}",
        end="",
        file=sys.stderr,
        flush=True,
    )


def finish_batch_progress(has_batches: bool, *, quiet: bool) -> None:
    """结束同一行进度显示。"""
    if has_batches and not quiet:
        print(file=sys.stderr, flush=True)


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


def write_console(rows: Sequence[ReportRow], output: TextIO) -> None:
    headers = (
        "观察点",
        "执行",
        "传送失败",
        "移动失败",
        "扫描失败",
        "总失败",
        "失败率",
    )
    values = [
        (
            row.observation_point,
            str(row.executions),
            str(row.teleport_failures),
            str(row.move_failures),
            str(row.scan_failures),
            str(row.total_failures),
            format_rate(row.failure_rate),
        )
        for row in rows
    ]
    widths = [
        max(display_width(value) for value in (header, *(row[index] for row in values)))
        for index, header in enumerate(headers)
    ]

    def border(left: str, middle: str, right: str) -> str:
        return left + middle.join("─" * (width + 2) for width in widths) + right

    def table_row(row: Sequence[str], *, header: bool = False) -> str:
        cells = [
            pad_display(value, widths[index], align_right=index > 0 and not header)
            for index, value in enumerate(row)
        ]
        return "│ " + " │ ".join(cells) + " │"

    print(border("┌", "┬", "┐"), file=output)
    print(table_row(headers, header=True), file=output)
    print(border("├", "┼", "┤"), file=output)
    for row in values:
        print(table_row(row), file=output)
    print(border("└", "┴", "┘"), file=output)


def write_markdown(rows: Sequence[ReportRow], output: TextIO) -> None:
    print(
        "| 观察点 | 执行 | 传送失败 | 移动失败 | 扫描失败 | 总失败 | 失败率 |",
        file=output,
    )
    print("|---|---:|---:|---:|---:|---:|---:|", file=output)
    for row in rows:
        name = row.observation_point.replace("|", "\\|")
        print(
            f"| {name} | {row.executions} | {row.teleport_failures} | "
            f"{row.move_failures} | {row.scan_failures} | {row.total_failures} | "
            f"{format_rate(row.failure_rate)} |",
            file=output,
        )


def write_csv(rows: Sequence[ReportRow], output: TextIO) -> None:
    writer = csv.DictWriter(output, fieldnames=ReportRow.__dataclass_fields__)
    writer.writeheader()
    for row in rows:
        writer.writerow(asdict(row))


def write_json(rows: Sequence[ReportRow], output: TextIO) -> None:
    json.dump(
        [asdict(row) for row in rows],
        output,
        ensure_ascii=False,
        indent=2,
    )
    output.write("\n")


def create_argument_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--release",
        help="精确的 Sentry release 名称；指定后覆盖默认 channel 筛选",
    )
    parser.add_argument(
        "--environment",
        default="beta",
        help="未指定 --release 时使用的 Sentry environment（默认：beta）",
    )
    parser.add_argument("--target", default="maaend/rust", help="<org>/<project>")
    parser.add_argument(
        "--period",
        default="24h",
        help='查询范围，例如 "24h"、"7d" 或 "2026-08-23..2026-08-24"',
    )
    parser.add_argument(
        "--format",
        choices=("console", "markdown", "csv", "json"),
        default="console",
        help="输出格式",
    )
    parser.add_argument(
        "--routes",
        type=Path,
        default=DEFAULT_ROUTES_PATH,
        help="EnvironmentMonitoring routes.json 路径",
    )
    parser.add_argument(
        "--trace-batch-size",
        type=int,
        default=20,
        help="每次批量查询的 trace 数量",
    )
    parser.add_argument("--verbose", action="store_true", help="输出 sentry 查询命令")
    parser.add_argument("--quiet", action="store_true", help="不输出查询进度")
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8")

    arguments = create_argument_parser().parse_args(argv)
    if arguments.trace_batch_size < 1:
        raise ValueError("--trace-batch-size 必须大于 0。")
    if not arguments.routes.is_file():
        raise FileNotFoundError(f"找不到环境监测路线配置：{arguments.routes}")

    report, unmatched = collect_report(
        sentry_command=resolve_sentry_command(),
        release=arguments.release,
        environment=arguments.environment,
        target=arguments.target,
        period=arguments.period,
        routes_path=arguments.routes,
        trace_batch_size=arguments.trace_batch_size,
        verbose=arguments.verbose,
        quiet=arguments.quiet,
    )
    if unmatched:
        print(
            f"警告：{unmatched} 个扫描失败 span 未找到前置 GoTo*Move，"
            "未计入观察点扫描失败。",
            file=sys.stderr,
        )

    writers = {
        "console": write_console,
        "markdown": write_markdown,
        "csv": write_csv,
        "json": write_json,
    }
    writers[arguments.format](report, sys.stdout)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (FileNotFoundError, RuntimeError, ValueError) as error:
        print(f"错误：{error}", file=sys.stderr)
        raise SystemExit(1) from error
