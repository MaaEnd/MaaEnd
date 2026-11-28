"""读流程文件：每一步调用了哪些 pipeline 节点，以及各段在哪台平台上跑。"""

from __future__ import annotations

import ast
import json
import keyword
import re
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, cast

from .flow import Step, as_dict, as_list, load_jsonc
from .runner import Segment
from .validate import node_name_of

# 流程函数里这些调用会碰 pipeline 节点；shot 只是截图，不跑节点。
NODE_METHODS = {"run", "click", "wait", "hit", "text", "sub_task"}

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
    """流程里的一次调用：跑节点、看节点、截图、点坐标，或者 goto 转跳。

    name 对节点类调用是节点名，对 click_at / swipe / sleep 是参数原文；
    where 说明这次调用写在哪种代码块里（循环里、条件里……），平铺直叙的写法留空。
    """

    kind: str
    name: str
    line: int
    route: str = ""
    shot: str = ""
    where: str = ""


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


# 不碰节点、但流程图里也想看到的调用：参数原样展示。
LITERAL_METHODS = {"click_at", "swipe", "sleep"}


def _text_arg(call: ast.Call, index: int) -> str:
    """第 index 个位置参数是字符串常量就返回它，否则返回空串。"""
    if len(call.args) > index:
        arg = call.args[index]
        if isinstance(arg, ast.Constant) and isinstance(arg.value, str):
            return arg.value
    return ""


def _node_arg(call: ast.Call, index: int) -> str:
    """第 index 个位置参数写的是节点名（Nodes.XXX 或字符串）就返回它，否则返回空串。"""
    if len(call.args) > index:
        return node_name_of(call.args[index]) or ""
    return ""


def _entry_of(call: ast.Call, where: str) -> Entry | None:
    if not isinstance(call.func, ast.Attribute):
        return None
    method = call.func.attr
    if method in LITERAL_METHODS:
        shown = ", ".join(ast.unparse(arg) for arg in [*call.args, *call.keywords])
        return Entry(method, shown, call.lineno, where=where)
    if method == "shot":
        shot = _text_arg(call, 0)
        return Entry("shot", shot, call.lineno, where=where) if shot else None
    node = _node_arg(call, 0)
    if not node:
        return None
    if method == "shot_when":
        return Entry("shot_when", node, call.lineno, shot=_text_arg(call, 1), where=where)
    if method in NODE_METHODS:
        return Entry(method, node, call.lineno, where=where)
    if method == "goto":
        route = _node_arg(call, 1)
        if not route and len(call.args) > 1 and isinstance(call.args[1], ast.Name):
            route = call.args[1].id
        return Entry("goto", node, call.lineno, route, where=where)
    return None


def _entries_of(function: ast.FunctionDef) -> list[Entry]:
    entries: list[Entry] = []

    def visit(node: ast.AST, where: str) -> None:
        if isinstance(node, ast.Call):
            entry = _entry_of(node, where)
            if entry is not None:
                entries.append(entry)
        for name, value in ast.iter_fields(node):
            inner = where
            if isinstance(node, (ast.For, ast.While)) and name in ("body", "orelse"):
                inner = "循环里"
            elif isinstance(node, (ast.If, ast.While)) and name == "test":
                inner = "判断条件"
            elif isinstance(node, ast.If) and name in ("body", "orelse"):
                inner = "条件成立时" if name == "body" else "条件不成立时"
            elif isinstance(node, ast.ExceptHandler) and name == "body":
                inner = "出错时"
            children: list[Any] = cast("list[Any]", value) if isinstance(value, list) else [value]
            for child in children:
                if isinstance(child, ast.AST):
                    visit(child, inner)

    for statement in function.body:
        visit(statement, "")
    entries.sort(key=lambda entry: entry.line)
    return entries


def strip_comments(text: str) -> str:
    """去掉 JSONC 里的 // 和 /* */ 注释，字符串里的不动。

    整份 pipeline 七千多个节点，用 json5 逐个文件解析要十几秒；
    去掉注释后交给标准库 json 不到一秒。
    """
    out: list[str] = []
    index, length = 0, len(text)
    while index < length:
        char = text[index]
        if char == '"':
            end = index + 1
            while end < length and text[end] != '"':
                end += 2 if text[end] == "\\" else 1
            out.append(text[index : end + 1])
            index = end + 1
        elif text.startswith("//", index):
            newline = text.find("\n", index)
            index = length if newline < 0 else newline
        elif text.startswith("/*", index):
            close = text.find("*/", index + 2)
            index = length if close < 0 else close + 2
        else:
            out.append(char)
            index += 1
    return "".join(out)


def read_pipeline(path: Path) -> dict[str, Any]:
    """读一个 pipeline 文件；标准 json 读不了（尾逗号之类）再退回 json5。"""
    text = path.read_text(encoding="utf-8")
    try:
        data = json.loads(strip_comments(text))
    except ValueError:
        data = load_jsonc(path)
    return cast("dict[str, Any]", data) if isinstance(data, dict) else {}


def pipeline_files(repo: Path) -> list[Path]:
    """所有资源包（resource、resource_adb、resource_cloud_web……）里的 pipeline 文件。"""
    files: list[Path] = []
    for root in sorted((repo / "assets").glob("resource*/pipeline")):
        files.extend(path for path in sorted(root.rglob("*")) if path.suffix in (".json", ".jsonc"))
    return files


def load_pipeline(repo: Path) -> dict[str, dict[str, Any]]:
    """把各资源包的 pipeline 合成一张「节点名 → 节点内容」表；同名节点后读的覆盖先读的。"""
    result: dict[str, dict[str, Any]] = {}
    for path in pipeline_files(repo):
        try:
            data = read_pipeline(path)
        except Exception:
            continue
        for name, raw in data.items():
            if not name.startswith("$"):
                result[name] = as_dict(raw)
    return result


def load_next_map(repo: Path) -> dict[str, list[str]]:
    """把各资源包的 pipeline 合成一张「节点 → next」表。"""
    return {name: [str(item) for item in as_list(node.get("next"))] for name, node in load_pipeline(repo).items()}


# 节点名的类型存根：编辑器靠它给节点名补全、显示节点说明、标出写错的名字。不进仓库，按需生成。
NODE_NAMES_STUB = Path(__file__).with_name("node_names.pyi")

# pipeline 文件里顶层节点的那一行：prettier 排版后固定缩进 4 格。
NODE_KEY_LINE = re.compile(r'^    "((?:[^"\\]|\\.)+)"\s*:')


def _comments_above(path: Path) -> dict[str, list[str]]:
    """每个节点名上方紧挨着的 // 注释行（去掉 //）。"""
    result: dict[str, list[str]] = {}
    pending: list[str] = []
    for line in path.read_text(encoding="utf-8").splitlines():
        stripped = line.strip()
        if stripped.startswith("//"):
            pending.append(stripped[2:].strip())
            continue
        match = NODE_KEY_LINE.match(line)
        if match and pending:
            result[match.group(1)] = [text for text in pending if text]
        pending = []
    return result


def _type_name(value: Any, default: str) -> str:
    """识别 / 动作类型：可能直接写类型名，也可能写成 {"type": ..., "param": ...}。"""
    if isinstance(value, dict):
        value = cast("dict[str, Any]", value).get("type")
    return value if isinstance(value, str) and value else default


@dataclass
class NodeDoc:
    """编辑器里显示的一个节点的说明。"""

    desc: str = ""
    comments: list[str] = field(default_factory=list[str])
    kinds: str = ""
    files: list[str] = field(default_factory=list[str])

    def text(self) -> str:
        parts = [part for part in (self.desc, "\n".join(self.comments)) if part]
        parts.append(f"{self.kinds} · {'、'.join(self.files)}")
        return "\n\n".join(parts)


def node_docs(repo: Path) -> dict[str, NodeDoc]:
    """所有节点的说明。同名节点在几个资源包里都有时，后读的说明覆盖先读的，文件都列上。"""
    docs: dict[str, NodeDoc] = {}
    assets = repo / "assets"
    for path in pipeline_files(repo):
        try:
            data = read_pipeline(path)
            comments = _comments_above(path)
        except Exception:
            continue
        where = path.relative_to(assets).as_posix()
        for name, raw in data.items():
            if name.startswith("$"):
                continue
            node = as_dict(raw)
            doc = docs.setdefault(name, NodeDoc())
            desc = node.get("desc")
            doc.desc = desc.strip() if isinstance(desc, str) else ""
            doc.comments = comments.get(name, [])
            recognition = _type_name(node.get("recognition"), "DirectHit")
            action = _type_name(node.get("action"), "DoNothing")
            doc.kinds = f"识别 {recognition}，动作 {action}"
            doc.files.append(where)
    return docs


def attribute_ok(name: str) -> bool:
    """能不能写成 Nodes.节点名。

    不是合法标识符的（带 -）写不成属性；下划线开头的是 pipeline 内部用的节点，
    写成属性 pyright 会当成私有成员报错，这两种还是用字符串写。
    """
    return name.isidentifier() and not keyword.iskeyword(name) and not name.startswith("_")


def write_node_names(repo: Path, target: Path = NODE_NAMES_STUB) -> tuple[int, bool]:
    """把资源里所有节点写成类型存根，返回（节点数，这次有没有改动）。

    NodeName 是所有节点名的 Literal，字符串写法靠它补全和核对；
    Nodes 每个节点一个属性，属性下面一行字符串是节点说明，编辑器补全和悬停时显示。
    内容没变就不写，免得编辑器反复重新分析。
    """
    docs = node_docs(repo)
    names = sorted(docs)
    lines = [
        "# 由 uv run test-capture nodes 生成，别手改；改了 pipeline 再跑一次就会更新。",
        "# 运行时 NodeName 就是 str、Nodes.XXX 就是字符串 XXX（见 node_names.py），这里只给编辑器做补全和拼写检查。",
        "from typing import Literal",
        "",
        "type NodeName = Literal[",
        *(f"    {json.dumps(name, ensure_ascii=False)}," for name in names),
        "]",
        "",
        "class Nodes:",
        '    """每个节点一个属性，值是节点名；属性下面一行是节点说明。"""',
        "",
    ]
    for name in names:
        if not attribute_ok(name):
            continue
        lines.append(f"    {name}: Literal[{json.dumps(name, ensure_ascii=False)}]")
        lines.append(f"    {json.dumps(docs[name].text(), ensure_ascii=False)}")
    lines.append("")
    content = "\n".join(lines)
    if target.is_file() and target.read_text(encoding="utf-8") == content:
        return len(names), False
    target.write_text(content, encoding="utf-8")
    return len(names), True


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
