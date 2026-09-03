"""根据 Sentry spans 生成自动送货任务分析报告。

默认分析 Sentry 最新发布的 MaaEnd release 最近 24 小时的 DeliveryJobs 与
SeizeDeliveryJobs。报告分别展示导航阶段失败率、失败节点分布和路线内部失败率，
并在同一条逻辑路线中对比开启和关闭滑索的失败率。路线内部失败可能已被上层
重试恢复，因此不等同于整次自动送货任务失败。
"""

from __future__ import annotations

import argparse
import json
import math
import re
import sys
from collections import defaultdict
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any, Iterable, Sequence, TextIO

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
    # 任务详情识别
    "AutoDelivery": "判断当前送货阶段",
    "AutoDeliveryRecognizeDestination": "识别送货目标",
    "AutoDeliveryRecognizeDepot": "识别仓储节点",
    "AutoDeliveryAfterResolveDestination": "进入送货目标处理",
    "AutoDeliveryInDeliveryMissionDetail": "确认送货任务详情页面",
    "AutoDeliveryCheckAreaText": "识别任务所在区域",
    "AutoDeliveryCheckStartTrackingButton": "识别开始追踪按钮",
    "AutoDeliveryCheckDestinationField": "识别送货条件区域",
    "AutoDeliveryCheckDestinationYellowText": "确认黄色送货条件",
    "AutoDeliveryCheckDestinationText": "识别送货目标文本",
    "AutoDeliveryEnd": "结束自动送货阶段",
    # 传送与取货
    "AutoDeliveryQuickTeleport": "准备快速传送",
    "AutoDeliveryViewDestinationMap": "点击查看任务位置",
    "AutoDeliveryStartTrackingTask": "开始追踪送货任务",
    "AutoDeliveryInTaskDestinationMap": "确认送货任务目标地图页面",
    "AutoDeliveryInDestinationMap": "确认任务目标地图",
    "AutoDeliveryQuickTeleportSelect": "选择任务附近传送点",
    "AutoDeliveryQuickTeleportClick": "执行快速传送",
    "AutoDeliveryInWorldAfterQuickTeleport": "确认传送完成",
    "AutoDeliveryPrepareNavigateDepot": "准备前往仓储节点",
    "AutoDeliveryCancelTrackingBeforeNavigateDepot": "仓储导航前取消追踪",
    "AutoDeliveryCheckTrackingAlreadyOffBeforeNavigateDepot": "确认仓储导航前任务未追踪",
    "AutoDeliveryCheckTrackingGoneBeforeNavigateDepot": "确认仓储导航前追踪标记已消失",
    "AutoDeliveryReturnWorldAndNavigateDepot": "返回大世界并准备仓储导航",
    "AutoDeliveryNavigateDepot": "前往仓储节点",
    "AutoDeliveryFetchGoods": "接取货物",
    "AutoDeliverySearchFetchGoodsButton": "环绕查找接取货物按钮",
    "AutoDeliveryCheckFetchGoodsButton": "识别取货按钮",
    "AutoDeliveryFetchGoodsButton": "点击取货按钮",
    "AutoDeliveryRetryNavigateDepot": "仓储站位重试",
    "AutoDeliveryOpenMissionAfterFetchGoods": "取货后返回任务界面",
    "AutoDeliveryOpenDeliveryMission": "打开并选择送货任务",
    "AutoDeliveryEnsureDeliveryMissionSelected": "查找并确认送货任务",
    "AutoDeliveryCheckDeliveryMissionSelected": "确认已选中送货任务",
    "AutoDeliveryCheckDeliveryMissionListComplete": "确认任务列表已到底",
    "AutoDeliveryCheckDeliveryMissionListItem": "识别任务列表中的送货任务",
    "AutoDeliverySelectDeliveryMission": "选择送货任务",
    "AutoDeliverySelectDeliveryMissionFromList": "查找送货任务",
    "AutoDeliveryScrollMissionList": "滚动查找送货任务",
    "AutoDeliveryDeliveryMissionNotFound": "未找到送货任务",
    # 终点导航与交货
    "AutoDeliveryCheckCancelCurrentJobTrackingButton": "识别取消追踪按钮",
    "AutoDeliveryCancelCurrentJobTracking": "送货导航前取消追踪",
    "AutoDeliveryCheckCurrentJobTrackingAlreadyOff": "确认送货任务未追踪",
    "AutoDeliveryCheckCurrentJobTrackingGone": "确认送货任务追踪标记已消失",
    "AutoDeliveryReturnWorldAndNavigateDestination": "返回大世界并准备终点导航",
    "AutoDeliveryPrepareNavigateDestination": "准备前往送货目标",
    "AutoDeliveryNavigateDestination": "前往送货目标",
    "AutoDeliverySearchSubmitGoodsButton": "环绕查找交货按钮",
    "AutoDeliveryRetryNavigateDestination": "送货目标站位重试",
    "AutoDeliveryCheckSubmitGoodsButton": "识别交货按钮",
    "AutoDeliverySubmitGoods": "提交货物",
    "AutoDeliveryInChatDialog": "识别送货对话",
    "AutoDeliveryCheckSkipChatButton": "识别跳过对话按钮",
    "AutoDeliverySkipChat": "跳过送货对话",
    "AutoDeliverySkipChatConfirm": "确认跳过送货对话",
    "AutoDeliveryCloseRewardDialog": "记录送货奖励",
    "AutoDeliveryCloseRewardDialogClick": "关闭送货奖励界面",
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
    failure_traces: int
    failure_rate: float
    without_zipline_failure_rate: float | None
    with_zipline_failure_rate: float | None


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


def require_object(value: Any, label: str) -> dict[str, Any]:
    """读取目录对象，并把结构错误统一转换为 ValueError。"""
    if not isinstance(value, dict):
        raise ValueError(f"送货目录结构错误：{label} 必须是对象。")
    return value


def require_array(value: Any, label: str) -> list[Any]:
    """读取目录数组，并把结构错误统一转换为 ValueError。"""
    if not isinstance(value, list):
        raise ValueError(f"送货目录结构错误：{label} 必须是数组。")
    return value


def require_text(record: dict[str, Any], key: str, label: str) -> str:
    """读取目录必需文本字段。"""
    value = record.get(key)
    if not isinstance(value, str) or not value.strip():
        raise ValueError(f"送货目录结构错误：{label}.{key} 必须是非空字符串。")
    return value


def require_localized_text(
    record: dict[str, Any],
    key: str,
    label: str,
) -> str:
    """读取目录必需的简体中文本地化字段。"""
    localized = require_object(record.get(key), f"{label}.{key}")
    return require_text(localized, "zh_cn", f"{label}.{key}")


def load_route_definitions(path: Path) -> dict[str, RouteDefinition]:
    """读取送货目录，并建立生成节点名到逻辑路线的映射。"""
    with path.open(encoding="utf-8") as file:
        catalog = json.load(file)

    catalog_object = require_object(catalog, "根节点")
    depots = require_array(catalog_object.get("depots"), "depots")
    destinations = require_array(
        catalog_object.get("destinations"),
        "destinations",
    )

    definitions: dict[str, RouteDefinition] = {}
    for index, value in enumerate(depots):
        label = f"depots[{index}]"
        depot = require_object(value, label)
        route_id = require_text(depot, "id", label)
        node_id = build_node_id(route_id)
        node_names = (
            f"AutoDeliveryRouteDepot{node_id}",
            f"AutoDeliveryRouteDepot{node_id}WithZipline",
            f"AutoDeliveryRouteDepotRetry{node_id}",
        )
        depot_name = require_localized_text(depot, "name", label)
        definition = RouteDefinition(
            route_id=route_id,
            route_type="仓储",
            name=depot_name,
            area=depot_name,
            node_names=node_names,
        )
        for node_name in node_names:
            definitions[node_name] = definition

    for index, value in enumerate(destinations):
        label = f"destinations[{index}]"
        destination = require_object(value, label)
        route_id = require_text(destination, "id", label)
        node_id = build_node_id(route_id)
        node_names = (
            f"AutoDeliveryRouteDestination{node_id}",
            f"AutoDeliveryRouteDestination{node_id}WithZipline",
            f"AutoDeliveryRouteDestinationRetry{node_id}",
        )
        definition = RouteDefinition(
            route_id=route_id,
            route_type="终点",
            name=require_localized_text(destination, "name", label),
            area=require_localized_text(destination, "area", label),
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
    traces_by_mode: dict[tuple[str, bool], set[str]] = defaultdict(set)
    failures_by_mode: dict[tuple[str, bool], set[str]] = defaultdict(set)
    definitions_by_id: dict[str, RouteDefinition] = {}
    unknown_nodes: set[str] = set()

    for row in rows:
        node = str(row["span.description"])
        definition = definitions.get(node)
        if definition is None:
            unknown_nodes.add(node)
            continue
        zipline_requested = node.endswith("WithZipline")
        route_id = definition.route_id
        mode_key = (route_id, zipline_requested)
        definitions_by_id[route_id] = definition
        trace = str(row["trace"])
        traces_by_route[route_id].add(trace)
        traces_by_mode[mode_key].add(trace)
        if row["span.status"] == INTERNAL_ERROR:
            failures_by_route[route_id].add(trace)
            failures_by_mode[mode_key].add(trace)

    report = []
    for route_id, traces in traces_by_route.items():
        definition = definitions_by_id[route_id]
        failures = len(failures_by_route[route_id])
        without_zipline_traces = traces_by_mode[(route_id, False)]
        with_zipline_traces = traces_by_mode[(route_id, True)]
        report.append(
            RouteRow(
                route_type=definition.route_type,
                name=definition.name,
                area=definition.area,
                route_id=route_id,
                traces=len(traces),
                failure_traces=failures,
                failure_rate=failures / len(traces),
                without_zipline_failure_rate=(
                    len(failures_by_mode[(route_id, False)])
                    / len(without_zipline_traces)
                    if without_zipline_traces
                    else None
                ),
                with_zipline_failure_rate=(
                    len(failures_by_mode[(route_id, True)]) / len(with_zipline_traces)
                    if with_zipline_traces
                    else None
                ),
            )
        )
    return (
        sorted(
            report,
            key=lambda row: (
                -row.failure_rate,
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
    environment: str | None,
    target: str,
    period: str,
    tasks: Sequence[str],
    catalog_path: Path,
    timeout_seconds: float,
    verbose: bool,
    quiet: bool,
) -> tuple[Report, set[str]]:
    route_definitions = load_route_definitions(catalog_path)
    if environment is not None:
        environment = normalize_sentry_environment(environment)
    if release is None:
        release_description = (
            f"{environment} 环境的最新 MaaEnd release"
            if environment is not None
            else "最新发布的 MaaEnd release"
        )
        show_progress(
            f"[0/3] 自动选择 {release_description}",
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
        timeout_seconds=timeout_seconds,
    )

    show_progress("[2/3] 查询失败节点分布", quiet=quiet)
    failure_rows = explore(
        sentry_command,
        target=target,
        period=period,
        fields=("span.description", "count_unique(trace)"),
        query=(
            f"{scope_filter} span.description:AutoDelivery* span.status:internal_error"
        ),
        sort="-count_unique(trace)",
        verbose=verbose,
        timeout_seconds=timeout_seconds,
    )

    show_progress("[3/3] 查询送货路线内部失败", quiet=quiet)
    route_rows = explore(
        sentry_command,
        target=target,
        period=period,
        fields=("timestamp", "trace", "span.description", "span.status"),
        query=f"{scope_filter} span.description:AutoDeliveryRoute*",
        sort="timestamp",
        verbose=verbose,
        timeout_seconds=timeout_seconds,
    )
    routes, unknown_nodes = build_route_rows(
        route_rows,
        route_definitions,
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
    print("报告内所有次数均按 trace 去重。", file=output)
    write_console_table(
        ("阶段", "次数", "失败次数", "失败率"),
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

    print("\n失败节点", file=output)
    print("失败节点表仅统计失败次数，不计算失败率。", file=output)
    write_console_table(
        ("阶段", "失败次数", "节点"),
        [
            (row.stage, str(row.failure_traces), row.node)
            for row in report.failure_nodes
        ],
        output,
        right_aligned={1},
    )

    print("\n路线内部失败（可能已被上层重试恢复）", file=output)
    print(
        "滑索开启表示选择了 WithZipline 路线分支，不代表运行时一定乘坐滑索。",
        file=output,
    )
    write_console_table(
        (
            "类型",
            "目标",
            "区域",
            "次数",
            "失败次数",
            "总失败率",
            "关闭滑索失败率",
            "开启滑索失败率",
        ),
        [
            (
                row.route_type,
                row.name,
                row.area,
                str(row.traces),
                str(row.failure_traces),
                format_rate(row.failure_rate),
                format_rate(row.without_zipline_failure_rate),
                format_rate(row.with_zipline_failure_rate),
            )
            for row in report.routes
        ],
        output,
        right_aligned={3, 4, 5, 6, 7},
    )


def write_markdown(report: Report, output: TextIO) -> None:
    print("## 自动送货导航阶段\n", file=output)
    print("> 报告内所有次数均按 trace 去重。\n", file=output)
    print("| 阶段 | 次数 | 失败次数 | 失败率 |", file=output)
    print("|---|---:|---:|---:|", file=output)
    for row in report.stages:
        print(
            f"| {row.stage} | {row.traces} | {row.failure_traces} | "
            f"{format_rate(row.failure_rate)} |",
            file=output,
        )

    print("\n## 失败节点\n", file=output)
    print(
        "> 失败节点表仅统计失败次数，不计算失败率。\n",
        file=output,
    )
    print("| 阶段 | 失败次数 | 节点 |", file=output)
    print("|---|---:|---|", file=output)
    for row in report.failure_nodes:
        print(f"| {row.stage} | {row.failure_traces} | `{row.node}` |", file=output)

    print("\n## 路线内部失败\n", file=output)
    print(
        "> 路线内部失败可能已被上层重试恢复。滑索开启表示选择了 "
        "`WithZipline` 路线分支，不代表运行时一定乘坐滑索。\n",
        file=output,
    )
    print(
        "| 类型 | 目标 | 区域 | 次数 | 失败次数 | 总失败率 | "
        "关闭滑索失败率 | 开启滑索失败率 |",
        file=output,
    )
    print("|---|---|---|---:|---:|---:|---:|---:|", file=output)
    for row in report.routes:
        print(
            f"| {row.route_type} | {row.name} | {row.area} | {row.traces} | "
            f"{row.failure_traces} | {format_rate(row.failure_rate)} | "
            f"{format_rate(row.without_zipline_failure_rate)} | "
            f"{format_rate(row.with_zipline_failure_rate)} |",
            file=output,
        )


def write_json(report: Report, output: TextIO) -> None:
    json.dump(asdict(report), output, ensure_ascii=False, indent=2)
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
    if not arguments.catalog.is_file():
        raise FileNotFoundError(f"找不到送货目标目录：{arguments.catalog}")
    if not math.isfinite(arguments.timeout) or arguments.timeout <= 0:
        raise ValueError("--timeout 必须是大于 0 的有限数值。")
    tasks = arguments.tasks or list(DEFAULT_TASKS)

    report, unknown_nodes = collect_report(
        sentry_command=resolve_sentry_command(),
        release=arguments.release,
        environment=arguments.environment,
        target=arguments.target,
        period=arguments.period,
        tasks=tasks,
        catalog_path=arguments.catalog,
        timeout_seconds=arguments.timeout,
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
