"""读流程文件：每一步调用了哪些 pipeline 节点，以及各段在哪台平台上跑。"""

from __future__ import annotations

import ast
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, cast

from .flow import Step, as_list, load_jsonc
from .runner import Segment

# 流程函数里这些调用会碰 pipeline 节点；shot 只是截图，不跑节点。
NODE_METHODS = {"run", "click", "wait", "hit", "sub_task"}

# 展开 next 链时最多列这么多节点，免得某个万能转跳把整份 pipeline 都拖出来。
CHAIN_LIMIT = 24


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
    shot: str = ""


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
        elif method == "shot_when":
            shot = ""
            if len(call.args) > 1:
                second = call.args[1]
                if isinstance(second, ast.Constant) and isinstance(second.value, str):
                    shot = second.value
            entries.append(Entry("shot_when", first.value, first.lineno, shot=shot))
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
