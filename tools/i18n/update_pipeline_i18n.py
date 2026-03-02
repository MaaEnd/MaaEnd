#!/usr/bin/env python3
# -*- coding: utf-8 -*-

"""
扫描 Pipeline JSON，提取 OCR 识别用的 expected 文本，
写入 assets/misc/locales/pipeline_i18n.json，并尝试用 tools/i18n 下的
多语言文本表自动补全繁中 / 英文 / 日文翻译。

使用方式（在仓库根目录）：

    python tools/i18n/update_pipeline_i18n.py

脚本会：
1. 遍历 assets/resource 与 assets/resource_fast 下所有 .json 文件
2. 找出 recognition 为 OCR 的节点（支持：
   - "recognition": "OCR"
   - "recognition": { "type": "OCR", ... }
3. 从节点中提取：
   - 节点名（JSON 对象在其父级中的 key）
   - 文件路径（写成 ./assets/... 相对路径）
   - expected 文本（按以下优先级）：
       a) obj["action"]["expected"]
       b) obj["expected"]
       c) obj["recognition"]["param"]["expected"]
     若为数组则用 "|" 连接为一个字符串
   - doc/desc/description 作为说明（仅写入 pipeline_i18n.json 的 doc 字段）
4. 合并进 assets/misc/locales/pipeline_i18n.json 中（不会覆盖已有的非空翻译）
5. 读取 tools/i18n/I18nTextTable_*.json，以 zh_cn 文本为键尝试在文本表中
   查出对应 ID，并用同一个 ID 在 TC/EN/JP 表中取出翻译自动补全
"""

from __future__ import annotations

import jsonc
import re
import sys
from pathlib import Path
from typing import Any, Dict, List, Tuple, Optional


REPO_ROOT = Path(__file__).resolve().parents[2]

PIPELINE_DIRS = [
    REPO_ROOT / "assets" / "resource",
    REPO_ROOT / "assets" / "resource_fast",
]

PIPELINE_I18N_PATH = REPO_ROOT / "assets" / "misc" / "locales" / "pipeline_i18n.json"

TEXT_TABLE_DIR = REPO_ROOT / "tools" / "i18n"
TEXT_TABLE_FILES = {
    "zh_cn": TEXT_TABLE_DIR / "I18nTextTable_CN.json",
    "zh_tc": TEXT_TABLE_DIR / "I18nTextTable_TC.json",
    "en_us": TEXT_TABLE_DIR / "I18nTextTable_EN.json",
    "ja_jp": TEXT_TABLE_DIR / "I18nTextTable_JP.json",
}


def load_json(path: Path) -> Any:
    with path.open("r", encoding="utf-8") as f:
        return jsonc.load(f)


def save_json(path: Path, data: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as f:
        jsonc.dump(data, f, ensure_ascii=False, indent=4)


def iter_pipeline_files() -> List[Path]:
    files: List[Path] = []
    for base in PIPELINE_DIRS:
        if not base.is_dir():
            continue
        for p in base.rglob("*.json"):
            files.append(p)
    return files


_CJK_RE = re.compile(r"[\u4e00-\u9fff]")
_LATIN_HANGUL_RE = re.compile(r"[A-Za-z\u1100-\u11FF\u3130-\u318F\uAC00-\uD7AF]")


def _is_likely_zh_cn(text: str) -> bool:
    # 只要包含汉字且不含拉丁 / 韩文，就视作 zh 文本
    return bool(_CJK_RE.search(text)) and not _LATIN_HANGUL_RE.search(text)


def extract_expected(
    node: Dict[str, Any],
    cn_reverse: Optional[Dict[str, List[str]]] = None,
) -> Optional[str]:
    """
    从节点中提取 expected 文本：
    1) node["action"]["expected"]
    2) node["expected"]
    3) node["recognition"]["param"]["expected"]
    若为数组则用 "|" 拼接为一个字符串。
    """
    expected: Any = None

    action = node.get("action")
    if isinstance(action, dict) and "expected" in action:
        expected = action.get("expected")

    if expected is None and "expected" in node:
        expected = node.get("expected")

    rec = node.get("recognition")
    if expected is None and isinstance(rec, dict):
        param = rec.get("param")
        if isinstance(param, dict) and "expected" in param:
            expected = param.get("expected")

    if expected is None:
        return None

    if isinstance(expected, list):
        # 若 expected 为列表，优先从中挑选在 zh_cn 文本表中“命中”的那一项，
        # 否则再退回到仅保留看起来像 zh_cn 的第一项，最后才拼接所有为 "|".
        texts = [item for item in expected if isinstance(item, str)]
        if not texts:
            return None

        if cn_reverse:
            for t in texts:
                if t in cn_reverse:
                    return t

        for t in texts:
            if _is_likely_zh_cn(t):
                return t

        # 没有命中任何 zh_cn 文本表，也没有明显的 zh_cn 项，使用原来的 "|" 拼接方式
        return "|".join(texts)

    if isinstance(expected, str):
        return expected

    return None


def is_ocr_node(obj: Dict[str, Any]) -> bool:
    rec = obj.get("recognition")
    if isinstance(rec, str):
        return rec.upper() == "OCR"
    if isinstance(rec, dict):
        t = rec.get("type")
        return isinstance(t, str) and t.upper() == "OCR"
    return False


def walk_for_ocr_nodes(
    obj: Any,
    path_stack: List[str],
    cn_reverse: Optional[Dict[str, List[str]]],
    results: List[Tuple[str, str, Optional[str]]],
) -> None:
    """
    深度优先遍历 JSON，收集 OCR 节点：
    - node_name: 当前字典在父级中的 key
    - expected: 提取到的 expected 文本
    - doc: doc/desc/description 中任意一个
    """
    if isinstance(obj, dict):
        if is_ocr_node(obj):
            node_name = path_stack[-1] if path_stack else ""
            expected = extract_expected(obj, cn_reverse)
            doc = obj.get("doc") or obj.get("desc") or obj.get("description")
            if expected:
                results.append(
                    (node_name, expected, doc if isinstance(doc, str) else None)
                )

        for key, value in obj.items():
            path_stack.append(str(key))
            walk_for_ocr_nodes(value, path_stack, cn_reverse, results)
            path_stack.pop()
    elif isinstance(obj, list):
        for idx, value in enumerate(obj):
            path_stack.append(f"[{idx}]")
            walk_for_ocr_nodes(value, path_stack, cn_reverse, results)
            path_stack.pop()


def build_cn_reverse_index(cn_table: Dict[str, str]) -> Dict[str, List[str]]:
    """
    将 I18nTextTable_CN.json 反向索引：
    中文文本 -> [ID1, ID2, ...]
    """
    reverse: Dict[str, List[str]] = {}
    for _id, text in cn_table.items():
        if not isinstance(text, str):
            continue
        reverse.setdefault(text, []).append(_id)
    return reverse


def load_text_tables() -> Tuple[Dict[str, str], Dict[str, Dict[str, str]]]:
    """
    加载四种语言的文本表：
    - 返回 (cn_table, other_tables)
      其中 other_tables[lang][id] = text
    """
    tables: Dict[str, Dict[str, str]] = {}
    for lang, path in TEXT_TABLE_FILES.items():
        if not path.is_file():
            tables[lang] = {}
            continue
        tables[lang] = load_json(path)

    cn_table = tables.get("zh_cn", {})
    return cn_table, tables


def main() -> int:
    print(f"[i18n] repo root: {REPO_ROOT}")

    try:
        existing = load_json(PIPELINE_I18N_PATH)
    except FileNotFoundError:
        existing = {}

    if not isinstance(existing, dict):
        print(f"[i18n] pipeline_i18n.json 结构异常，期望为对象：{PIPELINE_I18N_PATH}")
        return 1

    cn_table, all_tables = load_text_tables()
    cn_reverse = build_cn_reverse_index(cn_table)

    updated = dict(existing)

    pipeline_files = iter_pipeline_files()
    print(f"[i18n] 扫描 JSON 文件数：{len(pipeline_files)}")

    for json_path in pipeline_files:
        try:
            data = load_json(json_path)
        except Exception as e:  # noqa: BLE001
            print(f"[i18n] 解析失败，跳过：{json_path} ({e})")
            continue

        ocr_nodes: List[Tuple[str, str, Optional[str]]] = []
        walk_for_ocr_nodes(data, [], cn_reverse, ocr_nodes)
        if not ocr_nodes:
            continue

        rel_path = "./" + str(json_path.relative_to(REPO_ROOT).as_posix())

        for node_name, zh_cn_text, doc in ocr_nodes:
            if not node_name:
                continue

            entry = updated.get(node_name, {})

            # 保留已有 doc，若为空则用新的
            if doc:
                entry.setdefault("doc", doc)

            entry["path"] = rel_path

            i18n = entry.get("i18n")
            if not isinstance(i18n, dict):
                i18n = {}

            # 写入 / 保持 zh_cn
            if not i18n.get("zh_cn"):
                i18n["zh_cn"] = zh_cn_text

            # 尝试通过文本表补全 zh_tc / en_us / ja_jp
            ids = cn_reverse.get(zh_cn_text, [])
            target_id = ids[0] if ids else None
            if target_id:
                for lang_key, table_key in (
                    ("zh_tc", "zh_tc"),
                    ("en_us", "en_us"),
                    ("ja_jp", "ja_jp"),
                ):
                    if not i18n.get(lang_key):
                        table = all_tables.get(table_key, {})
                        text = table.get(target_id)
                        if isinstance(text, str):
                            i18n[lang_key] = text

            entry["i18n"] = i18n
            updated[node_name] = entry

    save_json(PIPELINE_I18N_PATH, updated)
    print(f"[i18n] 已更新：{PIPELINE_I18N_PATH}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
