#!/usr/bin/env python3
# -*- coding: utf-8 -*-

"""
根据 assets/misc/locales/pipeline_i18n.json 中的 OCR i18n 配置，
为多语言资源目录生成/更新精简的 pipeline 资源：

    assets/resource_en/pipeline/...
    assets/resource_tc/pipeline/...
    assets/resource_jp/pipeline/...

说明：
- zh_cn 原始资源位于 assets/resource/pipeline/...，不做修改
- 本脚本只使用 pipeline_i18n.json 中的 i18n.{en_us, zh_tc, ja_jp}
  来生成对应的精简 JSON，形如：

    {
        "NodeName": {
            "expected": "Localized Expected Text"
        },
        "OtherNode": {
            "expected": "..."
        }
    }

使用方式（在仓库根目录运行）：

    python tools/i18n/apply_pipeline_i18n_to_resources.py
"""

from __future__ import annotations

import jsonc
from pathlib import Path
from typing import Any, Dict


REPO_ROOT = Path(__file__).resolve().parents[2]

PIPELINE_I18N_PATH = REPO_ROOT / "assets" / "misc" / "locales" / "pipeline_i18n.json"

LANG_TO_RESOURCE_ROOT = {
    "en_us": REPO_ROOT / "assets" / "resource_en" / "pipeline",
    "zh_tc": REPO_ROOT / "assets" / "resource_tc" / "pipeline",
    "ja_jp": REPO_ROOT / "assets" / "resource_jp" / "pipeline",
}


def load_json(path: Path) -> Any:
    with path.open("r", encoding="utf-8") as f:
        return jsonc.load(f)


def save_json(path: Path, data: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as f:
        jsonc.dump(data, f, ensure_ascii=False, indent=4)


def compute_target_path(lang: str, src_path: str) -> Path | None:
    """
    根据 pipeline_i18n.json 中的原始 path 计算目标语言资源文件路径。
    例如：
        ./assets/resource/pipeline/AutoFight/Recognition.json
    -> assets/resource_en/pipeline/AutoFight/Recognition.json
    """
    root = LANG_TO_RESOURCE_ROOT.get(lang)
    if root is None:
        return None

    # 规范化 path 字符串
    rel = src_path.lstrip("./")
    parts = rel.split("/")
    try:
        idx = parts.index("pipeline")
    except ValueError:
        # 路径中没有 pipeline，跳过
        return None

    suffix_parts = parts[idx + 1 :]
    if not suffix_parts:
        return None

    return root.joinpath(*suffix_parts)


def main() -> int:
    if not PIPELINE_I18N_PATH.is_file():
        print(f"[apply] pipeline_i18n.json 不存在：{PIPELINE_I18N_PATH}")
        return 1

    data = load_json(PIPELINE_I18N_PATH)
    if not isinstance(data, dict):
        print(f"[apply] pipeline_i18n.json 结构异常，期望为对象")
        return 1

    # files_map[lang][target_path] = { node_name: { "expected": text } }
    files_map: Dict[str, Dict[Path, Dict[str, Dict[str, str]]]] = {
        lang: {} for lang in LANG_TO_RESOURCE_ROOT.keys()
    }

    for node_name, entry in data.items():
        if not isinstance(entry, dict):
            continue

        src_path = entry.get("path")
        if not isinstance(src_path, str):
            continue

        i18n = entry.get("i18n", {})
        if not isinstance(i18n, dict):
            continue

        for lang in ("en_us", "zh_tc", "ja_jp"):
            text = i18n.get(lang)
            if not isinstance(text, str) or not text.strip():
                continue

            target_path = compute_target_path(lang, src_path)
            if target_path is None:
                continue

            lang_files = files_map[lang]
            nodes = lang_files.setdefault(target_path, {})
            nodes[node_name] = {"expected": text}

    # 实际写入/合并到各语言资源文件
    total_files = 0
    for lang, lang_files in files_map.items():
        for path, nodes in lang_files.items():
            if path.is_file():
                try:
                    existing = load_json(path)
                except Exception:  # noqa: BLE001
                    existing = {}
            else:
                existing = {}

            if not isinstance(existing, dict):
                existing = {}

            # 合并：以新生成内容为准覆盖同名节点的 expected
            for node_name, node_value in nodes.items():
                if not isinstance(node_value, dict):
                    continue
                cur = existing.get(node_name)
                if isinstance(cur, dict):
                    cur.update(node_value)
                    existing[node_name] = cur
                else:
                    existing[node_name] = node_value

            save_json(path, existing)
            total_files += 1
            print(f"[apply] 更新 {lang}: {path.relative_to(REPO_ROOT)}")

    print(f"[apply] 完成，共写入/更新 {total_files} 个多语言资源文件。")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

