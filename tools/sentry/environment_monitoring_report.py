"""根据 Sentry spans 生成环境监测任务失败情况报告。

默认分析 Sentry 最新发布的 MaaEnd release。执行次数按包含 ``GoTo*Move`` span 的
唯一 trace 统计。传送失败取自路线专属的 ``QuickTeleport`` span，移动失败取自失败的
移动 span；扫描失败归属于同一 trace 中时间最近的前置 ``GoTo*Move`` span。同一
观察点、同一 trace 中的同类重复 span 只计数一次，总失败按三类失败 trace 的并集统计。
"""

from __future__ import annotations

import argparse
import csv
import json
import math
import re
import sys
from collections import defaultdict
from dataclasses import asdict, dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any, Iterable, Iterator, Sequence, TextIO

try:
    from .report_common import (
        DEFAULT_SENTRY_TIMEOUT_SECONDS,
        explore,
        format_rate,
        normalize_sentry_environment,
        resolve_latest_maaend_release,
        resolve_sentry_command,
        show_progress,
        write_console_table,
    )
except ImportError:
    from report_common import (
        DEFAULT_SENTRY_TIMEOUT_SECONDS,
        explore,
        format_rate,
        normalize_sentry_environment,
        resolve_latest_maaend_release,
        resolve_sentry_command,
        show_progress,
        write_console_table,
    )


MOVE_DESCRIPTION_PATTERN = re.compile(r"^GoTo(.+)Move$")
QUICK_TELEPORT_DESCRIPTION_PATTERN = re.compile(
    r"^(.+?)QuickTeleport(?:Select|Done)?$"
)
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
    """把相对或日期统计窗口向前扩一小时，覆盖边界外的前置移动。"""
    match = re.fullmatch(r"(\d+)([hdw])", period)
    if match:
        amount = int(match.group(1))
        unit = match.group(2)
        hours_per_unit = {"h": 1, "d": 24, "w": 24 * 7}
        return f"{amount * hours_per_unit[unit] + 1}h"

    absolute_match = re.fullmatch(
        r"(\d{4}-\d{2}-\d{2})\.\.(\d{4}-\d{2}-\d{2})",
        period,
    )
    if not absolute_match:
        return period
    try:
        start = datetime.strptime(absolute_match.group(1), "%Y-%m-%d").replace(
            tzinfo=timezone.utc
        )
        datetime.strptime(absolute_match.group(2), "%Y-%m-%d")
    except ValueError:
        return period

    extended_start = (start - timedelta(hours=1)).isoformat(timespec="seconds")
    extended_start = extended_start.replace("+00:00", "Z")
    return f"{extended_start}..{absolute_match.group(2)}"


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
    environment: str | None,
    target: str,
    period: str,
    routes_path: Path,
    trace_batch_size: int,
    timeout_seconds: float,
    verbose: bool,
    quiet: bool,
) -> tuple[list[ReportRow], int]:
    if environment is not None:
        environment = normalize_sentry_environment(environment)
    if release is None:
        release_description = (
            f"{environment} 环境的最新 MaaEnd release"
            if environment is not None
            else "最新发布的 MaaEnd release"
        )
        show_progress(
            f"[0/5] 自动选择 {release_description}",
            quiet=quiet,
        )
        release = resolve_latest_maaend_release(
            sentry_command,
            target=target,
            environment=environment,
            verbose=verbose,
            timeout_seconds=timeout_seconds,
        )
        show_progress(f"使用 Sentry release：{release}", quiet=quiet)
    escaped_release = release.replace('"', '\\"')
    scope_filter = f'release:"{escaped_release}"'
    if environment is not None:
        escaped_environment = environment.replace('"', '\\"')
        scope_filter = f'{scope_filter} environment:"{escaped_environment}"'

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
        timeout_seconds=timeout_seconds,
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
        timeout_seconds=timeout_seconds,
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
        timeout_seconds=timeout_seconds,
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
        timeout_seconds=timeout_seconds,
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
            timeout_seconds=timeout_seconds,
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
    write_console_table(
        headers,
        values,
        output,
        right_aligned=set(range(1, len(headers))),
    )


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


def create_argument_parser(prog: str | None = None) -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog=prog, description=__doc__)
    parser.add_argument(
        "--release",
        help=(
            "精确的 Sentry release 名称；未指定时选择最新发布版本，"
            "找不到发布记录时从用户样本回退"
        ),
    )
    parser.add_argument(
        "--environment",
        choices=("stable", "beta"),
        help="可选的 environment 过滤；stable 选择正式版，beta 选择 Beta / RC",
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
    parser.add_argument(
        "--timeout",
        type=float,
        default=DEFAULT_SENTRY_TIMEOUT_SECONDS,
        help="单次 Sentry 查询超时秒数（默认：120）",
    )
    parser.add_argument("--verbose", action="store_true", help="输出 sentry 查询命令")
    parser.add_argument("--quiet", action="store_true", help="不输出查询进度")
    return parser


def main(argv: Sequence[str] | None = None, prog: str | None = None) -> int:
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8")

    arguments = create_argument_parser(prog).parse_args(argv)
    if arguments.trace_batch_size < 1:
        raise ValueError("--trace-batch-size 必须大于 0。")
    if not math.isfinite(arguments.timeout) or arguments.timeout <= 0:
        raise ValueError("--timeout 必须是大于 0 的有限数值。")
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
        timeout_seconds=arguments.timeout,
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
