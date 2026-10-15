"""把 catalog item ID 同步到 MaaEnd 各语言 locale 文件。

名字直接取自 zmdmap 数据与 ``fixed_items.json`` 里的 ``names``（多语言），不再查 ``lang_*.json`` 的
哈希表。
"""

from __future__ import annotations

import argparse
import json
from collections.abc import Mapping
from pathlib import Path
from typing import Any

import json5

from fixed_items import FIXED_ITEMS
from text import clean_text


LOCALE_MAP = {
    "zh_cn": ("CN", "zh-CN"),
    "zh_tw": ("TC", "zh-TW"),
    "en_us": ("EN", "en-US"),
    "ja_jp": ("JP", "ja-JP"),
    "ko_kr": ("KR", "ko-KR"),
}
ICON_NAME_PREFIX = "iconRecognition.name."


def _name(
    payload: Mapping[str, Any],
    key: str,
    language: str,
    fallback: object,
) -> str:
    names = payload.get("names")
    if isinstance(names, Mapping):
        if language not in names:
            raise ValueError(f"{key}.names 缺少大写语言 key: {language}")
        return clean_text(names[language], field=f"{key}.names.{language}")
    return clean_text(payload.get("name") or fallback, field=f"{key}.name")


def build_locale_values(
    catalog: Mapping[str, Any],
    source: Mapping[str, Any],
    language: str,
) -> dict[str, str]:
    by_icon = {
        payload.get("iconId"): payload
        for payload in source.values()
        if isinstance(payload, Mapping) and isinstance(payload.get("iconId"), str)
    }
    values: dict[str, str] = {}
    for item_id in sorted(catalog):
        payload = source.get(item_id, {})
        if not isinstance(payload, Mapping):
            raise ValueError(f"{item_id} source must be an object")
        record = catalog[item_id]
        fallback = record.get("name") if isinstance(record, Mapping) else None
        localized = _name(payload, item_id, language, fallback)
        fluid_icon = record.get("fluidIconId") if isinstance(record, Mapping) else None
        if isinstance(fluid_icon, str) and fluid_icon:
            fluid = by_icon.get(fluid_icon, {})
            fluid_name = _name(fluid, fluid_icon, language, fluid_icon)
            container_name = localized.split("(", 1)[0]
            localized = f"{container_name}({fluid_name})"
        values[f"iconRecognition.name.{item_id}"] = localized
    return values


def update_interface_locale(
    path: str | Path,
    values: Mapping[str, str],
    *,
    remove_stale: bool = True,
) -> None:
    target = Path(path)
    interface = json5.loads(target.read_text(encoding="utf-8"))
    if not isinstance(interface, dict):
        raise ValueError(f"locale must be an object: {target}")

    existing_keys = [key for key in interface if key.startswith(ICON_NAME_PREFIX)]
    retained_keys = (
        [key for key in existing_keys if key in values]
        if remove_stale
        else existing_keys
    )
    retained_set = set(retained_keys)
    item_keys = retained_keys + sorted(key for key in values if key not in retained_set)

    # 以首个既有物品键为锚集中重建分组，避免新增键散落到文件末尾。
    updated: dict[str, Any] = {}
    item_group_written = False
    for key, value in interface.items():
        if key.startswith(ICON_NAME_PREFIX):
            if item_group_written:
                continue
            for item_key in item_keys:
                updated[item_key] = (
                    values[item_key]
                    if item_key in values
                    else interface[item_key]
                )
            item_group_written = True
            continue
        updated[key] = value
    if not item_group_written:
        for item_key in item_keys:
            updated[item_key] = values[item_key]

    if updated == interface:
        return
    target.write_text(json.dumps(updated, ensure_ascii=False, indent=4) + "\n", encoding="utf-8")


def load_json_object(path: str | Path) -> dict[str, Any]:
    source = Path(path)
    payload = json5.loads(source.read_text(encoding="utf-8-sig"))
    if not isinstance(payload, dict):
        raise ValueError(f"JSON 顶层必须是对象: {source}")
    return payload


def build_source_index(
    items: Mapping[str, Any],
    weapons: Mapping[str, Any],
    fixed_items: Mapping[str, Any] | None = None,
) -> dict[str, Any]:
    """名字来源索引：物品表 + 武器表（含 ``internal_id`` 别名）+ 固定物品。"""
    result = dict(items)
    for top_key, payload in weapons.items():
        if not isinstance(top_key, str) or not isinstance(payload, Mapping):
            raise ValueError("weapons.json 必须是 ID 到对象的映射")
        aliases = [top_key]
        internal_id = payload.get("internal_id")
        if isinstance(internal_id, str) and internal_id:
            aliases.append(internal_id)
        for alias in aliases:
            existing = result.get(alias)
            if existing is not None and existing is not payload:
                raise ValueError(f"物品与武器本地化 ID 冲突: {alias}")
            result[alias] = payload
    for item_id, payload in (fixed_items or FIXED_ITEMS).items():
        existing = result.get(item_id)
        if existing is not None and existing is not payload:
            raise ValueError(f"固定物品与远端物品本地化 ID 冲突: {item_id}")
        result[item_id] = payload
    return result


def generate_locales(
    catalog_path: str | Path,
    item_source_path: str | Path,
    weapon_source_path: str | Path,
    locale_root: str | Path,
) -> dict[str, int]:
    catalog = load_json_object(catalog_path)
    source = build_source_index(
        load_json_object(item_source_path),
        load_json_object(weapon_source_path),
    )
    locale_directory = Path(locale_root)
    counts: dict[str, int] = {}
    for locale, (weapon_language, _) in LOCALE_MAP.items():
        values = build_locale_values(catalog, source, weapon_language)
        if len(values) != len(catalog):
            raise ValueError(f"{locale} 本地化 key 数与 catalog 不一致")
        update_interface_locale(locale_directory / f"{locale}.json", values)
        counts[locale] = len(values)
    return counts


def main() -> int:
    parser = argparse.ArgumentParser(description="生成 IconRecognition 各语言名称")
    parser.add_argument("--catalog", type=Path, required=True)
    parser.add_argument("--item-source", type=Path, required=True)
    parser.add_argument("--weapon-source", type=Path, required=True)
    parser.add_argument(
        "--locale-root",
        type=Path,
        default=Path("assets/locales/interface"),
    )
    args = parser.parse_args()
    counts = generate_locales(
        args.catalog,
        args.item_source,
        args.weapon_source,
        args.locale_root,
    )
    print(json.dumps(counts, ensure_ascii=False, indent=4))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
