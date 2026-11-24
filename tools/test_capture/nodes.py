"""列一遍「这次运行到底会跑哪些 pipeline 节点」，跑之前先对清单。"""

from __future__ import annotations

import ast
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, cast

from .flow import Flow, Step, as_list, load_jsonc
from .runner import ENTER_WORLD_NODE, OPEN_GAME_NODE, OPEN_GAME_TASK, PROBE_NODE, Segment, split_segments

# 流程函数里这些调用会碰 pipeline 节点；shot 只是截图，不跑节点。
NODE_METHODS = {"run", "click", "wait", "hit", "sub_task"}

# 展开 next 链时最多列这么多节点，免得某个万能转跳把整份 pipeline 都拖出来。
CHAIN_LIMIT = 24

# 转跳函数再往下展开几层（回快速转交确认 → 回调度申请）。
ROUTE_DEPTH = 3

KIND_TEXT = {
    "run": "只跑它自己（next 已清空）",
    "click": "用它定位后点一下（不跑它的 action）",
    "wait": "等它命中（不点不动）",
    "hit": "看一眼在不在",
}


@dataclass
class Run:
    """某一段在某台平台上跑不跑、跑哪几步。"""

    controller: str
    steps: list[str]
    skipped: list[str]


def plan(segments: list[Segment], controllers: tuple[str, ...]) -> dict[int, list[Run]]:
    """照着 runner 的规则推一遍：平台顺序一段正一段反，once 的步骤只归第一个做的平台。"""
    done_once: dict[str, str] = {}
    result: dict[int, list[Run]] = {}
    for segment in segments:
        order = controllers if segment.index % 2 == 1 else tuple(reversed(controllers))
        runs: list[Run] = []
        for name in order:
            skipped = [
                step.name
                for _, step in segment.steps
                if step.once and step.name in done_once and done_once[step.name] != name
            ]
            runs.append(
                Run(
                    controller=name,
                    steps=[step.name for _, step in segment.steps if step.name not in skipped],
                    skipped=skipped,
                )
            )
            if len(skipped) != len(segment.steps):
                for _, step in segment.steps:
                    if step.once and step.name not in skipped:
                        done_once[step.name] = name
        result[segment.index] = runs
    return result


@dataclass
class Entry:
    """流程里的一次调用：跑节点、看节点、截图，或者 goto 转跳。"""

    kind: str
    name: str
    line: int
    route: str = ""


@dataclass
class Prepared:
    entries: dict[str, list[Entry]] = field(default_factory=dict[str, list[Entry]])
    functions: dict[str, ast.FunctionDef] = field(default_factory=dict[str, ast.FunctionDef])


def prepare(source: Path, steps: list[Step]) -> Prepared:
    """读一遍流程文件，把每个函数里按代码顺序的调用抠出来。"""
    text = source.read_text(encoding="utf-8")
    tree = ast.parse(text, filename=str(source))
    functions = {node.name: node for node in tree.body if isinstance(node, ast.FunctionDef)}
    result = Prepared(functions=functions)
    for name in functions:
        result.entries[name] = _entries_of(functions[name])
    return result


def _entries_of(function: ast.FunctionDef) -> list[Entry]:
    entries: list[Entry] = []
    for call in ast.walk(function):
        if not isinstance(call, ast.Call) or not isinstance(call.func, ast.Attribute):
            continue
        method = call.func.attr
        if not call.args:
            continue
        first = call.args[0]
        if not (isinstance(first, ast.Constant) and isinstance(first.value, str)):
            continue
        if method == "shot":
            entries.append(Entry("shot", first.value, first.lineno))
        elif method in NODE_METHODS:
            entries.append(Entry(method, first.value, first.lineno))
        elif method == "goto":
            route = ""
            if len(call.args) > 1:
                second = call.args[1]
                if isinstance(second, ast.Constant) and isinstance(second.value, str):
                    route = second.value
                elif isinstance(second, ast.Name):
                    route = second.id
            entries.append(Entry("goto", first.value, first.lineno, route))
    entries.sort(key=lambda entry: entry.line)
    return entries


def load_next_map(repo: Path) -> dict[str, list[str]]:
    """把各资源包的 pipeline 合成一张「节点 → next」表。"""
    result: dict[str, list[str]] = {}
    for bundle in ("resource", "resource_adb", "resource_cloud", "resource_cloud_web"):
        root = repo / "assets" / bundle / "pipeline"
        if not root.is_dir():
            continue
        for path in sorted(root.rglob("*.json")):
            try:
                data = load_jsonc(path)
            except Exception:
                continue
            for name, raw in data.items():
                node = cast("dict[str, Any]", raw)
                result[name] = [str(item) for item in as_list(node.get("next"))]
    return result


def strip_prefix(name: str) -> str:
    """去掉 next 里的 [JumpBack] / [Anchor] 前缀，只看节点名。"""
    return name.split("]", 1)[1] if name.startswith("[") and "]" in name else name


def chain(next_map: dict[str, list[str]], start: str) -> list[str]:
    """沿 next 往下走，列出这一串会跑的节点（只走第一条分支，够看个大概）。"""
    seen: list[str] = []
    current = start
    while current and len(seen) < CHAIN_LIMIT:
        name = strip_prefix(current)
        seen.append(name)
        following = next_map.get(name) or []
        current = following[0] if following else ""
    return seen


def render(flow: Flow, repo: Path) -> str:
    """生成清单文本：开场固定跑的节点 + 每步调用的节点 + 转跳时连带跑的节点。"""
    next_map = load_next_map(repo)
    prepared = prepare(flow.source, flow.steps) if flow.source is not None else Prepared()
    segments = split_segments(flow)

    touched: set[str] = set()
    for step in flow.steps:
        if step.enter:
            touched.add(step.enter)
        if isinstance(step.route, str):
            touched.add(step.route)
        for entry in prepared.entries.get(step.name, []):
            if entry.kind != "shot":
                touched.add(entry.name)
            if entry.route and entry.route not in prepared.entries:
                touched.add(entry.route)
    lines: list[str] = [
        f"流程「{flow.name}」{len(flow.steps)} 步、{len(segments)} 段",
        f"直接点到的 pipeline 节点 {len(touched)} 个（转跳、sub_task 沿 next 连带跑的还没算）",
        "",
    ]
    lines.append("每个平台开场都会跑的：")
    lines.append("  预任务（go-service 跑，不是 pipeline 节点）：拉游戏 / 拉起云外壳")
    lines.append(f"  {OPEN_GAME_NODE}（安卓端才跑，对应任务 {OPEN_GAME_TASK}）")
    lines.append(f"  {ENTER_WORLD_NODE}  把游戏带回大世界")
    lines.append(f"  {PROBE_NODE}  每次「看界面 / 点一下」都用它包一层，本身不算业务节点")
    lines.append("")

    for segment in segments:
        lines.append(f"—— 第 {segment.index} 段 ——")
        for index, step in segment.steps:
            marks: list[str] = []
            if step.once:
                marks.append("只能做一次")
            if not step.stop:
                marks.append("连着下一段")
            head = f"第 {index} 步 {step.name}"
            if marks:
                head += "（" + "、".join(marks) + "）"
            lines.append(head)
            if step.enter:
                lines.append(f"  进界面    {step.enter}")
            if step.route is not None:
                lines.extend(_route_lines(step.route, prepared, next_map, indent="  "))
            entries = prepared.entries.get(step.name, [])
            if not entries:
                lines.append("  这一步不调用节点，只截图")
            lines.extend(_entry_lines(entries, prepared, next_map, indent="  "))
            lines.append("")
    return "\n".join(lines).rstrip() + "\n"


def _route_lines(route: object, prepared: Prepared, next_map: dict[str, list[str]], indent: str) -> list[str]:
    if isinstance(route, str):
        joined = " → ".join(chain(next_map, route))
        return [f"{indent}不在就转  {route}，会连带跑：{joined}"]
    name = getattr(route, "__name__", "")
    lines = [f"{indent}不在就转  {name}（流程函数，跑这些）："]
    return lines + _entry_lines_raw(name, prepared, next_map, indent + "  ", ROUTE_DEPTH)


def _entry_lines(entries: list[Entry], prepared: Prepared, next_map: dict[str, list[str]], indent: str) -> list[str]:
    return _render_entries(entries, prepared, next_map, indent, ROUTE_DEPTH)


def _entry_lines_raw(
    function: str, prepared: Prepared, next_map: dict[str, list[str]], indent: str, depth: int
) -> list[str]:
    if depth <= 0 or function not in prepared.entries:
        return [indent + "（流程函数不在这个文件里，看不到）"]
    return _render_entries(prepared.entries[function], prepared, next_map, indent, depth - 1)


def _render_entries(
    entries: list[Entry], prepared: Prepared, next_map: dict[str, list[str]], indent: str, depth: int
) -> list[str]:
    lines: list[str] = []
    for entry in entries:
        if entry.kind == "shot":
            lines.append(f"{indent}shot      {entry.name}")
        elif entry.kind == "sub_task":
            joined = " → ".join(chain(next_map, entry.name))
            lines.append(f"{indent}sub_task  {entry.name}  —— 整条 next 一起跑：{joined}")
        elif entry.kind == "goto":
            lines.append(f"{indent}goto      先看 {entry.name} 在不在")
            if entry.route:
                if entry.route in prepared.entries:
                    lines.append(f"{indent}          不在就跑 {entry.route}：")
                    lines.extend(_entry_lines_raw(entry.route, prepared, next_map, indent + "            ", depth))
                else:
                    lines.append(f"{indent}          不在就转 {entry.route}")
        else:
            lines.append(f"{indent}{entry.kind:<9} {entry.name}  —— {KIND_TEXT[entry.kind]}")
    return lines
