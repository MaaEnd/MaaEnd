#!/usr/bin/env python3
"""
将 pipeline 中 OCR 节点的 expected 统一替换为 CN/TC/EN/JP 四语文本。

规则：
1) 扫描目录：
   - assets/resource/pipeline
   - assets/resource_fast/pipeline
   - assets/resource_adb/pipeline
2) OCR 节点判定：
   - recognition == "OCR"
   - 或 recognition.type == "OCR"
3) expected 位置支持：
   - node.expected
   - node.recognition.expected
   - node.recognition.param.expected
4) 用 tools/i18n 下四个表反查语言 ID，回填顺序固定：
   简中(CN) -> 繁中(TC) -> 英文(EN) -> 日文(JP)

默认 dry-run，不修改文件；使用 --write 才会写入。
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from collections import defaultdict
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, List, Optional, Sequence, Set, Tuple


PIPELINE_DIRS = [
    Path("assets/resource/pipeline"),
    Path("assets/resource_fast/pipeline"),
    Path("assets/resource_adb/pipeline"),
]

I18N_FILES = {
    "CN": Path("tools/i18n/I18nTextTable_CN.json"),
    "TC": Path("tools/i18n/I18nTextTable_TC.json"),
    "EN": Path("tools/i18n/I18nTextTable_EN.json"),
    "JP": Path("tools/i18n/I18nTextTable_JP.json"),
}

LANG_ORDER = ("CN", "TC", "EN", "JP")
INDENT = "    "


def normalize_text(text: str) -> str:
    text = text.replace("\r\n", "\n").replace("\r", "\n").strip()
    return re.sub(r"\s+", " ", text)


@dataclass
class Member:
    key: str
    key_start: int
    value_start: int
    value_end: int


class JsoncParser:
    """轻量 JSONC 解析器，仅用于拿对象成员和数组字符串范围。"""

    def __init__(self, text: str):
        self.text = text
        self.n = len(text)

    def skip_ws_comments(self, i: int) -> int:
        while i < self.n:
            ch = self.text[i]
            if ch in " \t\r\n":
                i += 1
                continue
            if ch == "/" and i + 1 < self.n:
                nxt = self.text[i + 1]
                if nxt == "/":
                    i += 2
                    while i < self.n and self.text[i] not in "\r\n":
                        i += 1
                    continue
                if nxt == "*":
                    i += 2
                    while i + 1 < self.n and not (
                        self.text[i] == "*" and self.text[i + 1] == "/"
                    ):
                        i += 1
                    i += 2
                    continue
            break
        return i

    def parse_string(self, i: int) -> Tuple[str, int]:
        if i >= self.n or self.text[i] != '"':
            raise ValueError(f"Expected string at index {i}")
        j = i + 1
        escaped = False
        while j < self.n:
            ch = self.text[j]
            if escaped:
                escaped = False
            elif ch == "\\":
                escaped = True
            elif ch == '"':
                raw = self.text[i : j + 1]
                return json.loads(raw), j + 1
            j += 1
        raise ValueError("Unterminated string")

    def parse_primitive_end(self, i: int) -> int:
        j = i
        while j < self.n:
            ch = self.text[j]
            if ch in ",]}":
                break
            if ch == "/" and j + 1 < self.n and self.text[j + 1] in ("/", "*"):
                break
            j += 1
        return j

    def parse_array_end(self, i: int) -> int:
        if self.text[i] != "[":
            raise ValueError(f"Expected '[' at index {i}")
        i += 1
        while True:
            i = self.skip_ws_comments(i)
            if i >= self.n:
                raise ValueError("Unterminated array")
            if self.text[i] == "]":
                return i + 1
            i = self.parse_value_end(i)
            i = self.skip_ws_comments(i)
            if i < self.n and self.text[i] == ",":
                i += 1
                continue
            i = self.skip_ws_comments(i)
            if i < self.n and self.text[i] == "]":
                return i + 1
            raise ValueError(f"Expected ',' or ']' at index {i}")

    def parse_value_end(self, i: int) -> int:
        i = self.skip_ws_comments(i)
        if i >= self.n:
            raise ValueError("Unexpected EOF while parsing value")
        ch = self.text[i]
        if ch == '"':
            _, j = self.parse_string(i)
            return j
        if ch == "{":
            _, j = self.parse_object_members(i)
            return j
        if ch == "[":
            return self.parse_array_end(i)
        return self.parse_primitive_end(i)

    def parse_object_members(self, i: int) -> Tuple[List[Member], int]:
        if i >= self.n or self.text[i] != "{":
            raise ValueError(f"Expected '{{' at index {i}")
        members: List[Member] = []
        i += 1
        while True:
            i = self.skip_ws_comments(i)
            if i >= self.n:
                raise ValueError("Unterminated object")
            if self.text[i] == "}":
                return members, i + 1

            key_start = i
            key, i = self.parse_string(i)
            i = self.skip_ws_comments(i)
            if i >= self.n or self.text[i] != ":":
                raise ValueError(f"Expected ':' at index {i}")
            i += 1
            value_start = self.skip_ws_comments(i)
            value_end = self.parse_value_end(value_start)
            members.append(
                Member(
                    key=key,
                    key_start=key_start,
                    value_start=value_start,
                    value_end=value_end,
                )
            )
            i = self.skip_ws_comments(value_end)
            if i < self.n and self.text[i] == ",":
                i += 1
                continue
            i = self.skip_ws_comments(i)
            if i < self.n and self.text[i] == "}":
                return members, i + 1
            raise ValueError(f"Expected ',' or '}}' at index {i}")

    def parse_array_string_values(self, i: int) -> Tuple[List[str], int]:
        if i >= self.n or self.text[i] != "[":
            raise ValueError(f"Expected '[' at index {i}")
        values: List[str] = []
        i += 1
        while True:
            i = self.skip_ws_comments(i)
            if i >= self.n:
                raise ValueError("Unterminated array")
            if self.text[i] == "]":
                return values, i + 1
            if self.text[i] != '"':
                raise ValueError(
                    f"Expected string element in expected[] at index {i}, got '{self.text[i]}'"
                )
            val, i = self.parse_string(i)
            values.append(val)
            i = self.skip_ws_comments(i)
            if i < self.n and self.text[i] == ",":
                i += 1
                continue
            i = self.skip_ws_comments(i)
            if i < self.n and self.text[i] == "]":
                return values, i + 1
            raise ValueError(f"Expected ',' or ']' at index {i}")


def load_i18n_tables(base_dir: Path) -> Dict[str, Dict[str, str]]:
    tables: Dict[str, Dict[str, str]] = {}
    for lang, rel_path in I18N_FILES.items():
        path = base_dir / rel_path
        with path.open("r", encoding="utf-8") as f:
            data = json.load(f)
        if not isinstance(data, dict):
            raise ValueError(f"{path} 不是 JSON object")
        tables[lang] = {str(k): str(v) for k, v in data.items()}
    return tables


def build_reverse_index(tables: Dict[str, Dict[str, str]]) -> Dict[str, Set[str]]:
    reverse: Dict[str, Set[str]] = defaultdict(set)
    for table in tables.values():
        for lang_id, text in table.items():
            if not text:
                continue
            reverse[normalize_text(text)].add(lang_id)
    return reverse


def member_map(members: Sequence[Member]) -> Dict[str, Member]:
    return {m.key: m for m in members}


def get_string_value(parser: JsoncParser, member: Member) -> Optional[str]:
    if parser.text[member.value_start] != '"':
        return None
    value, _ = parser.parse_string(member.value_start)
    return value


def get_object_members(parser: JsoncParser, member: Member) -> Optional[List[Member]]:
    if parser.text[member.value_start] != "{":
        return None
    members, _ = parser.parse_object_members(member.value_start)
    return members


def get_array_member_if_exists(parser: JsoncParser, members: Dict[str, Member], key: str) -> Optional[Member]:
    m = members.get(key)
    if not m:
        return None
    if parser.text[m.value_start] != "[":
        return None
    return m


def detect_line_indent(text: str, key_start: int) -> str:
    line_start = text.rfind("\n", 0, key_start)
    line_start = 0 if line_start < 0 else line_start + 1
    i = line_start
    while i < len(text) and text[i] in (" ", "\t"):
        i += 1
    return text[line_start:i]


def build_expected_array_text(values: Sequence[str], key_indent: str, newline: str) -> str:
    if not values:
        return "[]"
    inner = ("," + newline).join(
        f"{key_indent}{INDENT}{json.dumps(v, ensure_ascii=False)}" for v in values
    )
    return f"[{newline}{inner}{newline}{key_indent}]"


def resolve_lang_ids(
    expected_values: Sequence[str], reverse_index: Dict[str, Set[str]]
) -> Tuple[List[str], List[str]]:
    candidates_by_text: List[Tuple[str, Set[str]]] = []
    for text in expected_values:
        norm = normalize_text(text)
        candidates = set(reverse_index.get(norm, set()))
        candidates_by_text.append((text, candidates))

    resolved_in_order: List[str] = []
    resolved_set: Set[str] = set()
    unresolved_texts: List[str] = []

    # 第一轮：唯一命中
    for text, candidates in candidates_by_text:
        if len(candidates) == 1:
            lang_id = next(iter(candidates))
            if lang_id not in resolved_set:
                resolved_in_order.append(lang_id)
                resolved_set.add(lang_id)
        elif len(candidates) == 0:
            unresolved_texts.append(text)

    # 第二轮：如果歧义候选与已解析 ID 有交集，用交集兜底
    for text, candidates in candidates_by_text:
        if len(candidates) > 1:
            intersection = [lang_id for lang_id in resolved_in_order if lang_id in candidates]
            if len(intersection) == 1:
                lang_id = intersection[0]
                if lang_id not in resolved_set:
                    resolved_in_order.append(lang_id)
                    resolved_set.add(lang_id)
            else:
                unresolved_texts.append(text)

    return resolved_in_order, unresolved_texts


def expand_expected_from_ids(lang_ids: Sequence[str], tables: Dict[str, Dict[str, str]]) -> List[str]:
    expanded: List[str] = []
    for lang_id in lang_ids:
        row = [tables[lang].get(lang_id, "") for lang in LANG_ORDER]
        if any(row):
            # 若某一语种缺失，保留空字符串会影响 OCR；这里跳过缺失项
            expanded.extend([txt for txt in row if txt])
    return expanded


def safe_print(message: str) -> None:
    """在 Windows GBK 控制台下安全输出，避免因无法编码而崩溃。"""
    try:
        print(message)
    except UnicodeEncodeError:
        encoding = getattr(sys.stdout, "encoding", None) or "utf-8"
        if hasattr(sys.stdout, "buffer"):
            sys.stdout.buffer.write((message + "\n").encode(encoding, errors="replace"))
        else:
            print(message.encode(encoding, errors="replace").decode(encoding, errors="replace"))


@dataclass
class NodeChange:
    node_name: str
    value_start: int
    value_end: int
    replacement: str
    old_expected: List[str]
    new_expected: List[str]
    unresolved_texts: List[str]


def process_pipeline_file(
    path: Path,
    tables: Dict[str, Dict[str, str]],
    reverse_index: Dict[str, Set[str]],
) -> Tuple[str, List[NodeChange], List[Tuple[str, str, List[str]]], int]:
    text = path.read_text(encoding="utf-8")
    parser = JsoncParser(text)
    newline = "\r\n" if "\r\n" in text else "\n"

    root_start = parser.skip_ws_comments(0)
    root_members, _ = parser.parse_object_members(root_start)

    changes: List[NodeChange] = []
    unresolved_nodes: List[Tuple[str, str, List[str]]] = []
    ocr_nodes_with_expected = 0

    for node_member in root_members:
        if text[node_member.value_start] != "{":
            continue

        node_name = node_member.key
        node_members, _ = parser.parse_object_members(node_member.value_start)
        node_map = member_map(node_members)

        recognition_member = node_map.get("recognition")
        is_ocr = False
        expected_member: Optional[Member] = None

        if recognition_member:
            recognition_str = get_string_value(parser, recognition_member)
            if recognition_str == "OCR":
                is_ocr = True
            else:
                rec_members = get_object_members(parser, recognition_member)
                if rec_members is not None:
                    rec_map = member_map(rec_members)
                    type_member = rec_map.get("type")
                    rec_type = get_string_value(parser, type_member) if type_member else None
                    if rec_type == "OCR":
                        is_ocr = True

                    # 优先取 recognition.param.expected，其次 recognition.expected
                    param_member = rec_map.get("param")
                    if param_member:
                        param_members = get_object_members(parser, param_member)
                        if param_members is not None:
                            param_map = member_map(param_members)
                            expected_member = get_array_member_if_exists(
                                parser, param_map, "expected"
                            )
                    if expected_member is None:
                        expected_member = get_array_member_if_exists(
                            parser, rec_map, "expected"
                        )

        if expected_member is None:
            expected_member = get_array_member_if_exists(parser, node_map, "expected")

        if not (is_ocr and expected_member):
            continue

        ocr_nodes_with_expected += 1
        old_expected, _ = parser.parse_array_string_values(expected_member.value_start)
        lang_ids, unresolved_texts = resolve_lang_ids(old_expected, reverse_index)

        if not lang_ids:
            unresolved_nodes.append((str(path), node_name, unresolved_texts or old_expected))
            continue

        new_expected = expand_expected_from_ids(lang_ids, tables)
        if not new_expected:
            unresolved_nodes.append((str(path), node_name, unresolved_texts or old_expected))
            continue

        if new_expected == old_expected:
            continue

        key_indent = detect_line_indent(text, expected_member.key_start)
        replacement = build_expected_array_text(new_expected, key_indent, newline)

        changes.append(
            NodeChange(
                node_name=node_name,
                value_start=expected_member.value_start,
                value_end=expected_member.value_end,
                replacement=replacement,
                old_expected=old_expected,
                new_expected=new_expected,
                unresolved_texts=unresolved_texts,
            )
        )

    if not changes:
        return text, [], unresolved_nodes, ocr_nodes_with_expected

    new_text = text
    for change in sorted(changes, key=lambda c: c.value_start, reverse=True):
        new_text = (
            new_text[: change.value_start]
            + change.replacement
            + new_text[change.value_end :]
        )
    return new_text, changes, unresolved_nodes, ocr_nodes_with_expected


def iter_pipeline_files(base_dir: Path) -> List[Path]:
    files: List[Path] = []
    for rel_dir in PIPELINE_DIRS:
        abs_dir = base_dir / rel_dir
        if not abs_dir.exists():
            continue
        files.extend(sorted(abs_dir.rglob("*.json")))
    return files


def main() -> int:
    argp = argparse.ArgumentParser(
        description="统一 OCR expected 为 CN/TC/EN/JP 四语文本（默认 dry-run）"
    )
    argp.add_argument(
        "--base-dir",
        type=Path,
        default=Path.cwd(),
        help="仓库根目录（默认当前目录）",
    )
    argp.add_argument(
        "--write",
        action="store_true",
        help="实际写入文件（默认仅预览统计）",
    )
    argp.add_argument(
        "--verbose",
        action="store_true",
        help="打印每个文件与节点的详细信息",
    )
    args = argp.parse_args()

    base_dir = args.base_dir.resolve()
    tables = load_i18n_tables(base_dir)
    reverse_index = build_reverse_index(tables)
    pipeline_files = iter_pipeline_files(base_dir)

    total_files = len(pipeline_files)
    touched_files = 0
    total_ocr_nodes = 0
    total_changed_nodes = 0
    unresolved_all: List[Tuple[str, str, List[str]]] = []

    for file_path in pipeline_files:
        try:
            new_text, changes, unresolved_nodes, ocr_nodes = process_pipeline_file(
                file_path, tables, reverse_index
            )
        except Exception as exc:
            safe_print(f"[ERROR] {file_path}: {exc}")
            continue

        total_ocr_nodes += ocr_nodes
        unresolved_all.extend(unresolved_nodes)

        if changes:
            touched_files += 1
            total_changed_nodes += len(changes)
            if args.write:
                file_path.write_text(new_text, encoding="utf-8")
            if args.verbose:
                safe_print(f"[CHANGED] {file_path} ({len(changes)} nodes)")
                for c in changes:
                    safe_print(f"  - {c.node_name}")
        elif args.verbose:
            safe_print(f"[SKIP] {file_path}")

    mode = "WRITE" if args.write else "DRY-RUN"
    safe_print(
        f"[{mode}] files={total_files}, touched_files={touched_files}, "
        f"ocr_nodes_with_expected={total_ocr_nodes}, changed_nodes={total_changed_nodes}, "
        f"unresolved_nodes={len(unresolved_all)}"
    )

    if unresolved_all:
        safe_print("---- unresolved nodes (top 50) ----")
        for file_path, node_name, unresolved in unresolved_all[:50]:
            unresolved_preview = ", ".join(repr(x) for x in unresolved[:3])
            if len(unresolved) > 3:
                unresolved_preview += ", ..."
            safe_print(f"{file_path} :: {node_name} :: [{unresolved_preview}]")

    if not args.write:
        safe_print("提示：加 --write 才会写入文件。")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
