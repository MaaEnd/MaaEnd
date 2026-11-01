"""从当版 Lua 源码里读字面量：路径模板、常量拼接和 FOV，不执行 Lua。

只认三种写法：无转义的字符串、前面已经读到的常量、以及 ``..`` 拼接；函数调用、下标、算术一律报错。
需要判断表结构时（找 ``RegionMap3D`` 这一段配置、找 ``fov`` 这个字段）也只做词法切分，不跑解释器。
"""

import math
import re

from .errors import InputError
from .provider import LocalDataProvider

DEFAULT_LUA_PATH = "Phase/PhaseConfig.lua"
RETURN_ANCHOR = "RegionMap"

LONG_BRACKET_RE = re.compile(r"\[(=*)\[")


# ── 注释与长括号 ───────────────────────────────────────────────────────────


def _skip_long_bracket(source: str, index: int) -> int | None:
    """长括号从 *index* 开始就返回它的结束位置；这里没有长括号则返回 ``None``，由调用方自己往前走。"""
    match = LONG_BRACKET_RE.match(source, index)
    if match is None:
        return None
    closing = f"]{match.group(1)}]"
    end = source.find(closing, match.end())
    return len(source) if end < 0 else end + len(closing)


def strip_comments(source: str) -> str:
    """去掉 ``--`` 行注释和长括号注释；字符串与长括号字面量原样保留（注释里提到的名字不是值）。"""
    out: list[str] = []
    index = 0
    length = len(source)
    while index < length:
        char = source[index]
        if char in {'"', "'"}:
            out.append(char)
            index += 1
            while index < length:
                out.append(source[index])
                if source[index] == "\\" and index + 1 < length:
                    out.append(source[index + 1])
                    index += 2
                    continue
                if source[index] == char:
                    index += 1
                    break
                index += 1
            continue
        if source.startswith("--", index):
            long_stop = _skip_long_bracket(source, index + 2)
            if long_stop is not None:
                index = long_stop
                continue
            newline = source.find("\n", index)
            index = length if newline < 0 else newline
            continue
        if char == "[":
            long_stop = _skip_long_bracket(source, index)
            if long_stop is not None:
                out.append(source[index:long_stop])
                index = long_stop
                continue
        out.append(char)
        index += 1
    return "".join(out)


# ── 常量与模板 ─────────────────────────────────────────────────────────────


def literal_string(source: str, key: str, constants: dict[str, str] | None = None) -> str:
    """读 ``key = '...'`` 这一行的字面量：只接受无转义字符串、已读到的常量和 ``..`` 拼接。"""
    matches = re.findall(
        rf"^\s*(?:local\s+)?{re.escape(key)}\s*=([^\r\n]+)",
        strip_comments(source),
        re.MULTILINE,
    )
    if len(matches) != 1:
        raise InputError(f"Lua 里的 {key}：应当恰好有一处字面量赋值")
    expression = matches[0].strip().removesuffix(",").removesuffix(";").strip()
    token = re.compile(r"'([^'\\]*)'|\"([^\"\\]*)\"|([A-Za-z_]\w*)")
    values: list[str] = []
    end = 0
    for match in token.finditer(expression):
        gap = expression[end : match.start()].strip()
        if gap != (".." if values else ""):
            raise InputError(f"Lua 里的 {key}：不认识的写法 {expression!r}")
        if match.group(3) is not None:
            name = match.group(3)
            if constants is None or name not in constants:
                raise InputError(f"Lua 里的 {key}：常量 {name!r} 还没读到")
            values.append(constants[name])
        else:
            values.append(match.group(1) if match.group(1) is not None else match.group(2))
        end = match.end()
    if not values or expression[end:].strip():
        raise InputError(f"Lua 里的 {key}：不认识的写法 {expression!r}")
    return "".join(values)


def format_template(template: str, *values: str) -> str:
    """在 Python 里替换已知的 ``%s`` 模板，不去调用或执行 Lua 的 string.format。"""
    if template.count("%s") != len(values) or "%" in template.replace("%s", ""):
        raise InputError(f"不认识这个资源或名字模板：{template!r}")
    for value in values:
        template = template.replace("%s", value, 1)
    return template


# ── FOV ────────────────────────────────────────────────────────────────────


def extract_fov(provider: LocalDataProvider, logical_path: str = DEFAULT_LUA_PATH) -> float:
    try:
        src = provider.get_lua(logical_path)
    except InputError as error:
        if logical_path != DEFAULT_LUA_PATH:
            raise InputError(f"读不到 {logical_path}：{error}") from error
        # 有的解包布局把 PhaseConfig.lua 直接放在 LuaScripts 根上。
        logical_path = "PhaseConfig.lua"
        try:
            src = provider.get_lua(logical_path)
        except InputError as fallback_error:
            raise InputError(f"Phase/PhaseConfig.lua 和 PhaseConfig.lua 都读不到：{error}") from fallback_error
    candidates = _region_fovs(src)
    if not candidates:
        raise InputError(f"{logical_path}：RegionMap 所在的表里没有字面量 fov")
    values = set(candidates)
    if len(values) != 1:
        raise InputError(f"{logical_path}：RegionMap 的 fov 不唯一：{sorted(values)}")
    value = candidates[0]
    if not math.isfinite(value) or not 0 < value < 180:
        raise InputError(f"{logical_path}：fov {value} 不是合法的视角")
    return value


def _region_fovs(source: str) -> list[float]:
    # 只切分词法单元和表层级；字符串和注释里的花括号、fov 都不参与解析。
    token_re = re.compile(r"[A-Za-z_][A-Za-z_0-9]*|[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?|[^\s]")
    tokens: list[tuple[str, str]] = []
    source = strip_comments(source)
    index = 0
    while index < len(source):
        char = source[index]
        if char.isspace():
            index += 1
            continue
        if char in {"'", '"'}:
            quote = char
            end = index + 1
            while end < len(source):
                if source[end] == "\\":
                    end += 2
                elif source[end] == quote:
                    break
                else:
                    end += 1
            tokens.append(("string", source[index + 1 : end]))
            index = end + 1
            continue
        stop = _skip_long_bracket(source, index)
        if stop is not None:
            tokens.append(("string", "<long literal>"))
            index = stop
            continue
        match = token_re.match(source, index)
        if match is None:
            raise InputError("Lua 里有不认识的字符")
        tokens.append(("code", match.group()))
        index = match.end()
    stack: list[tuple[list[tuple[str, str]], list[tuple[str, str]]]] = []
    top: list[tuple[str, str]] = []
    candidates: list[float] = []
    for token in tokens:
        direct = stack[-1][1] if stack else top
        if token == ("code", "{"):
            # 赋值左侧带 RegionMap，或表里写着 name='RegionMap'，才算到阶段配置这一段。
            boundary = max((i for i, t in enumerate(direct) if t in [("code", ","), ("code", ";")]), default=-1)
            stack.append((direct[boundary + 1 :].copy(), []))
        elif token == ("code", "}"):
            if not stack:
                raise InputError("Lua 的表括号不配对")
            prefix, body = stack.pop()
            named = any(t[1] == RETURN_ANCHOR for t in prefix)
            named = named or any(
                body[i : i + 3] == [("code", "name"), ("code", "="), ("string", RETURN_ANCHOR)]
                for i in range(len(body) - 2)
            )
            if named:
                for i in range(len(body) - 2):
                    if body[i : i + 2] != [("code", "fov"), ("code", "=")]:
                        continue
                    literal = body[i + 2]
                    if literal[0] != "code" or (i + 3 < len(body) and body[i + 3][1] not in {",", ";"}):
                        raise InputError("RegionMap 的 fov 必须写成数字字面量")
                    try:
                        candidates.append(float(literal[1]))
                    except ValueError as error:
                        raise InputError("RegionMap 的 fov 必须写成数字字面量") from error
            parent = stack[-1][1] if stack else top
            parent.append(("code", "<table>"))
        else:
            direct.append(token)
    if stack:
        raise InputError("Lua 的表括号不配对")
    return candidates
