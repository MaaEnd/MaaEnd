#!/usr/bin/env python3
"""检查并生成 AutoCollect 路线的五语言文案。

简中路线文案是唯一输入，格式为 ``线路/路线N：材料``。材料必须能够由
``item.*`` 或 ``iconRecognition.name.*`` 中文词条完整匹配；多个材料可以直接
拼接，例如 ``血菌武陵石`` 会拆分为 ``血菌`` 和 ``武陵石``。

默认只检查，不修改文件。使用 ``--write`` 后，根据各语言的材料词条生成
``option.AutoCollect(Route|CommonRoute)N.label`` 和 ``.failed``。生成前会先
完成全部校验，避免中文源文案不完整时写出错误的多语言文案。
"""

from __future__ import annotations

import argparse
import difflib
import json
import re
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, List, Optional, Sequence, Tuple


LOCALES = ("zh_cn", "zh_tw", "en_us", "ja_jp", "ko_kr")
GLOSSARY_PREFIXES = ("item.", "iconRecognition.name.")
ROUTE_LABEL_RE = re.compile(
    r"^(?P<prefix>线路|路线)(?P<number>\d+)(?P<separator>：|:)(?P<material>.+)$"
)
ROUTE_KEY_RE = re.compile(
    r"^option\.(?P<route>AutoCollect(?:Common)?Route)(?P<number>\d+)\.(?P<field>label|failed)$"
)
KEY_LINE_RE = re.compile(r'^(?P<indent>\s*)"(?P<key>(?:\\.|[^"\\])*)"\s*:')
FAILURE_RE = re.compile(
    r'^<span style="color: red; font-weight: bold;">(?P<body>.*)</span>$'
)
FAILURE_TEMPLATE = '<span style="color: red; font-weight: bold;">{body}</span>'


@dataclass(frozen=True)
class GlossaryEntry:
    key: str
    text: str


@dataclass(frozen=True)
class RouteSource:
    key_prefix: str
    number: str
    label_key: str
    failed_key: str
    source_label: str
    material_text: str
    materials: Tuple[GlossaryEntry, ...]


@dataclass(frozen=True)
class LocaleStyle:
    label_prefix: str
    material_joiner: str
    failure_suffix: str


LOCALE_STYLES: Dict[str, LocaleStyle] = {
    "zh_tw": LocaleStyle("路線{number}：", "", "採集失敗"),
    "en_us": LocaleStyle("Collect Route {number}: ", " and ", " collection failed"),
    "ja_jp": LocaleStyle("採集ルート{number}：", "と", "：採集失敗"),
    "ko_kr": LocaleStyle("채집 경로 {number}: ", "과 ", " 채집 실패"),
}


def load_json(path: Path) -> Dict[str, object]:
    try:
        with path.open(encoding="utf-8") as file:
            value = json.load(file)
    except (OSError, json.JSONDecodeError) as error:
        raise RuntimeError(f"读取 {path} 失败: {error}") from error
    if not isinstance(value, dict):
        raise RuntimeError(f"{path} 顶层不是 JSON 对象")
    return value


def build_glossary(messages: Dict[str, object]) -> Dict[str, GlossaryEntry]:
    """按中文显示文本建立词表，优先使用 item.* 词条。"""

    entries: Dict[str, List[GlossaryEntry]] = {}
    for key, value in messages.items():
        if not isinstance(value, str) or not value:
            continue
        if not key.startswith(GLOSSARY_PREFIXES):
            continue
        entries.setdefault(value, []).append(GlossaryEntry(key, value))

    result: Dict[str, GlossaryEntry] = {}
    for text, candidates in entries.items():
        candidates.sort(key=lambda entry: (0 if entry.key.startswith("item.") else 1, entry.key))
        result[text] = candidates[0]
    return result


def segment_material(text: str, glossary: Dict[str, GlossaryEntry]) -> Optional[Tuple[GlossaryEntry, ...]]:
    """将无分隔的材料名拆成词表条目，优先少分段、长词条。"""

    if text in glossary:
        return (glossary[text],)

    phrases = sorted(glossary, key=lambda phrase: (-len(phrase), phrase))
    memo: Dict[int, Optional[Tuple[GlossaryEntry, ...]]] = {}

    def visit(offset: int) -> Optional[Tuple[GlossaryEntry, ...]]:
        if offset == len(text):
            return ()
        if offset in memo:
            return memo[offset]

        best: Optional[Tuple[GlossaryEntry, ...]] = None
        for phrase in phrases:
            if not text.startswith(phrase, offset):
                continue
            tail = visit(offset + len(phrase))
            if tail is None:
                continue
            candidate = (glossary[phrase],) + tail
            if best is None or len(candidate) < len(best):
                best = candidate
        memo[offset] = best
        return best

    return visit(0)


def route_sort_key(key: str) -> Tuple[int, int]:
    match = ROUTE_KEY_RE.match(key)
    assert match is not None
    return (0 if match.group("route") == "AutoCollectRoute" else 1, int(match.group("number")))


def extract_routes(messages: Dict[str, object], glossary: Dict[str, GlossaryEntry]) -> Tuple[List[RouteSource], List[str]]:
    errors: List[str] = []
    routes: List[RouteSource] = []
    label_keys = sorted(
        (key for key in messages if ROUTE_KEY_RE.fullmatch(key) and key.endswith(".label")),
        key=route_sort_key,
    )

    for label_key in label_keys:
        match = ROUTE_KEY_RE.fullmatch(label_key)
        assert match is not None
        raw_label = messages[label_key]
        if not isinstance(raw_label, str):
            errors.append(f"{label_key}: 文案不是字符串")
            continue
        label_match = ROUTE_LABEL_RE.fullmatch(raw_label)
        if label_match is None:
            errors.append(f"{label_key}: 不符合“线路/路线N：材料”格式：{raw_label!r}")
            continue

        number = match.group("number")
        if label_match.group("number") != number:
            errors.append(f"{label_key}: 路线编号与 key 不一致")
            continue
        material_text = label_match.group("material")
        materials = segment_material(material_text, glossary)
        if materials is None:
            suggestions = difflib.get_close_matches(material_text, glossary, n=3, cutoff=0.45)
            hint = f"；可能是：{'、'.join(suggestions)}" if suggestions else ""
            errors.append(f"{label_key}: 材料“{material_text}”无法完整匹配中文词表{hint}")
            continue

        failed_key = label_key[:-len(".label")] + ".failed"
        raw_failed = messages.get(failed_key)
        if not isinstance(raw_failed, str):
            errors.append(f"{failed_key}: 缺少失败提示")
        else:
            failure_match = FAILURE_RE.fullmatch(raw_failed)
            expected_body = raw_label + "采集失败"
            if failure_match is None or failure_match.group("body") != expected_body:
                errors.append(f"{failed_key}: 未与简中 label 保持“{raw_label}采集失败”格式")

        routes.append(
            RouteSource(
                key_prefix=match.group("route"),
                number=number,
                label_key=label_key,
                failed_key=failed_key,
                source_label=raw_label,
                material_text=material_text,
                materials=materials,
            )
        )

    if not label_keys:
        errors.append("简中 locale 中没有找到 AutoCollect 路线文案")
    return routes, errors


def render_route(route: RouteSource, locale: str, messages: Dict[str, object]) -> Tuple[str, str, List[str]]:
    style = LOCALE_STYLES[locale]
    translated: List[str] = []
    errors: List[str] = []
    for material in route.materials:
        value = messages.get(material.key)
        if not isinstance(value, str) or not value:
            errors.append(f"{locale}.json 缺少 {material.key}")
            continue
        translated.append(value)
    material_text = style.material_joiner.join(translated)
    label = style.label_prefix.format(number=route.number) + material_text
    failed = FAILURE_TEMPLATE.format(body=label + style.failure_suffix)
    return label, failed, errors


def replace_locale_values(path: Path, replacements: Dict[str, str]) -> int:
    original = path.read_text(encoding="utf-8")
    lines = original.splitlines(keepends=True)
    replaced: set[str] = set()
    output: List[str] = []

    for line in lines:
        match = KEY_LINE_RE.match(line)
        if match is None:
            output.append(line)
            continue
        key = json.loads('"' + match.group("key") + '"')
        if key not in replacements:
            output.append(line)
            continue
        body = line.rstrip("\r\n")
        comma = "," if body.rstrip().endswith(",") else ""
        newline = line[len(body) :]
        output.append(
            f'{match.group("indent")}{json.dumps(key, ensure_ascii=False)}: '
            f"{json.dumps(replacements[key], ensure_ascii=False)}{comma}{newline}"
        )
        replaced.add(key)

    missing = sorted(set(replacements) - replaced)
    if missing:
        raise RuntimeError(f"{path} 缺少待生成的 key: {', '.join(missing)}")
    updated = "".join(output)
    if updated != original:
        path.write_text(updated, encoding="utf-8")
    return len(replaced)


def collect_replacements(
    locale_dir: Path, routes: Sequence[RouteSource]
) -> Tuple[Dict[str, Dict[str, str]], List[str], int]:
    replacements: Dict[str, Dict[str, str]] = {}
    errors: List[str] = []
    differences = 0
    for locale in LOCALES:
        path = locale_dir / f"{locale}.json"
        if not path.exists():
            errors.append(f"缺少 locale 文件: {path}")
            continue
        messages = load_json(path)
        if locale == "zh_cn":
            continue
        locale_replacements: Dict[str, str] = {}
        for route in routes:
            label, failed, render_errors = render_route(route, locale, messages)
            errors.extend(render_errors)
            for key, expected in ((route.label_key, label), (route.failed_key, failed)):
                if key not in messages:
                    errors.append(f"{locale}.json 缺少 {key}")
                elif messages[key] != expected:
                    differences += 1
                    locale_replacements[key] = expected
        replacements[locale] = locale_replacements
    return replacements, errors, differences


def main() -> int:
    parser = argparse.ArgumentParser(description="检查并生成 AutoCollect 路线多语言文案")
    parser.add_argument(
        "--locale-dir",
        type=Path,
        default=Path("assets/locales/interface"),
        help="interface locale 目录（默认：assets/locales/interface）",
    )
    parser.add_argument("--write", action="store_true", help="生成并写回非简中 locale（默认只检查）")
    args = parser.parse_args()

    source_path = args.locale_dir / "zh_cn.json"
    try:
        source = load_json(source_path)
        glossary = build_glossary(source)
        routes, source_errors = extract_routes(source, glossary)
        replacements, generation_errors, differences = collect_replacements(args.locale_dir, routes)
    except RuntimeError as error:
        print(f"[ERROR] {error}", file=sys.stderr)
        return 2

    for error in source_errors + generation_errors:
        print(f"[ERROR] {error}")
    if source_errors or generation_errors:
        print("[FAIL] 未写入文件，请先修复上述问题。")
        return 2

    if not differences:
        print(f"[OK] {len(routes)} 条路线的材料词条和五语言文案均已同步。")
        return 0

    if not args.write:
        print(f"[CHECK] 发现 {differences} 处文案需要生成；使用 --write 写入。")
        return 1

    changed = 0
    for locale, locale_replacements in replacements.items():
        if locale_replacements:
            changed += replace_locale_values(args.locale_dir / f"{locale}.json", locale_replacements)
            print(f"[WRITE] {locale}.json 更新 {len(locale_replacements)} 个 key。")
    print(f"[OK] 已生成 {changed} 个 AutoCollect locale 文案。")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
