"""根据 Sentry spans 生成自动送货任务分析报告。

默认分析 beta 环境最近 24 小时的 DeliveryJobs 与 SeizeDeliveryJobs。报告分别展示
导航阶段失败率、失败节点分布和路线内部错误率。路线内部错误可能已被上层重试恢复，
因此不等同于整次自动送货任务失败。
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from collections import defaultdict
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any, Iterable, Sequence, TextIO

try:
    from .report_common import (
        explore,
        format_rate,
        resolve_sentry_command,
        show_progress,
        write_console_table,
    )
except ImportError:
    from report_common import (
        explore,
        format_rate,
        resolve_sentry_command,
        show_progress,
        write_console_table,
    )


DEFAULT_CATALOG_PATH = (
    Path(__file__).resolve().parents[1]
    / "pipeline-generate"
    / "data"
    / "delivery_destinations.json"
)
DEFAULT_TASKS = ("DeliveryJobs", "SeizeDeliveryJobs")
INTERNAL_ERROR = "internal_error"
NAVIGATION_PHASES = (
    ("AutoDeliveryNavigateDepot", "前往仓储节点"),
    ("AutoDeliveryNavigateDestination", "前往送货目标"),
)
FAILURE_LABELS = {
    "AutoDeliveryInDestinationMap": "打开任务目标地图",
    "AutoDeliveryNavigateDepot": "前往仓储节点",
    "AutoDeliveryRetryNavigateDepot": "仓储站位重试",
    "AutoDeliveryOpenMissionAfterFetchGoods": "取货后返回任务界面",
    "AutoDeliveryRecognizeDestination": "识别送货目标",
    "AutoDeliveryNavigateDestination": "前往送货目标",
    "AutoDeliverySubmitGoodsWaitFreezes": "识别交货按钮",
    "AutoDeliverySubmitGoods": "提交货物",
    "AutoDeliverySkipChat": "跳过送货对话",
    "AutoDeliveryCloseRewardDialog": "关闭奖励界面",
}


@dataclass(frozen=True)
class StageRow:
    stage: str
    node: str
    traces: int
    failure_traces: int
    failure_rate: float | None


@dataclass(frozen=True)
class FailureNodeRow:
    stage: str
    node: str
    failure_traces: int


@dataclass(frozen=True)
class RouteDefinition:
    route_id: str
    route_type: str
    name: str
    area: str
    node_names: tuple[str, ...]


@dataclass(frozen=True)
class RouteRow:
    route_type: str
    name: str
    area: str
    route_id: str
    traces: int
    internal_error_traces: int
    internal_error_rate: float


@dataclass(frozen=True)
class Report:
    stages: list[StageRow]
    failure_nodes: list[FailureNodeRow]
    routes: list[RouteRow]


def build_node_id(source_id: str) -> str:
    """按 AutoDelivery 生成器规则把数据 ID 转换为节点名片段。"""
    return "".join(
        f"{part[0].upper()}{part[1:]}"
        for part in re.split(r"[^A-Za-z0-9]+", source_id)
        if part
    )


def load_route_definitions(path: Path) -> dict[str, RouteDefinition]:
    """读取送货目录，并建立生成节点名到逻辑路线的映射。"""
    with path.open(encoding="utf-8") as file:
        catalog = json.load(file)

    definitions: dict[str, RouteDefinition] = {}
    for depot in catalog["depots"]:
        route_id = str(depot["id"])
        node_id = build_node_id(route_id)
        node_names = (
            f"AutoDeliveryRouteDepot{node_id}",
            f"AutoDeliveryRouteDepot{node_id}WithZipline",
            f"AutoDeliveryRouteDepotRetry{node_id}",
        )
        definition = RouteDefinition(
            route_id=route_id,
            route_type="仓储",
            name=str(depot["name"]["zh_cn"]),
            area=str(depot["name"]["zh_cn"]),
            node_names=node_names,
        )
        for node_name in node_names:
            definitions[node_name] = definition

    for destination in catalog["destinations"]:
        route_id = str(destination["id"])
        node_id = build_node_id(route_id)
        node_names = (
            f"AutoDeliveryRouteDestination{node_id}",
            f"AutoDeliveryRouteDestination{node_id}WithZipline",
        )
        definition = RouteDefinition(
            route_id=route_id,
            route_type="终点",
            name=str(destination["name"]["zh_cn"]),
            area=str(destination["area"]["zh_cn"]),
            node_names=node_names,
        )
        for node_name in node_names:
            definitions[node_name] = definition
    return definitions


def build_stage_rows(rows: Iterable[dict[str, Any]]) -> list[StageRow]:
    traces_by_node: dict[str, set[str]] = defaultdict(set)
    failures_by_node: dict[str, set[str]] = defaultdict(set)
    for row in rows:
        node = str(row["span.description"])
        trace = str(row["trace"])
        traces_by_node[node].add(trace)
        if row["span.status"] == INTERNAL_ERROR:
            failures_by_node[node].add(trace)

    report = []
    for node, stage in NAVIGATION_PHASES:
        traces = len(traces_by_node[node])
        failures = len(failures_by_node[node])
        report.append(
            StageRow(
                stage=stage,
                node=node,
                traces=traces,
                failure_traces=failures,
                failure_rate=failures / traces if traces else None,
            )
        )
    return report


def build_failure_node_rows(
    rows: Iterable[dict[str, Any]],
) -> list[FailureNodeRow]:
    report = []
    for row in rows:
        node = str(row["span.description"])
        if node.startswith("AutoDeliveryRoute"):
            continue
        report.append(
            FailureNodeRow(
                stage=FAILURE_LABELS.get(node, node),
                node=node,
                failure_traces=int(row["count_unique(trace)"]),
            )
        )
    return sorted(report, key=lambda row: (-row.failure_traces, row.stage))


def build_route_rows(
    rows: Iterable[dict[str, Any]],
    definitions: dict[str, RouteDefinition],
) -> tuple[list[RouteRow], set[str]]:
    traces_by_route: dict[str, set[str]] = defaultdict(set)
    failures_by_route: dict[str, set[str]] = defaultdict(set)
    definitions_by_id: dict[str, RouteDefinition] = {}
    unknown_nodes: set[str] = set()

    for row in rows:
        node = str(row["span.description"])
        definition = definitions.get(node)
        if definition is None:
            unknown_nodes.add(node)
            continue
        definitions_by_id[definition.route_id] = definition
        trace = str(row["trace"])
        traces_by_route[definition.route_id].add(trace)
        if row["span.status"] == INTERNAL_ERROR:
            failures_by_route[definition.route_id].add(trace)

    report = []
    for route_id, traces in traces_by_route.items():
        definition = definitions_by_id[route_id]
        failures = len(failures_by_route[route_id])
        report.append(
            RouteRow(
                route_type=definition.route_type,
                name=definition.name,
                area=definition.area,
                route_id=route_id,
                traces=len(traces),
                internal_error_traces=failures,
                internal_error_rate=failures / len(traces),
            )
        )
    return (
        sorted(
            report,
            key=lambda row: (
                -row.internal_error_rate,
                -row.traces,
                row.route_type,
                row.name,
            ),
        ),
        unknown_nodes,
    )


def collect_report(
    *,
    sentry_command: str,
    release: str | None,
    environment: str,
    target: str,
    period: str,
    tasks: Sequence[str],
    catalog_path: Path,
    verbose: bool,
    quiet: bool,
) -> tuple[Report, set[str]]:
    if release:
        escaped_release = release.replace('"', '\\"')
        scope_filter = f'release:"{escaped_release}"'
    else:
        scope_filter = f"environment:{environment}"
    task_filter = f"task:[{','.join(tasks)}]"
    scope_filter = f"{scope_filter} {task_filter}"

    phase_nodes = ",".join(node for node, _stage in NAVIGATION_PHASES)
    show_progress("[1/3] 查询导航阶段执行情况", quiet=quiet)
    stage_rows = explore(
        sentry_command,
        target=target,
        period=period,
        fields=("timestamp", "trace", "span.description", "span.status"),
        query=f"{scope_filter} span.description:[{phase_nodes}]",
        sort="timestamp",
        verbose=verbose,
    )

    show_progress("[2/3] 查询失败节点分布", quiet=quiet)
    failure_rows = explore(
        sentry_command,
        target=target,
        period=period,
        fields=("span.description", "count_unique(trace)"),
        query=(
            f"{scope_filter} span.description:AutoDelivery* "
            "span.status:internal_error"
        ),
        sort="-count_unique(trace)",
        verbose=verbose,
    )

    show_progress("[3/3] 查询送货路线内部错误", quiet=quiet)
    route_rows = explore(
        sentry_command,
        target=target,
        period=period,
        fields=("timestamp", "trace", "span.description", "span.status"),
        query=f"{scope_filter} span.description:AutoDeliveryRoute*",
        sort="timestamp",
        verbose=verbose,
    )
    routes, unknown_nodes = build_route_rows(
        route_rows,
        load_route_definitions(catalog_path),
    )
    return (
        Report(
            stages=build_stage_rows(stage_rows),
            failure_nodes=build_failure_node_rows(failure_rows),
            routes=routes,
        ),
        unknown_nodes,
    )


def write_console(report: Report, output: TextIO) -> None:
    print("自动送货导航阶段", file=output)
    write_console_table(
        ("阶段", "执行 trace", "失败 trace", "失败率"),
        [
            (
                row.stage,
                str(row.traces),
                str(row.failure_traces),
                format_rate(row.failure_rate),
            )
            for row in report.stages
        ],
        output,
        right_aligned={1, 2, 3},
    )

    print("\n失败节点（仅统计失败 trace，不推算缺少成功 span 的阶段失败率）", file=output)
    write_console_table(
        ("阶段", "失败 trace", "节点"),
        [
            (row.stage, str(row.failure_traces), row.node)
            for row in report.failure_nodes
        ],
        output,
        right_aligned={1},
    )

    print("\n路线内部错误（可能已被上层重试恢复）", file=output)
    write_console_table(
        ("类型", "目标", "区域", "涉及 trace", "错误 trace", "错误率"),
        [
            (
                row.route_type,
                row.name,
                row.area,
                str(row.traces),
                str(row.internal_error_traces),
                format_rate(row.internal_error_rate),
            )
            for row in report.routes
        ],
        output,
        right_aligned={3, 4, 5},
    )


def write_markdown(report: Report, output: TextIO) -> None:
    print("## 自动送货导航阶段\n", file=output)
    print("| 阶段 | 执行 trace | 失败 trace | 失败率 |", file=output)
    print("|---|---:|---:|---:|", file=output)
    for row in report.stages:
        print(
            f"| {row.stage} | {row.traces} | {row.failure_traces} | "
            f"{format_rate(row.failure_rate)} |",
            file=output,
        )

    print("\n## 失败节点\n", file=output)
    print(
        "> 仅统计失败 trace，不推算缺少成功 span 的阶段失败率。\n",
        file=output,
    )
    print("| 阶段 | 失败 trace | 节点 |", file=output)
    print("|---|---:|---|", file=output)
    for row in report.failure_nodes:
        print(f"| {row.stage} | {row.failure_traces} | `{row.node}` |", file=output)

    print("\n## 路线内部错误\n", file=output)
    print("> 路线内部错误可能已被上层重试恢复。\n", file=output)
    print("| 类型 | 目标 | 区域 | 涉及 trace | 错误 trace | 错误率 |", file=output)
    print("|---|---|---|---:|---:|---:|", file=output)
    for row in report.routes:
        print(
            f"| {row.route_type} | {row.name} | {row.area} | {row.traces} | "
            f"{row.internal_error_traces} | {format_rate(row.internal_error_rate)} |",
            file=output,
        )


def write_json(report: Report, output: TextIO) -> None:
    json.dump(asdict(report), output, ensure_ascii=False, indent=2)
    output.write("\n")


def create_argument_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--release",
        help="精确的 Sentry release 名称；指定后覆盖默认 environment 筛选",
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
        "--task",
        action="append",
        dest="tasks",
        help="Sentry task 名称，可重复指定（默认：DeliveryJobs、SeizeDeliveryJobs）",
    )
    parser.add_argument(
        "--format",
        choices=("console", "markdown", "json"),
        default="console",
        help="输出格式",
    )
    parser.add_argument(
        "--catalog",
        type=Path,
        default=DEFAULT_CATALOG_PATH,
        help="delivery_destinations.json 路径",
    )
    parser.add_argument("--verbose", action="store_true", help="输出 sentry 查询命令")
    parser.add_argument("--quiet", action="store_true", help="不输出查询进度")
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8")

    arguments = create_argument_parser().parse_args(argv)
    if not arguments.catalog.is_file():
        raise FileNotFoundError(f"找不到送货目标目录：{arguments.catalog}")
    tasks = arguments.tasks or list(DEFAULT_TASKS)

    report, unknown_nodes = collect_report(
        sentry_command=resolve_sentry_command(),
        release=arguments.release,
        environment=arguments.environment,
        target=arguments.target,
        period=arguments.period,
        tasks=tasks,
        catalog_path=arguments.catalog,
        verbose=arguments.verbose,
        quiet=arguments.quiet,
    )
    if unknown_nodes:
        print(
            "警告：以下路线节点未在送货目录中找到，已忽略："
            f"{', '.join(sorted(unknown_nodes))}",
            file=sys.stderr,
        )

    writers = {
        "console": write_console,
        "markdown": write_markdown,
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
