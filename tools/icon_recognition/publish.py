"""从下载缓存一次生成 IconRecognition catalog 与五语言名称。"""

from __future__ import annotations

import argparse
import json
from collections import OrderedDict
from dataclasses import dataclass
from pathlib import Path

from catalog import build_catalog, write_catalog
from localization import generate_locales


@dataclass(frozen=True)
class PublishPaths:
    item_source: Path
    image_root: Path
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


def main() -> int:
    defaults = default_publish_paths()
    parser = argparse.ArgumentParser(description="生成 IconRecognition 发布资源")
    parser.add_argument("--item-source", type=Path, default=defaults.item_source)
    parser.add_argument("--image-root", type=Path, default=defaults.image_root)
    parser.add_argument("--catalog-output", type=Path, default=defaults.catalog_output)
    parser.add_argument("--localization-item-source", type=Path, default=defaults.localization_item_source)
    parser.add_argument("--weapon-source", type=Path, default=defaults.weapon_source)
    parser.add_argument("--language-root", type=Path, default=defaults.language_root)
    parser.add_argument("--locale-root", type=Path, default=defaults.locale_root)
    args = parser.parse_args()
    count, locale_counts = publish(
        PublishPaths(
            item_source=args.item_source,
            image_root=args.image_root,
            catalog_output=args.catalog_output,
            localization_item_source=args.localization_item_source,
            weapon_source=args.weapon_source,
            language_root=args.language_root,
            locale_root=args.locale_root,
        )
    )
    print(json.dumps({"catalog": count, "locales": locale_counts}, ensure_ascii=False, indent=4))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
