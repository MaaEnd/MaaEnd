"""把 catalog item ID 同步到 MaaEnd 五语言 locale 文件。"""

from __future__ import annotations

import argparse
import json
from collections.abc import Mapping
from pathlib import Path
from typing import Any

from text import clean_text


LOCALE_MAP = {
    "zh_cn": ("CN", "zh-CN"),
    "zh_tw": ("TC", "zh-TW"),
    "en_us": ("EN", "en-US"),
    "ja_jp": ("JP", "ja-JP"),
    "ko_kr": ("KR", "ko-KR"),
}

FIXED_NAME_KEYS = {
    "item_gold": "1b858553aff0ccfee876ed0367300534",
    "item_diamond": "aa5edf17dd5cbb87661ec9f3d86d353e",
    "item_gachabyproducts_weapongold": "011c0bffc4a5c215eb651edaf3e5b929",
}


def _name(
    payload: Mapping[str, Any],
    key: str,
    language: str,
    fallback: object,
    translations: Mapping[str, Any],
) -> str:
    names = payload.get("names")
    if isinstance(names, Mapping):
        if language not in names:
            raise ValueError(f"{key}.names 缺少大写语言 key: {language}")
        return clean_text(names[language], field=f"{key}.names.{language}")
    name = payload.get("name")
    if isinstance(name, str) and name in translations:
        return clean_text(translations[name], field=f"{key}.name")
    fixed_key = FIXED_NAME_KEYS.get(key)
    if fixed_key and fixed_key in translations:
        return clean_text(translations[fixed_key], field=f"{key}.name")
    return clean_text(name or fallback, field=f"{key}.name")


def build_locale_values(
    catalog: Mapping[str, Any],
    source: Mapping[str, Any],
    language: str,
    translations: Mapping[str, Any] | None = None,
) -> dict[str, str]:
    selected_translations = translations or {}
    by_icon = {
        payload.get("iconId"): payload
        for payload in source.values()
        if isinstance(payload, Mapping) and isinstance(payload.get("iconId"), str)
    }
    values: dict[str, str] = {}
    for item_id in catalog:
        payload = source.get(item_id, {})
        if not isinstance(payload, Mapping):
            raise ValueError(f"{item_id} source must be an object")
        record = catalog[item_id]
        fallback = record.get("name") if isinstance(record, Mapping) else None
        localized = _name(payload, item_id, language, fallback, selected_translations)
        fluid_icon = record.get("fluidIconId") if isinstance(record, Mapping) else None
        if isinstance(fluid_icon, str) and fluid_icon:
            fluid = by_icon.get(fluid_icon, {})
            fluid_name = _name(fluid, fluid_icon, language, fluid_icon, selected_translations)
            container_name = localized.split("(", 1)[0]
            localized = f"{container_name}({fluid_name})"
        values[f"iconRecognition.name.{item_id}"] = localized
    return values


def update_interface_locale(path: str | Path, values: Mapping[str, str]) -> None:
    target = Path(path)
    interface = json.loads(target.read_text(encoding="utf-8"))
    if not isinstance(interface, dict):
        raise ValueError(f"locale must be an object: {target}")
    stale = [
        key
        for key in interface
        if key.startswith("iconRecognition.name.") and key not in values
    ]
    for key in stale:
        del interface[key]
    interface.update(values)
    target.write_text(json.dumps(interface, ensure_ascii=False, indent=4) + "\n", encoding="utf-8")


def load_json_object(path: str | Path) -> dict[str, Any]:
    source = Path(path)
    payload = json.loads(source.read_text(encoding="utf-8-sig"))
    if not isinstance(payload, dict):
        raise ValueError(f"JSON 顶层必须是对象: {source}")
    return payload


def build_source_index(
    items: Mapping[str, Any],
    weapons: Mapping[str, Any],
) -> dict[str, Any]:
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
    return result


def generate_locales(
    catalog_path: str | Path,
    item_source_path: str | Path,
    weapon_source_path: str | Path,
    language_root: str | Path,
    locale_root: str | Path,
) -> dict[str, int]:
    catalog = load_json_object(catalog_path)
    source = build_source_index(
        load_json_object(item_source_path),
        load_json_object(weapon_source_path),
    )
    language_directory = Path(language_root)
    locale_directory = Path(locale_root)
    counts: dict[str, int] = {}
    for locale, (weapon_language, path_language) in LOCALE_MAP.items():
        translations = load_json_object(
            language_directory / f"lang_{path_language}.json"
        )
        values = build_locale_values(
            catalog,
            source,
            weapon_language,
            translations,
        )
        if len(values) != len(catalog):
            raise ValueError(f"{locale} 本地化 key 数与 catalog 不一致")
        update_interface_locale(locale_directory / f"{locale}.json", values)
        counts[locale] = len(values)
    return counts


def main() -> int:
    parser = argparse.ArgumentParser(description="生成 IconRecognition 五语言名称")
    parser.add_argument("--catalog", type=Path, required=True)
    parser.add_argument("--item-source", type=Path, required=True)
    parser.add_argument("--weapon-source", type=Path, required=True)
    parser.add_argument("--language-root", type=Path, required=True)
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
        args.language_root,
        args.locale_root,
    )
    print(json.dumps(counts, ensure_ascii=False, indent=4))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
