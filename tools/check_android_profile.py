#!/usr/bin/env python3
"""校验 Android/profile.yaml 的打包白名单是否完整覆盖 assets/interface.json 声明的移动端资源。"""

import argparse
import json
import os
import re
import sys
from collections.abc import Sequence
from pathlib import Path

DEFAULT_PI_INCLUDE = (
    "interface.json",
    "tasks/**",
    "resource/**",
    "resource_*/**",
    "data/**",
    "locales/**",
    "CONTACT",
    "LICENSE",
)

ALWAYS_EXCLUDED = (
    ".git/**",
    "node_modules/**",
    ".venv/**",
    "__pycache__/**",
)

ENV_PLACEHOLDER_RE = re.compile(r"\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}")


def strip_jsonc(text: str) -> str:
    """移除 JSONC 单行/多行注释与尾随逗号，保持纯标准库可解析。"""
    out: list[str] = []
    state = 0  # 0=normal, 1=string, 2=escape
    i = 0
    n = len(text)
    while i < n:
        ch = text[i]
        if state == 0:
            if ch == '"':
                out.append(ch)
                state = 1
                i += 1
            elif ch == "/" and i + 1 < n and text[i + 1] == "/":
                i += 2
                while i < n and text[i] != "\n":
                    i += 1
            elif ch == "/" and i + 1 < n and text[i + 1] == "*":
                i += 2
                while i + 1 < n and not (text[i] == "*" and text[i + 1] == "/"):
                    if text[i] == "\n":
                        out.append("\n")
                    i += 1
                i += 2
            else:
                out.append(ch)
                i += 1
        elif state == 1:
            out.append(ch)
            if ch == "\\":
                state = 2
            elif ch == '"':
                state = 0
            i += 1
        else:
            out.append(ch)
            state = 1
            i += 1

    no_comments = "".join(out)
    # 仅移除不在字符串字面量内的尾随逗号
    cleaned: list[str] = []
    in_str = False
    escaped = False
    j = 0
    m = len(no_comments)
    while j < m:
        ch = no_comments[j]
        if in_str:
            cleaned.append(ch)
            if escaped:
                escaped = False
            elif ch == "\\":
                escaped = True
            elif ch == '"':
                in_str = False
            j += 1
            continue

        if ch == '"':
            in_str = True
            cleaned.append(ch)
            j += 1
            continue

        if ch == ",":
            k = j + 1
            while k < m and no_comments[k] in " \t\r\n":
                k += 1
            if k < m and no_comments[k] in "]}":
                j += 1
                continue

        cleaned.append(ch)
        j += 1

    return "".join(cleaned)


def expand_env(value: str) -> str:
    def _replace(match: re.Match[str]) -> str:
        name = match.group(1)
        fallback = match.group(2)
        env_val = os.environ.get(name)
        if env_val:
            return env_val
        if fallback is not None:
            return fallback
        raise ValueError(f"环境变量 ${{{name}}} 未设置且无默认值")

    return ENV_PLACEHOLDER_RE.sub(_replace, value)


def _unquote_yaml_scalar(raw: str) -> str:
    val = raw.strip()
    if not val:
        return ""
    if val[0] in ('"', "'") and len(val) >= 2 and val[-1] == val[0]:
        quote = val[0]
        inner = val[1:-1]
        inner = inner.replace('\\"', '"').replace("\\\\", "\\") if quote == '"' else inner.replace("''", "'")
        return expand_env(inner)
    # 剥离行尾未加引号的注释（YAML 要求 # 前有空白）
    comment_idx = re.search(r"\s+#", val)
    if comment_idx:
        val = val[: comment_idx.start()].rstrip()
    return expand_env(val)


def parse_profile_yaml(profile_path: Path) -> tuple[Path | None, list[str], list[str]]:
    """解析 profile.yaml 顶层的 assets、include、exclude 字段（对齐 BuildProfile.kt 语义）。"""
    lines = profile_path.read_text(encoding="utf-8").splitlines()
    assets_raw: str | None = None
    include_list: list[str] | None = None
    exclude_list: list[str] = []

    current_list_key: str | None = None
    for raw_line in lines:
        stripped = raw_line.strip()
        if not stripped or stripped.startswith("#"):
            continue

        indent = len(raw_line) - len(raw_line.lstrip(" "))
        if indent == 0:
            current_list_key = None
            key, sep, rest = stripped.partition(":")
            if not sep:
                continue
            key = key.strip()
            rest_clean = _unquote_yaml_scalar(rest)
            if key == "assets":
                assets_raw = rest_clean or None
            elif key in ("include", "exclude"):
                target: list[str] = []
                if rest_clean == "[]":
                    pass
                elif rest_clean.startswith("[") and rest_clean.endswith("]"):
                    inner = rest_clean[1:-1].strip()
                    if inner:
                        target = [
                            s
                            for item in inner.split(",")
                            if (s := _unquote_yaml_scalar(item))
                        ]
                elif not rest_clean:
                    current_list_key = key
                else:
                    raise ValueError(f"{profile_path}: {key} 必须是列表")

                if key == "include":
                    include_list = target
                else:
                    exclude_list = target
        elif current_list_key and stripped.startswith("-"):
            item_raw = stripped[1:]
            item_val = _unquote_yaml_scalar(item_raw)
            if item_val:
                if current_list_key == "include":
                    assert include_list is not None
                    include_list.append(item_val)
                elif current_list_key == "exclude":
                    exclude_list.append(item_val)

    assets_dir: Path | None = None
    if assets_raw:
        cand = Path(assets_raw)
        assets_dir = cand if cand.is_absolute() else (profile_path.parent / cand).resolve()

    effective_include = include_list if include_list is not None else list(DEFAULT_PI_INCLUDE)
    return assets_dir, effective_include, exclude_list


def ant_glob_to_regex(pattern: str) -> re.Pattern[str]:
    """将 Gradle/Ant 风格路径通配符（支持 **、*、?，尾随 / 等价 /**）编译为正则。"""
    norm = pattern.strip().replace("\\", "/").lstrip("/")
    if norm.endswith("/"):
        norm += "**"

    segments = [seg for seg in norm.split("/") if seg]
    if not segments:
        return re.compile(r"^$")

    regex_parts: list[str] = ["^"]
    idx = 0
    total = len(segments)
    while idx < total:
        seg = segments[idx]
        if seg == "**":
            # 折叠连续的 **
            while idx + 1 < total and segments[idx + 1] == "**":
                idx += 1
            if idx == total - 1:
                # 末尾 /**：匹配任意深度子路径（至少一层）
                if idx == 0:
                    regex_parts.append(r".+")
                else:
                    regex_parts.append(r"(?:/.*)?")
            else:
                # 中间 **/：匹配零层或多层目录
                if idx == 0:
                    regex_parts.append(r"(?:.+/)?")
                else:
                    regex_parts.append(r"(?:/.*)?/")
        else:
            if idx > 0 and segments[idx - 1] != "**":
                regex_parts.append("/")
            seg_re: list[str] = []
            for ch in seg:
                if ch == "*":
                    seg_re.append(r"[^/]*")
                elif ch == "?":
                    seg_re.append(r"[^/]")
                else:
                    seg_re.append(re.escape(ch))
            regex_parts.append("".join(seg_re))
        idx += 1

    regex_parts.append("$")
    return re.compile("".join(regex_parts))


class ProfileMatcher:
    def __init__(self, includes: Sequence[str], excludes: Sequence[str]) -> None:
        self._includes = [ant_glob_to_regex(p) for p in includes]
        self._excludes = [ant_glob_to_regex(p) for p in (*excludes, *ALWAYS_EXCLUDED)]

    def matches(self, rel_posix_path: str) -> bool:
        return any(r.match(rel_posix_path) for r in self._includes) and not any(
            r.match(rel_posix_path) for r in self._excludes
        )


def _as_string_list(value: object) -> list[str]:
    if isinstance(value, str):
        return [value]
    if isinstance(value, list):
        return [item for item in value if isinstance(item, str)]
    return []


def _normalize_pi_path(raw: str) -> str:
    return raw.removeprefix("./")


def check_android_profile(profile_path: Path) -> list[str]:
    """校验 profile.yaml 与其指向的 interface.json；返回所有问题列表（空列表表示通过）。"""
    if not profile_path.is_file():
        return [f"找不到打包配置文件: {profile_path}"]

    try:
        assets_dir, includes, excludes = parse_profile_yaml(profile_path)
    except ValueError as exc:
        return [str(exc)]

    if assets_dir is None:
        return [f"{profile_path}: 未配置 assets 目录"]
    if not assets_dir.is_dir():
        return [f"{profile_path}: assets 目录不存在: {assets_dir}"]

    matcher = ProfileMatcher(includes, excludes)
    problems: list[str] = []

    interface_file = assets_dir / "interface.json"
    if not interface_file.is_file():
        return [f"找不到 interface.json: {interface_file}"]
    if not matcher.matches("interface.json"):
        problems.append("interface.json 未被 profile.yaml 的 include 覆盖（或被 exclude 排除）")

    try:
        interface_data = json.loads(strip_jsonc(interface_file.read_text(encoding="utf-8")))
    except json.JSONDecodeError as exc:
        return [f"{interface_file}: JSON 解析失败: {exc}"]

    if not isinstance(interface_data, dict):
        return [f"{interface_file}: 顶层必须是 JSON 对象"]

    assets_root = assets_dir.resolve()

    def verify_dir(raw_path: str, owner_desc: str) -> None:
        norm = _normalize_pi_path(raw_path)
        if not norm or Path(norm).is_absolute():
            problems.append(f"{owner_desc}: 非法相对路径 '{raw_path}'")
            return
        target = (assets_root / norm).resolve()
        if not target.is_relative_to(assets_root) or target == assets_root:
            problems.append(f"{owner_desc}: 路径 '{raw_path}' 超出 assets 目录")
            return
        if not target.is_dir():
            problems.append(f"{owner_desc}: 目录不存在 '{norm}'（{target}）")
            return

        has_any_included_file = False
        for file_path in target.rglob("*"):
            if not file_path.is_file():
                continue
            rel_posix = file_path.relative_to(assets_root).as_posix()
            if matcher.matches(rel_posix):
                has_any_included_file = True
                break

        if not has_any_included_file:
            problems.append(
                f"{owner_desc}: 目录 '{norm}' 下没有任何文件进入 Android 包（请检查 {profile_path.as_posix()} 的 include/exclude）"
            )

    def verify_file(raw_path: str, owner_desc: str) -> None:
        norm = _normalize_pi_path(raw_path)
        if not norm or Path(norm).is_absolute():
            problems.append(f"{owner_desc}: 非法相对路径 '{raw_path}'")
            return
        target = (assets_root / norm).resolve()
        if not target.is_relative_to(assets_root) or target == assets_root:
            problems.append(f"{owner_desc}: 路径 '{raw_path}' 超出 assets 目录")
            return
        if not target.is_file():
            problems.append(f"{owner_desc}: 文件不存在 '{norm}'（{target}）")
            return
        rel_posix = target.relative_to(assets_root).as_posix()
        if not matcher.matches(rel_posix):
            problems.append(
                f"{owner_desc}: 文件 '{norm}' 未被 {profile_path.as_posix()} 的 include 覆盖（或被 exclude 排除）"
            )

    controllers = interface_data.get("controller")
    ctrl_dicts = [c for c in controllers if isinstance(c, dict)] if isinstance(controllers, list) else []
    adb_controllers = [
        c for c in ctrl_dicts if isinstance(c.get("type"), str) and c["type"].lower() == "adb"
    ]
    adb_controller_names = {name for c in adb_controllers if isinstance(name := c.get("name"), str)}

    # 1. resource[].path（跳过通过 resource.controller 仅限定给非 Adb 控制器的桌面端资源）
    resources = interface_data.get("resource")
    if isinstance(resources, list) and resources:
        for idx, res in enumerate(resources):
            if not isinstance(res, dict):
                continue
            supported_ctrls = _as_string_list(res.get("controller"))
            if supported_ctrls and ctrl_dicts and not any(c in adb_controller_names for c in supported_ctrls):
                continue
            name = res.get("name") or f"#{idx}"
            for raw_p in _as_string_list(res.get("path")):
                verify_dir(raw_p, f"resource '{name}' path")
    else:
        verify_dir("resource", "默认 resource 目录")

    # 2. Android 外壳驱动的所有 Adb controller 的 attach_resource_path
    for idx, ctrl in enumerate(adb_controllers):
        name = ctrl.get("name") or f"#{idx}"
        for raw_p in _as_string_list(ctrl.get("attach_resource_path")):
            verify_dir(raw_p, f"controller '{name}' (Adb) attach_resource_path")

    # 3. languages 与 import 分片
    languages = interface_data.get("languages")
    if isinstance(languages, dict):
        for lang, raw_p in languages.items():
            if isinstance(raw_p, str):
                verify_file(raw_p, f"languages.{lang}")

    imports = interface_data.get("import")
    if isinstance(imports, list):
        for raw_p in imports:
            if isinstance(raw_p, str):
                verify_file(raw_p, "import")

    return problems


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="校验 Android/profile.yaml 资源打包覆盖完整性")
    parser.add_argument(
        "--profile",
        type=Path,
        default=Path("Android/profile.yaml"),
        help="Android 打包配置文件路径（默认: Android/profile.yaml）",
    )
    args = parser.parse_args(argv)

    problems = check_android_profile(args.profile)
    if problems:
        print(f"Android 打包配置校验失败 ({args.profile.as_posix()}):", file=sys.stderr)
        for item in problems:
            print(f"  - {item}", file=sys.stderr)
        return 1

    print(f"Android 打包配置校验通过: {args.profile.as_posix()}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
