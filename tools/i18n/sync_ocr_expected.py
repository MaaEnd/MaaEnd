#!/usr/bin/env python3
"""
将 pipeline 中 OCR 节点的 expected 统一替换为 CN/TC/EN/JP 四语文本。

规则：
1) 扫描目录：
   - assets/resource/pipeline
   - assets/resource_adb/pipeline
2) OCR 节点判定：
   - recognition == "OCR"
   - 或 recognition.type == "OCR"
3) expected 位置支持：
   - node.expected
   - node.recognition.expected
   - node.recognition.param.expected
4) 用 i18n 四语表反查语言 ID（默认目录：tools/i18n）：
   - tools/i18n
   - 也可通过 --i18n-dir 指向临时克隆仓库的 i18n 目录
   - 若存在 I18nHotFix.json，会先将 hotfix 覆盖到对应语言表
   简中(CN) -> 繁中(TC) -> 英文(EN) -> 日文(JP)
5) 额外处理：
   - 英文 expected 会转为仅忽略大小写、并在单词间放宽空格的正则
   - OCR 节点会按新旧文本的最长显示宽度，通过 roi_offset 尝试扩展 roi 宽度
   - roi 优先读取 recognition.param.roi，其次 node.roi
   - only_rec: true 的节点不参与 roi 调整

默认 dry-run，不修改文件；使用 --write 才会写入。
若同一节点仅部分命中语言 ID，会保留未命中的原始 expected 文本，
输出顺序为：四语补全内容在前，未命中内容追加在后。
可在 expected 内部添加注释标记 @i18n-skip，脚本会跳过该节点。
"""

from __future__ import annotations

import argparse
import json
import math
import re
import sys
import unicodedata
from collections import defaultdict
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, List, Optional, Sequence, Set, Tuple, Union


PIPELINE_DIRS = [
    Path("assets/resource/pipeline"),
    Path("assets/resource_adb/pipeline"),
]

I18N_FILE_NAMES = {
    "CN": "I18nTextTable_CN.json",
    "TC": "I18nTextTable_TC.json",
    "EN": "I18nTextTable_EN.json",
    "JP": "I18nTextTable_JP.json",
}

HOTFIX_FILE_NAME = "I18nHotFix.json"

DEFAULT_I18N_DIR_CANDIDATES = [
    Path("tools/i18n"),
]

LANG_ORDER = ("CN", "TC", "EN", "JP")
INDENT = "    "

HOTFIX_TYPE_TO_LANG = {
    # 简中
    "CN": "CN",
    "SC": "CN",
    "CHS": "CN",
    "ZH": "CN",
    "ZH_CN": "CN",
    "SIMPLIFIED_CHINESE": "CN",
    # 繁中
    "TC": "TC",
    "CHT": "TC",
    "ZH_TW": "TC",
    "TRADITIONAL_CHINESE": "TC",
    # 英文
    "EN": "EN",
    # 日文
    "JP": "JP",
    "JA": "JP",
    "JPN": "JP",
}

I18N_SKIP_MARKER = "@i18n-skip"


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


def resolve_i18n_dir(base_dir: Path, i18n_dir_override: Optional[Path] = None) -> Path:
    if i18n_dir_override is not None:
        candidate = (
            i18n_dir_override
            if i18n_dir_override.is_absolute()
            else base_dir / i18n_dir_override
        )
        if not candidate.exists():
            raise FileNotFoundError(f"指定的 i18n 目录不存在: {candidate}")
        return candidate

    for rel_dir in DEFAULT_I18N_DIR_CANDIDATES:
        candidate = base_dir / rel_dir
        if candidate.exists():
            return candidate
    raise FileNotFoundError(
        "未找到可用 i18n 目录，已尝试: "
        + ", ".join(str(base_dir / p) for p in DEFAULT_I18N_DIR_CANDIDATES)
    )


def load_i18n_tables(
    base_dir: Path, i18n_dir_override: Optional[Path] = None
) -> Tuple[Dict[str, Dict[str, str]], Path]:
    i18n_dir = resolve_i18n_dir(base_dir, i18n_dir_override)
    tables: Dict[str, Dict[str, str]] = {}
    for lang, file_name in I18N_FILE_NAMES.items():
        path = i18n_dir / file_name
        if not path.exists():
            raise FileNotFoundError(f"缺少语言表: {path}")
        with path.open("r", encoding="utf-8") as f:
            data = json.load(f)
        if not isinstance(data, dict):
            raise ValueError(f"{path} 不是 JSON object")
        tables[lang] = {str(k): str(v) for k, v in data.items()}
    return tables, i18n_dir


def normalize_hotfix_type(type_value: str) -> Optional[str]:
    normalized = type_value.strip().upper().replace("-", "_")
    return HOTFIX_TYPE_TO_LANG.get(normalized)


def apply_hotfix_to_tables(
    tables: Dict[str, Dict[str, str]], i18n_dir: Path
) -> Tuple[int, int, bool]:
    """
    将 I18nHotFix.json 的文本覆盖到四语主表中。
    返回: (applied_count, skipped_count, hotfix_exists)
    """
    hotfix_path = i18n_dir / HOTFIX_FILE_NAME
    if not hotfix_path.exists():
        return 0, 0, False

    with hotfix_path.open("r", encoding="utf-8") as f:
        hotfix_data = json.load(f)
    if not isinstance(hotfix_data, dict):
        raise ValueError(f"{hotfix_path} 不是 JSON object")

    applied_count = 0
    skipped_count = 0

    for raw_outer_id, payload in hotfix_data.items():
        outer_id = str(raw_outer_id)
        if not isinstance(payload, dict):
            skipped_count += 1
            continue

        entries = payload.get("list")
        if not isinstance(entries, list):
            skipped_count += 1
            continue

        for entry in entries:
            if not isinstance(entry, dict):
                skipped_count += 1
                continue

            type_value = entry.get("type")
            text_value = entry.get("text")
            if not isinstance(type_value, str) or not isinstance(text_value, str):
                skipped_count += 1
                continue

            lang = normalize_hotfix_type(type_value)
            if lang is None:
                # 非 CN/TC/EN/JP 的 hotfix 忽略
                continue

            entry_id = entry.get("id")
            lang_id = str(entry_id) if entry_id is not None else outer_id

            tables[lang][lang_id] = text_value
            applied_count += 1

    return applied_count, skipped_count, True


def build_reverse_index(
    tables: Dict[str, Dict[str, str]],
) -> Tuple[Dict[str, Set[str]], Dict[str, Set[str]]]:
    reverse: Dict[str, Set[str]] = defaultdict(set)
    english_reverse: Dict[str, Set[str]] = defaultdict(set)
    for lang, table in tables.items():
        for lang_id, text in table.items():
            if not text:
                continue
            reverse[normalize_text(text)].add(lang_id)
            if lang == "EN":
                english_reverse[normalize_english_for_match(text)].add(lang_id)
                reverse[normalize_text(build_english_ocr_regex(text))].add(lang_id)
    return reverse, english_reverse


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


def get_array_member_if_exists(
    parser: JsoncParser, members: Dict[str, Member], key: str
) -> Optional[Member]:
    m = members.get(key)
    if not m:
        return None
    if parser.text[m.value_start] != "[":
        return None
    return m


def get_bool_value(parser: JsoncParser, member: Optional[Member]) -> Optional[bool]:
    if member is None:
        return None
    raw = parser.text[member.value_start : member.value_end].strip()
    if raw == "true":
        return True
    if raw == "false":
        return False
    return None


def detect_line_indent(text: str, key_start: int) -> str:
    line_start = text.rfind("\n", 0, key_start)
    line_start = 0 if line_start < 0 else line_start + 1
    i = line_start
    while i < len(text) and text[i] in (" ", "\t"):
        i += 1
    return text[line_start:i]


def detect_closing_brace_indent(text: str, brace_index: int) -> str:
    line_start = text.rfind("\n", 0, brace_index)
    line_start = 0 if line_start < 0 else line_start + 1
    i = line_start
    while i < len(text) and text[i] in (" ", "\t"):
        i += 1
    return text[line_start:i]


def build_expected_array_text(
    values: Sequence[str], key_indent: str, newline: str
) -> str:
    if not values:
        return "[]"
    inner = ("," + newline).join(
        f"{key_indent}{INDENT}{json.dumps(v, ensure_ascii=False)}" for v in values
    )
    return f"[{newline}{inner}{newline}{key_indent}]"


def build_numeric_array_text(
    values: Sequence[Union[int, float]], key_indent: str, newline: str
) -> str:
    def format_number(value: Union[int, float]) -> str:
        if isinstance(value, float) and value.is_integer():
            value = int(value)
        return str(value)

    if not values:
        return "[]"
    inner = ("," + newline).join(
        f"{key_indent}{INDENT}{format_number(v)}" for v in values
    )
    return f"[{newline}{inner}{newline}{key_indent}]"


def parse_array_number_values(
    parser: JsoncParser, member: Member
) -> Optional[List[Union[int, float]]]:
    if parser.text[member.value_start] != "[":
        return None

    values: List[Union[int, float]] = []
    i = member.value_start + 1
    while True:
        i = parser.skip_ws_comments(i)
        if i >= parser.n:
            raise ValueError("Unterminated numeric array")
        if parser.text[i] == "]":
            return values

        value_end = parser.parse_value_end(i)
        raw = parser.text[i:value_end].strip()
        try:
            value = json.loads(raw)
        except ValueError:
            return None
        if isinstance(value, bool) or not isinstance(value, (int, float)):
            return None
        values.append(value)

        i = parser.skip_ws_comments(value_end)
        if i < parser.n and parser.text[i] == ",":
            i += 1
            continue
        i = parser.skip_ws_comments(i)
        if i < parser.n and parser.text[i] == "]":
            return values
        raise ValueError(f"Expected ',' or ']' at index {i}")


def build_inserted_array_member_text(
    key: str,
    values: Sequence[Union[int, float]],
    key_indent: str,
    closing_indent: str,
    newline: str,
) -> str:
    array_text = build_numeric_array_text(values, key_indent, newline)
    return f",{newline}{key_indent}{json.dumps(key)}: {array_text}{newline}{closing_indent}"


def apply_roi_offset(
    roi_values: Sequence[Union[int, float]],
    roi_offset_values: Sequence[Union[int, float]],
) -> Optional[List[Union[int, float]]]:
    if len(roi_values) != 4 or len(roi_offset_values) != 4:
        return None

    x, y, w, h = [float(v) for v in roi_values]
    left, top, right, bottom = [float(v) for v in roi_offset_values]
    new_w = w + right - left
    new_h = h + bottom - top
    if new_w <= 0 or new_h <= 0:
        return None
    return [x + left, y + top, new_w, new_h]


def locate_node_roi_members(
    parser: JsoncParser, node_member: Member
) -> Tuple[Optional[Member], Optional[Member]]:
    node_members, _ = parser.parse_object_members(node_member.value_start)
    node_map = member_map(node_members)

    recognition_member = node_map.get("recognition")
    if recognition_member:
        recognition_str = get_string_value(parser, recognition_member)
        if recognition_str != "OCR":
            rec_members = get_object_members(parser, recognition_member)
            if rec_members is not None:
                rec_map = member_map(rec_members)
                param_member = rec_map.get("param")
                if param_member:
                    param_members = get_object_members(parser, param_member)
                    if param_members is not None:
                        param_map = member_map(param_members)
                        roi_member = param_map.get("roi")
                        roi_offset_member = get_array_member_if_exists(
                            parser, param_map, "roi_offset"
                        )
                        if roi_member is not None:
                            return roi_member, roi_offset_member

    roi_member = node_map.get("roi")
    if roi_member is None:
        return None, None
    roi_offset_member = get_array_member_if_exists(parser, node_map, "roi_offset")
    return roi_member, roi_offset_member


def resolve_effective_roi(
    parser: JsoncParser,
    roi_member: Member,
    roi_offset_member: Optional[Member],
    root_member_map: Dict[str, Member],
    roi_cache: Dict[str, Optional[List[Union[int, float]]]],
    visiting: Set[str],
) -> Optional[List[Union[int, float]]]:
    ch = parser.text[roi_member.value_start]
    base_roi: Optional[List[Union[int, float]]] = None
    if ch == "[":
        base_roi = parse_array_number_values(parser, roi_member)
    elif ch == '"':
        roi_ref = get_string_value(parser, roi_member)
        if roi_ref is None:
            return None
        if roi_ref in roi_cache:
            base_roi = roi_cache[roi_ref]
        else:
            ref_member = root_member_map.get(roi_ref)
            if ref_member is None or roi_ref in visiting:
                return None
            visiting.add(roi_ref)
            ref_roi_member, ref_roi_offset_member = locate_node_roi_members(
                parser, ref_member
            )
            if ref_roi_member is not None:
                base_roi = resolve_effective_roi(
                    parser,
                    ref_roi_member,
                    ref_roi_offset_member,
                    root_member_map,
                    roi_cache,
                    visiting,
                )
            visiting.remove(roi_ref)
            roi_cache[roi_ref] = base_roi
    if base_roi is None or len(base_roi) != 4:
        return None

    if roi_offset_member is None:
        return base_roi
    roi_offset_values = parse_array_number_values(parser, roi_offset_member)
    if roi_offset_values is None or len(roi_offset_values) != 4:
        return None
    return apply_roi_offset(base_roi, roi_offset_values)


def resolve_lang_ids(
    expected_values: Sequence[str],
    reverse_index: Dict[str, Set[str]],
    english_reverse_index: Dict[str, Set[str]],
    tables: Dict[str, Dict[str, str]],
) -> Tuple[List[str], List[str]]:
    candidates_by_text: List[Tuple[str, Set[str]]] = []
    for text in expected_values:
        norm = normalize_text(text)
        candidates = set(reverse_index.get(norm, set()))
        if not candidates:
            candidates = set(
                english_reverse_index.get(normalize_english_for_match(text), set())
            )
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
            intersection = [
                lang_id for lang_id in resolved_in_order if lang_id in candidates
            ]
            if len(intersection) == 1:
                # 这里表示“该歧义文本可复用一个已解析 ID”，无需重复加入列表
                # 只把它视为已解析，不进入 unresolved_texts
                pass
            else:
                # Third fallback: if ambigous IDs have identical rows in all languages, pick anyone (smallest ID)
                rows = [
                    tuple(tables[lang].get(lid, "") for lang in LANG_ORDER)
                    for lid in candidates
                ]
                if len(set(rows)) == 1:
                    lang_id = min(candidates)  # Choose smallest ID
                    if lang_id not in resolved_set:
                        resolved_in_order.append(lang_id)
                        resolved_set.add(lang_id)
                else:
                    unresolved_texts.append(text)

    return resolved_in_order, unresolved_texts


def expand_expected_from_ids(
    lang_ids: Sequence[str], tables: Dict[str, Dict[str, str]]
) -> List[str]:
    expanded: List[str] = []
    seen: Set[str] = set()
    for lang_id in lang_ids:
        row = [tables[lang].get(lang_id, "") for lang in LANG_ORDER]
        if any(row):
            # 若某一语种缺失，保留空字符串会影响 OCR；这里跳过缺失项，并去重
            for lang, txt in zip(LANG_ORDER, row):
                if txt and txt not in seen:
                    expanded.append(build_expected_text_for_lang(lang, txt))
                    seen.add(txt)
    return expanded


def append_unresolved_texts(
    base_expected: List[str], unresolved_texts: Sequence[str]
) -> List[str]:
    """
    将未命中的原始 expected 追加到结果末尾，并避免重复追加。
    """
    result = list(base_expected)
    existing = set(result)
    for text in unresolved_texts:
        if text not in existing:
            result.append(text)
            existing.add(text)
    return result


def build_expected_text_for_lang(lang: str, text: str) -> str:
    if lang != "EN":
        return text
    return build_english_ocr_regex(text)


def escape_regex_literal(text: str) -> str:
    return re.sub(r"([\\.^$*+?{}\[\]|()])", r"\\\1", text)


def split_english_text_tokens(text: str) -> List[str]:
    return re.findall(r"[A-Za-z0-9]+|[^A-Za-z0-9\s]+", text)


def build_english_ocr_regex(text: str) -> str:
    tokens = split_english_text_tokens(text)
    if not tokens:
        return r"(?i)\s*"
    pieces = [escape_regex_literal(token) for token in tokens]
    return rf"(?i){r'\s*'.join(pieces)}"


def normalize_english_for_match(text: str) -> str:
    tokens = split_english_text_tokens(
        text.replace("\r\n", "\n").replace("\r", "\n").strip()
    )
    return "\u0000".join(token.casefold() for token in tokens)


def expected_text_matches_lang_text(
    expected_text: str, lang: str, lang_text: str
) -> bool:
    if lang == "EN":
        if normalize_text(expected_text) == normalize_text(
            build_english_ocr_regex(lang_text)
        ):
            return True
        return normalize_english_for_match(
            expected_text
        ) == normalize_english_for_match(lang_text)
    return normalize_text(expected_text) == normalize_text(lang_text)


def estimate_text_display_width(text: str) -> float:
    width = 0.0
    for ch in text:
        if ch.isspace():
            continue
        if unicodedata.east_asian_width(ch) in ("W", "F"):
            width += 2.0
        else:
            width += 1.0
    return width


def estimate_expected_max_width(
    expected_values: Sequence[str],
    lang_ids: Sequence[str],
    tables: Dict[str, Dict[str, str]],
) -> float:
    widths: List[float] = []
    for expected_text in expected_values:
        for lang_id in lang_ids:
            matched = False
            for lang in LANG_ORDER:
                lang_text = tables[lang].get(lang_id, "")
                if not lang_text:
                    continue
                if expected_text_matches_lang_text(expected_text, lang, lang_text):
                    widths.append(estimate_text_display_width(lang_text))
                    matched = True
                    break
            if matched:
                break
    return max(widths, default=0.0)


def estimate_translated_max_width(
    lang_ids: Sequence[str], tables: Dict[str, Dict[str, str]]
) -> float:
    widths = [
        estimate_text_display_width(text)
        for lang_id in lang_ids
        for lang in LANG_ORDER
        for text in [tables[lang].get(lang_id, "")]
        if text
    ]
    return max(widths, default=0.0)


def estimate_roi_base_width(
    old_expected: Sequence[str],
    new_expected: Sequence[str],
    lang_ids: Sequence[str],
    tables: Dict[str, Dict[str, str]],
) -> float:
    """
    ROI 宽度基准：
    - 若本轮生成结果与旧 expected 不同，说明当前 ROI 仍对应“旧文本”，
      基准宽度取旧 expected 中能匹配回语言表的文本宽度。
    - 若本轮生成结果与旧 expected 相同，说明节点已经处于当前展开结果，
      基准宽度取当前展开后的最长文本宽度，避免重复放大。
    """
    if list(old_expected) == list(new_expected):
        return estimate_translated_max_width(lang_ids, tables)
    return estimate_expected_max_width(old_expected, lang_ids, tables)


def compute_expanded_roi(
    roi_values: Sequence[Union[int, float]],
    old_max_width: float,
    new_max_width: float,
) -> Optional[List[Union[int, float]]]:
    if len(roi_values) != 4 or old_max_width <= 0 or new_max_width <= old_max_width:
        return None

    x, y, w, h = roi_values
    if isinstance(w, bool) or not isinstance(w, (int, float)):
        return None
    if isinstance(x, bool) or not isinstance(x, (int, float)):
        return None

    new_w = int(math.ceil(float(w) * new_max_width / old_max_width))
    max_w = int(math.floor(1280 - float(x)))
    if max_w <= int(math.floor(float(w))):
        return None

    new_w = min(new_w, max_w)
    if new_w <= float(w):
        return None

    return [x, y, new_w, h]


def has_i18n_skip_marker(text: str, expected_member: Member) -> bool:
    """
    检查 expected 数组源码片段中是否包含跳过标记。
    标记示例：
      "expected": [
          // @i18n-skip
          "xxx"
      ]
    """
    raw_expected = text[expected_member.value_start : expected_member.value_end]
    return I18N_SKIP_MARKER in raw_expected


def safe_print(message: str) -> None:
    """在 Windows GBK 控制台下安全输出，避免因无法编码而崩溃。"""
    try:
        print(message)
    except UnicodeEncodeError:
        encoding = getattr(sys.stdout, "encoding", None) or "utf-8"
        if hasattr(sys.stdout, "buffer"):
            sys.stdout.buffer.write((message + "\n").encode(encoding, errors="replace"))
        else:
            print(
                message.encode(encoding, errors="replace").decode(
                    encoding, errors="replace"
                )
            )


@dataclass
class Replacement:
    value_start: int
    value_end: int
    replacement: str


@dataclass
class NodeChange:
    node_name: str
    replacements: List[Replacement]
    old_expected: List[str]
    new_expected: List[str]
    unresolved_texts: List[str]
    old_roi: Optional[List[Union[int, float]]] = None
    new_roi: Optional[List[Union[int, float]]] = None


def process_pipeline_file(
    path: Path,
    tables: Dict[str, Dict[str, str]],
    reverse_index: Dict[str, Set[str]],
    english_reverse_index: Dict[str, Set[str]],
) -> Tuple[str, List[NodeChange], List[Tuple[str, str, List[str]]], int, int]:
    text = path.read_text(encoding="utf-8")
    parser = JsoncParser(text)
    newline = "\r\n" if "\r\n" in text else "\n"

    root_start = parser.skip_ws_comments(0)
    root_members, _ = parser.parse_object_members(root_start)
    root_member_map = {member.key: member for member in root_members}
    roi_cache: Dict[str, Optional[List[Union[int, float]]]] = {}

    changes: List[NodeChange] = []
    unresolved_nodes: List[Tuple[str, str, List[str]]] = []
    ocr_nodes_with_expected = 0
    skipped_by_marker = 0

    for node_member in root_members:
        if text[node_member.value_start] != "{":
            continue

        node_name = node_member.key
        node_members, node_end = parser.parse_object_members(node_member.value_start)
        node_map = member_map(node_members)
        only_rec = get_bool_value(parser, node_map.get("only_rec")) is True

        recognition_member = node_map.get("recognition")
        is_ocr = False
        expected_member: Optional[Member] = None
        roi_member: Optional[Member] = None
        roi_offset_member: Optional[Member] = None
        roi_container_end = node_end

        if recognition_member:
            recognition_str = get_string_value(parser, recognition_member)
            if recognition_str == "OCR":
                is_ocr = True
            else:
                rec_members = get_object_members(parser, recognition_member)
                if rec_members is not None:
                    rec_map = member_map(rec_members)
                    type_member = rec_map.get("type")
                    rec_type = (
                        get_string_value(parser, type_member) if type_member else None
                    )
                    if rec_type == "OCR":
                        is_ocr = True

                    # 优先取 recognition.param.expected，其次 recognition.expected
                    param_member = rec_map.get("param")
                    if param_member:
                        param_members, param_end = parser.parse_object_members(
                            param_member.value_start
                        )
                        if param_members is not None:
                            param_map = member_map(param_members)
                            only_rec = only_rec or (
                                get_bool_value(parser, param_map.get("only_rec"))
                                is True
                            )
                            expected_member = get_array_member_if_exists(
                                parser, param_map, "expected"
                            )
                            roi_member = param_map.get("roi")
                            roi_offset_member = get_array_member_if_exists(
                                parser, param_map, "roi_offset"
                            )
                            if roi_member is not None:
                                roi_container_end = param_end
                    if expected_member is None:
                        expected_member = get_array_member_if_exists(
                            parser, rec_map, "expected"
                        )

        if expected_member is None:
            expected_member = get_array_member_if_exists(parser, node_map, "expected")
        if roi_member is None:
            roi_member = node_map.get("roi")
            roi_offset_member = get_array_member_if_exists(
                parser, node_map, "roi_offset"
            )
            if roi_member is not None:
                roi_container_end = node_end

        if not (is_ocr and expected_member):
            continue

        if has_i18n_skip_marker(text, expected_member):
            skipped_by_marker += 1
            continue

        ocr_nodes_with_expected += 1
        old_expected, _ = parser.parse_array_string_values(expected_member.value_start)
        lang_ids, unresolved_texts = resolve_lang_ids(
            old_expected, reverse_index, english_reverse_index, tables
        )

        if not lang_ids:
            unresolved_nodes.append(
                (str(path), node_name, unresolved_texts or old_expected)
            )
            continue

        new_expected = expand_expected_from_ids(lang_ids, tables)
        if not new_expected:
            unresolved_nodes.append(
                (str(path), node_name, unresolved_texts or old_expected)
            )
            continue
        new_expected = append_unresolved_texts(new_expected, unresolved_texts)
        replacements: List[Replacement] = []

        if new_expected != old_expected:
            key_indent = detect_line_indent(text, expected_member.key_start)
            replacement = build_expected_array_text(new_expected, key_indent, newline)
            replacements.append(
                Replacement(
                    value_start=expected_member.value_start,
                    value_end=expected_member.value_end,
                    replacement=replacement,
                )
            )

        old_roi: Optional[List[Union[int, float]]] = None
        new_roi: Optional[List[Union[int, float]]] = None
        if not only_rec and roi_member is not None:
            effective_roi = resolve_effective_roi(
                parser,
                roi_member,
                roi_offset_member,
                root_member_map,
                roi_cache,
                {node_name},
            )
            if effective_roi is not None and len(effective_roi) == 4:
                old_max_width = estimate_roi_base_width(
                    old_expected, new_expected, lang_ids, tables
                )
                new_max_width = estimate_translated_max_width(lang_ids, tables)
                expanded_roi = compute_expanded_roi(
                    effective_roi, old_max_width, new_max_width
                )
                if expanded_roi is not None:
                    delta_w = expanded_roi[2] - effective_roi[2]
                    if delta_w > 0:
                        key_indent = detect_line_indent(text, roi_member.key_start)
                        if roi_offset_member is not None:
                            roi_offset_values = parse_array_number_values(
                                parser, roi_offset_member
                            )
                            if (
                                roi_offset_values is not None
                                and len(roi_offset_values) == 4
                            ):
                                new_roi_offset = list(roi_offset_values)
                                new_roi_offset[2] = float(new_roi_offset[2]) + delta_w
                                replacements.append(
                                    Replacement(
                                        value_start=roi_offset_member.value_start,
                                        value_end=roi_offset_member.value_end,
                                        replacement=build_numeric_array_text(
                                            new_roi_offset, key_indent, newline
                                        ),
                                    )
                                )
                            else:
                                delta_w = 0
                        else:
                            closing_indent = detect_closing_brace_indent(
                                text, roi_container_end - 1
                            )
                            replacements.append(
                                Replacement(
                                    value_start=roi_container_end - 1,
                                    value_end=roi_container_end - 1,
                                    replacement=build_inserted_array_member_text(
                                        "roi_offset",
                                        [0, 0, delta_w, 0],
                                        key_indent,
                                        closing_indent,
                                        newline,
                                    ),
                                )
                            )
                        if delta_w > 0:
                            old_roi = list(effective_roi)
                            new_roi = expanded_roi

        if not replacements:
            continue

        changes.append(
            NodeChange(
                node_name=node_name,
                replacements=replacements,
                old_expected=old_expected,
                new_expected=new_expected,
                unresolved_texts=unresolved_texts,
                old_roi=old_roi,
                new_roi=new_roi,
            )
        )

    if not changes:
        return text, [], unresolved_nodes, ocr_nodes_with_expected, skipped_by_marker

    new_text = text
    replacements = [
        replacement for change in changes for replacement in change.replacements
    ]
    for change in sorted(replacements, key=lambda c: c.value_start, reverse=True):
        new_text = (
            new_text[: change.value_start]
            + change.replacement
            + new_text[change.value_end :]
        )
    return (
        new_text,
        changes,
        unresolved_nodes,
        ocr_nodes_with_expected,
        skipped_by_marker,
    )


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
    argp.add_argument(
        "--i18n-dir",
        type=Path,
        default=None,
        help="i18n 目录；默认使用 tools/i18n",
    )
    args = argp.parse_args()

    base_dir = args.base_dir.resolve()
    tables, i18n_dir = load_i18n_tables(base_dir, args.i18n_dir)
    hotfix_applied, hotfix_skipped, has_hotfix = apply_hotfix_to_tables(
        tables, i18n_dir
    )
    reverse_index, english_reverse_index = build_reverse_index(tables)
    pipeline_files = iter_pipeline_files(base_dir)

    safe_print(f"[INFO] using i18n dir: {i18n_dir}")
    if has_hotfix:
        safe_print(
            f"[INFO] applied hotfix: {hotfix_applied} entries"
            + (f", skipped={hotfix_skipped}" if hotfix_skipped else "")
        )
    else:
        safe_print("[INFO] no I18nHotFix.json found, skip hotfix merge")

    total_files = len(pipeline_files)
    touched_files = 0
    total_ocr_nodes = 0
    total_changed_nodes = 0
    total_skipped_nodes = 0
    unresolved_all: List[Tuple[str, str, List[str]]] = []
    failed_files: List[Tuple[str, str]] = []

    for file_path in pipeline_files:
        try:
            new_text, changes, unresolved_nodes, ocr_nodes, skipped_nodes = (
                process_pipeline_file(
                    file_path, tables, reverse_index, english_reverse_index
                )
            )
        except Exception as exc:
            safe_print(f"[ERROR] {file_path}: {exc}")
            failed_files.append((str(file_path), str(exc)))
            continue

        total_ocr_nodes += ocr_nodes
        total_skipped_nodes += skipped_nodes
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
        f"unresolved_nodes={len(unresolved_all)}, skipped_by_marker={total_skipped_nodes}"
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

    if failed_files:
        safe_print(f"[ERROR] 共有 {len(failed_files)} 个文件处理失败，退出码为 1：")
        for path, reason in failed_files:
            safe_print(f"  - {path}: {reason}")
        return 1

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
