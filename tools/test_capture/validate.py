"""开跑前核对流程里写的节点名，写错了当场停下，不用等连上设备、跑到那一步才发现。"""

from __future__ import annotations

import ast
import difflib
from dataclasses import dataclass, field
from pathlib import Path

import click

from .flow import FlowError
from .types import Resource

# 这些 Capture 方法的前几个参数是节点名，-1 表示位置参数全是节点名；其余方法收截图名、坐标、文字，不在这里核对。
NODE_ARG_METHODS = {
    "hit": 1,
    "text": 1,
    "wait": 1,
    "click": 1,
    "run": 1,
    "sub_task": 1,
    "shot_when": 1,
    "goto": 2,
    "wait_any": -1,
}

# 步骤声明里的关键字，值也是节点名。
STEP_KEYWORDS = frozenset({"enter", "route"})

# 这些位置上的参数是「怎么过去」，可以写一段转跳动作，那样没法静态核对。
ROUTE_POSITIONS = frozenset({("goto", 1)})

# 节点名写错时最多给几个候选。
SUGGESTIONS = 3


def node_name_of(expr: ast.expr) -> str | None:
    """参数是写死的节点名就返回它：Nodes.XXX 或者字符串常量；其它写法（变量、表达式）返回 None。"""
    if isinstance(expr, ast.Attribute) and isinstance(expr.value, ast.Name) and expr.value.id == "Nodes":
        return expr.attr
    if isinstance(expr, ast.Constant) and isinstance(expr.value, str):
        return expr.value
    return None


class NodeNameError(FlowError):
    """流程里的节点名跟资源对不上；跟哪台平台无关，一台报出来就够了。"""


@dataclass(frozen=True)
class NodeRef:
    """流程里当成节点名用的一处：Nodes.XXX 或者字符串。"""

    name: str
    line: int


@dataclass
class NodeRefs:
    """一份流程收集到的节点引用。

    nodes 是静态能认出来的节点名；unchecked 是参数写成变量、跑之前没法核对的行号。
    where 是流程文件，报错时一起打出来。
    """

    where: str = ""
    nodes: list[NodeRef] = field(default_factory=list[NodeRef])
    unchecked: list[int] = field(default_factory=list[int])


def collect(path: Path) -> NodeRefs:
    """扫流程文件，把当成节点名用的 Nodes.XXX 和字符串都收出来。"""
    tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
    refs = NodeRefs(where=str(path))
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call):
            continue
        method = node.func.attr if isinstance(node.func, ast.Attribute) else ""
        count = NODE_ARG_METHODS.get(method, 0)
        for position, arg in enumerate(node.args if count < 0 else node.args[:count]):
            name = node_name_of(arg)
            if name is not None:
                refs.nodes.append(NodeRef(name, arg.lineno))
            elif (method, position) in ROUTE_POSITIONS:
                continue
            elif not (isinstance(arg, ast.Constant) and arg.value is None):
                refs.unchecked.append(arg.lineno)
        for keyword in node.keywords:
            if keyword.arg not in STEP_KEYWORDS:
                continue
            value = keyword.value
            name = node_name_of(value)
            if name is not None:
                refs.nodes.append(NodeRef(name, value.lineno))
            elif keyword.arg == "enter" and not (isinstance(value, ast.Constant) and value.value is None):
                # enter 必须是节点名；route 还可以写成一段转跳动作，那样静态核对不了。
                refs.unchecked.append(value.lineno)
    return refs


def check(refs: NodeRefs, resource: Resource) -> None:
    """节点名跟资源对不上就抛错，一次把所有写错的都列出来。"""
    known = set(resource.node_list or [])
    problems: list[str] = []
    reported: set[str] = set()
    for ref in refs.nodes:
        if ref.name in known or ref.name in reported:
            continue
        reported.add(ref.name)
        close = difflib.get_close_matches(ref.name, known, n=SUGGESTIONS, cutoff=0.6)
        options = "、".join(close)
        hint = f"，是不是「{options}」" if close else ""
        problems.append(f"  {refs.where}:{ref.line}「{ref.name}」资源里没有{hint}")
    if problems:
        raise NodeNameError("流程里的节点名对不上资源，先改好再跑：\n" + "\n".join(problems))
    if refs.unchecked:
        lines = "、".join(str(line) for line in refs.unchecked)
        click.echo(f"节点名核对：第 {lines} 行写的是变量，没法在跑之前核对，只能跑到那一步才知道")
