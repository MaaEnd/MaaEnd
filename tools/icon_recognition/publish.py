"""从下载缓存一次生成 IconRecognition catalog 与五语言名称。"""

from __future__ import annotations

import argparse
import json
import shutil
from collections import OrderedDict
from dataclasses import dataclass
from pathlib import Path

from catalog import build_catalog, write_catalog
from download import validate_icon_png_bytes
from fixed_items import FIXED_ITEMS
from localization import LOCALE_MAP, build_locale_values, generate_locales, load_json_object


@dataclass(frozen=True)
class PublishPaths:
    item_source: Path
    image_root: Path
    asset_image_root: Path
    catalog_output: Path
    localization_item_source: Path
    weapon_source: Path
    language_root: Path
    locale_root: Path


def default_publish_paths(repo_root: str | Path | None = None) -> PublishPaths:
    root = Path(repo_root) if repo_root is not None else Path(__file__).resolve().parents[2]
    cache_root = root / "tools" / "icon_recognition" / ".cache" / "downloads"
    return PublishPaths(
        item_source=cache_root / "item.json",
        image_root=cache_root / "images",
        asset_image_root=root / "assets" / "resource" / "image" / "IconRecognition",
        catalog_output=root / "assets" / "data" / "IconRecognition" / "recognition_items.json",
        localization_item_source=cache_root / "item_mini_table.json",
        weapon_source=cache_root / "weapons.json",
        language_root=cache_root,
        locale_root=root / "assets" / "locales" / "interface",
    )


def publish(paths: PublishPaths) -> tuple[int, dict[str, int]]:
    source = json.loads(paths.item_source.read_text(encoding="utf-8-sig"), object_pairs_hook=OrderedDict)
    if not isinstance(source, dict):
        raise ValueError(f"JSON 顶层必须是对象: {paths.item_source}")
    catalog = build_catalog(source, paths.image_root)
    paths.catalog_output.parent.mkdir(parents=True, exist_ok=True)
    write_catalog(catalog, paths.catalog_output)
    locale_counts = generate_locales(
        paths.catalog_output,
        paths.localization_item_source,
        paths.weapon_source,
        paths.language_root,
        paths.locale_root,
    )
    return len(catalog), locale_counts


def publish_fixed_items(paths: PublishPaths) -> int:
    fixed_source = {
        item_id: {
            "name": payload["i18nKey"],
            "category": payload["category"],
            "storageKind": payload["storageKind"],
            "categoryType": payload["categoryType"],
            "rarity": payload["rarity"],
            "iconId": payload["iconId"],
            "fullContainers": [],
        }
        for item_id, payload in FIXED_ITEMS.items()
    }
    rarity_directories = (
        [path for path in paths.asset_image_root.iterdir() if path.is_dir()]
        if paths.asset_image_root.is_dir()
        else []
    )
    for payload in FIXED_ITEMS.values():
        source = paths.image_root / str(payload["rarity"]) / f"{payload['iconId']}.png"
        validate_icon_png_bytes(source.read_bytes())
        for rarity_directory in rarity_directories:
            stale = rarity_directory / source.name
            expected = paths.asset_image_root / str(payload["rarity"]) / source.name
            if stale.is_file() and stale != expected:
                stale.unlink()
        destination = paths.asset_image_root / str(payload["rarity"]) / source.name
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, destination)

    fixed_catalog = build_catalog(fixed_source, paths.asset_image_root)
    if len(fixed_catalog) != len(FIXED_ITEMS):
        raise ValueError("固定物品图标未完整生成 catalog")
    catalog = load_json_object(paths.catalog_output)
    catalog.update(fixed_catalog)
    write_catalog(OrderedDict(sorted(catalog.items())), paths.catalog_output)

    for locale, (weapon_language, path_language) in LOCALE_MAP.items():
        translations = load_json_object(paths.language_root / f"lang_{path_language}.json")
        values = build_locale_values(fixed_catalog, {}, weapon_language, translations)
        locale_path = paths.locale_root / f"{locale}.json"
        interface = load_json_object(locale_path)
        interface.update(values)
        locale_path.write_text(
            json.dumps(interface, ensure_ascii=False, indent=4) + "\n",
            encoding="utf-8",
        )
    return len(fixed_catalog)


def main() -> int:
    defaults = default_publish_paths()
    parser = argparse.ArgumentParser(description="生成 IconRecognition 发布资源")
    parser.add_argument("--item-source", type=Path, default=defaults.item_source)
    parser.add_argument("--image-root", type=Path, default=defaults.image_root)
    parser.add_argument("--asset-image-root", type=Path, default=defaults.asset_image_root)
    parser.add_argument("--catalog-output", type=Path, default=defaults.catalog_output)
    parser.add_argument("--localization-item-source", type=Path, default=defaults.localization_item_source)
    parser.add_argument("--weapon-source", type=Path, default=defaults.weapon_source)
    parser.add_argument("--language-root", type=Path, default=defaults.language_root)
    parser.add_argument("--locale-root", type=Path, default=defaults.locale_root)
    parser.add_argument("--fixed-only", action="store_true")
    args = parser.parse_args()
    paths = PublishPaths(
        item_source=args.item_source,
        image_root=args.image_root,
        asset_image_root=args.asset_image_root,
        catalog_output=args.catalog_output,
        localization_item_source=args.localization_item_source,
        weapon_source=args.weapon_source,
        language_root=args.language_root,
        locale_root=args.locale_root,
    )
    if args.fixed_only:
        print(json.dumps({"fixed": publish_fixed_items(paths)}, ensure_ascii=False, indent=4))
        return 0
    count, locale_counts = publish(paths)
    print(json.dumps({"catalog": count, "locales": locale_counts}, ensure_ascii=False, indent=4))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
