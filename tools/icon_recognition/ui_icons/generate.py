"""根据已发布 IconRecognition catalog 和图片生成 UI 图标。"""

from __future__ import annotations

import argparse
import json
import re
from collections.abc import Mapping
from pathlib import Path
from typing import Any

import json5
from PIL import Image, ImageColor


TARGET_SIZE = 32
ITEM_BOX_SIZE = (30, 28)
COMPOSITE_CONTENT_SIZE = round(TARGET_SIZE * 7 / 16)
QUALITY_BAR_HEIGHT = 2
COMMON_RARITY_BAR_HEIGHT = round(TARGET_SIZE / 3)
SIX_BACKGROUND_WIDTH = round(TARGET_SIZE * 1.2)
RARITY_COLORS = {
    1: "#9B9B9B",
    2: "#ABCE42",
    3: "#26BBFD",
    4: "#9452FA",
    5: "#FFBB03",
    6: "#FF7100",
}


def default_paths(repo_root: str | Path | None = None) -> dict[str, Path]:
    root = Path(repo_root) if repo_root is not None else Path(__file__).resolve().parents[3]
    tool_root = root / "tools" / "icon_recognition" / "ui_icons"
    return {
        "catalog": root / "assets" / "data" / "IconRecognition" / "recognition_items.json",
        "image_root": root / "assets" / "resource" / "image" / "IconRecognition",
        "output_root": root / "assets" / "resource" / "image" / "UI" / "Item",
        "config": tool_root / "config.jsonc",
        "mask_root": tool_root / "assets",
    }


def _load_json(path: Path) -> Mapping[str, Any]:
    """统一按 JSONC 读取；JSON 是 JSONC 的兼容子集。"""
    value = json5.loads(path.read_text(encoding="utf-8-sig"))
    if not isinstance(value, Mapping):
        raise ValueError(f"JSON 顶层必须是对象: {path}")
    return value


def _string_list(config: Mapping[str, Any], key: str) -> tuple[str, ...]:
    value = config.get(key, [])
    if not isinstance(value, list) or not all(isinstance(item, str) for item in value):
        raise ValueError(f"UI 图标集配置的 {key} 必须是字符串数组")
    return tuple(value)


def _parse_config(config: Mapping[str, Any]) -> dict[str, tuple[str, ...]]:
    return {
        key: _string_list(config, key)
        for key in (
            "item_ids",
            "item_filters",
            "additional_item_filters",
            "excluded_item_ids",
        )
    }


def _parse_exclude_rules(config: Mapping[str, Any]) -> tuple[Mapping[str, Any], ...]:
    rules = config.get("exclude_rules", [])
    if not isinstance(rules, list) or not all(isinstance(rule, Mapping) for rule in rules):
        raise ValueError("UI 图标集配置的 exclude_rules 必须是对象数组")

    normalized_rules: list[Mapping[str, Any]] = []
    for rule in rules:
        item_filter = rule.get("item_filter")
        sub_rules = rule.get("sub_rules")
        if not isinstance(item_filter, str) or not isinstance(sub_rules, list):
            raise ValueError("UI 图标集排除规则必须包含 item_filter 和 sub_rules")
        _parse_filter(item_filter)

        normalized_sub_rules: list[Mapping[str, Any]] = []
        for sub_rule in sub_rules:
            if not isinstance(sub_rule, Mapping):
                raise ValueError("UI 图标集排除规则的 sub_rules 必须是对象数组")
            rarity_rule = sub_rule.get("rarity")
            if not isinstance(rarity_rule, Mapping):
                raise ValueError("UI 图标集排除规则的子规则必须包含 rarity")
            normalized_sub_rules.append({"rarity": _parse_rarity_rule(rarity_rule)})
        normalized_rules.append(
            {"item_filter": item_filter, "sub_rules": normalized_sub_rules}
        )
    return tuple(normalized_rules)


def _parse_filter(item_filter: str) -> tuple[str, str]:
    try:
        storage_kind, category_type = item_filter.split(":", 1)
    except ValueError as exc:
        raise ValueError(f"非法 UI 图标集筛选条件: {item_filter}") from exc
    if not storage_kind or not category_type:
        raise ValueError(f"非法 UI 图标集筛选条件: {item_filter}")
    return storage_kind, category_type


def _parse_rarity_rule(rarity_rule: Mapping[str, Any]) -> Mapping[str, list[int]]:
    operators = [operator for operator in ("in", "not_in") if operator in rarity_rule]
    if len(operators) != 1:
        raise ValueError("UI 图标集排除规则的 rarity 必须且只能包含 in 或 not_in")
    operator = operators[0]
    values = rarity_rule[operator]
    if not isinstance(values, list) or not all(
        isinstance(value, int) and not isinstance(value, bool) for value in values
    ):
        raise ValueError(f"UI 图标集排除规则的 rarity.{operator} 必须是整数数组")
    return {operator: list(values)}


def _matches_filter(record: Mapping[str, Any], item_filter: str) -> bool:
    storage_kind, category_type = _parse_filter(item_filter)
    return (
        record.get("storageKind") == storage_kind
        and (category_type == "*" or record.get("categoryType") == category_type)
    )


def _matches_rarity_rule(record: Mapping[str, Any], rarity_rule: Mapping[str, Any]) -> bool:
    parsed_rule = _parse_rarity_rule(rarity_rule)
    operator, values = next(iter(parsed_rule.items()))
    if operator == "in":
        return record.get("rarity") in values
    return record.get("rarity") not in values


def _matches_exclude_rule(record: Mapping[str, Any], rule: Mapping[str, Any]) -> bool:
    item_filter = rule.get("item_filter")
    sub_rules = rule.get("sub_rules")
    if not isinstance(item_filter, str) or not isinstance(sub_rules, list):
        raise ValueError("UI 图标集排除规则必须包含 item_filter 和 sub_rules")
    if not _matches_filter(record, item_filter):
        return False
    for sub_rule in sub_rules:
        if not isinstance(sub_rule, Mapping):
            raise ValueError("UI 图标集排除规则的 sub_rules 必须是对象数组")
        rarity_rule = sub_rule.get("rarity")
        if not isinstance(rarity_rule, Mapping):
            raise ValueError("UI 图标集排除规则的子规则必须包含 rarity")
        if _matches_rarity_rule(record, rarity_rule):
            return True
    return False


def select_items(
    catalog: Mapping[str, Mapping[str, Any]], config: Mapping[str, Any]
) -> list[tuple[str, Mapping[str, Any]]]:
    parsed = _parse_config(config)
    exclude_rules = _parse_exclude_rules(config)
    item_ids = set(parsed["item_ids"])
    excluded = set(parsed["excluded_item_ids"])
    base_filters = parsed["item_filters"]
    additional_filters = parsed["additional_item_filters"]
    unknown_ids = (item_ids | excluded) - set(catalog)
    if unknown_ids:
        raise ValueError(f"UI 图标集包含未知 item_id: {sorted(unknown_ids)}")

    selected = []
    for item_id, record in catalog.items():
        if item_id in excluded:
            continue
        if any(_matches_exclude_rule(record, rule) for rule in exclude_rules):
            continue
        base_match = (
            any(_matches_filter(record, value) for value in base_filters)
            if base_filters
            else not item_ids or item_id in item_ids
        )
        additional_match = any(_matches_filter(record, value) for value in additional_filters)
        if item_ids:
            base_match = base_match and item_id in item_ids
        if base_match or additional_match:
            selected.append((item_id, record))
    return selected


def _validate_item_id(item_id: str) -> str:
    if not re.fullmatch(r"[A-Za-z0-9_]+", item_id):
        raise ValueError(f"item_id 不能用于文件名: {item_id}")
    return item_id


def _tint_by_alpha(mask_image: Image.Image, color: str) -> Image.Image:
    mask = mask_image.convert("RGBA")
    result = Image.new("RGBA", mask.size, ImageColor.getrgb(color) + (255,))
    result.putalpha(mask.getchannel("A"))
    return result


def _contain(image: Image.Image, box_size: tuple[int, int]) -> Image.Image:
    scale = min(box_size[0] / image.width, box_size[1] / image.height)
    size = (max(1, round(image.width * scale)), max(1, round(image.height * scale)))
    return image.resize(size, Image.Resampling.LANCZOS)


def _paste_center(canvas: Image.Image, layer: Image.Image, x: float, y: float) -> None:
    canvas.alpha_composite(layer, (round(x - layer.width / 2), round(y - layer.height / 2)))


def _composite_base(item_path: Path, rarity: int, masks: Mapping[str, Image.Image]) -> Image.Image:
    canvas = Image.new("RGBA", (TARGET_SIZE, TARGET_SIZE), (0, 0, 0, 0))
    content_height = TARGET_SIZE - QUALITY_BAR_HEIGHT
    if rarity <= 5:
        background = masks["common"].resize(
            (masks["common"].width, COMMON_RARITY_BAR_HEIGHT), Image.Resampling.LANCZOS
        )
        background = background.resize((TARGET_SIZE, COMMON_RARITY_BAR_HEIGHT), Image.Resampling.LANCZOS)
        background = _tint_by_alpha(background, RARITY_COLORS[rarity])
        canvas.alpha_composite(background, (0, content_height - COMMON_RARITY_BAR_HEIGHT))
    else:
        background = masks["six"].resize(
            (SIX_BACKGROUND_WIDTH,
             round(masks["six"].height * SIX_BACKGROUND_WIDTH / masks["six"].width)),
            Image.Resampling.LANCZOS,
        )
        x = (TARGET_SIZE - background.width) // 2
        y = content_height - background.height
        visible = background.crop((max(0, -x), max(0, -y), min(background.width, TARGET_SIZE - x), min(background.height, TARGET_SIZE - y)))
        canvas.alpha_composite(visible, (max(0, x), max(0, y)))

    item = _contain(Image.open(item_path).convert("RGBA"), ITEM_BOX_SIZE)
    _paste_center(canvas, item, TARGET_SIZE / 2, 14)
    quality_bar = masks["quality"].resize((TARGET_SIZE, QUALITY_BAR_HEIGHT), Image.Resampling.LANCZOS)
    quality_bar = _tint_by_alpha(quality_bar, RARITY_COLORS[rarity])
    canvas.alpha_composite(quality_bar, (0, content_height))
    return canvas


def _overlay_content(base: Image.Image, content_path: Path) -> Image.Image:
    content = _contain(Image.open(content_path).convert("RGBA"), (COMPOSITE_CONTENT_SIZE,) * 2)
    _paste_center(base, content, TARGET_SIZE / 2, TARGET_SIZE / 2)
    return base


def _find_icon(
    image_root: Path,
    icon_id: str,
    rarity_values: tuple[str, ...],
    preferred_rarity: str | None = None,
) -> Path:
    search_rarities = ([preferred_rarity] if preferred_rarity else []) + [
        rarity for rarity in rarity_values if rarity != preferred_rarity
    ]
    matches = [
        image_root / rarity / f"{icon_id}.png"
        for rarity in search_rarities
        if (image_root / rarity / f"{icon_id}.png").is_file()
    ]
    if not matches:
        raise FileNotFoundError(f"找不到已发布图标: {icon_id}.png")
    return matches[0]


def generate_ui_icons(
    catalog_path: str | Path | None = None,
    image_root: str | Path | None = None,
    config_path: str | Path | None = None,
    output_root: str | Path | None = None,
    mask_root: str | Path | None = None,
) -> dict[str, int]:
    defaults = default_paths()
    catalog_path = catalog_path or defaults["catalog"]
    image_root = image_root or defaults["image_root"]
    config_path = config_path or defaults["config"]
    output_root = output_root or defaults["output_root"]
    mask_root = mask_root or defaults["mask_root"]
    catalog = _load_json(Path(catalog_path))
    config = _load_json(Path(config_path))
    selected = select_items(catalog, config)
    rarity_values = tuple(
        sorted(
            {
                str(record["rarity"])
                for record in catalog.values()
                if isinstance(record, Mapping) and "rarity" in record
            }
        )
    )
    image_root = Path(image_root)
    output_root = Path(output_root)
    mask_root = Path(mask_root)
    masks = {
        "common": Image.open(mask_root / "bg_item_rarity_bar_common.png").convert("RGBA"),
        "six": Image.open(mask_root / "bg_6starcolour_b.png").convert("RGBA"),
        "quality": Image.open(mask_root / "bg_item_quality_bar.png").convert("RGBA"),
    }
    output_root.mkdir(parents=True, exist_ok=True)
    stats = {"selected": len(selected), "generated": 0, "skipped": 0, "missing": 0}
    for item_id, record in selected:
        destination = output_root / f"{_validate_item_id(item_id)}.png"
        if destination.is_file():
            try:
                with Image.open(destination) as existing:
                    if existing.size == (TARGET_SIZE, TARGET_SIZE):
                        stats["skipped"] += 1
                        continue
            except OSError:
                pass
            # 已有文件即使尺寸异常也不覆盖，避免破坏 CI 优化过的资源。
            stats["skipped"] += 1
            continue
        try:
            base = _composite_base(
                _find_icon(
                    image_root,
                    str(record["iconId"]),
                    rarity_values,
                    preferred_rarity=str(record["rarity"]),
                ),
                int(record["rarity"]),
                masks,
            )
            fluid_icon_id = record.get("fluidIconId")
            if isinstance(fluid_icon_id, str) and fluid_icon_id:
                base = _overlay_content(
                    base, _find_icon(image_root, fluid_icon_id, rarity_values)
                )
        except FileNotFoundError:
            stats["missing"] += 1
            continue
        base.save(destination)
        stats["generated"] += 1
    if stats["missing"]:
        raise FileNotFoundError(f"有 {stats['missing']} 个 UI 图标缺少已发布素材")
    return stats


def main() -> int:
    defaults = default_paths()
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--catalog", type=Path, default=defaults["catalog"])
    parser.add_argument("--image-root", type=Path, default=defaults["image_root"])
    parser.add_argument("--config", type=Path, default=defaults["config"])
    parser.add_argument("--output-root", type=Path, default=defaults["output_root"])
    parser.add_argument("--mask-root", type=Path, default=defaults["mask_root"])
    args = parser.parse_args()
    stats = generate_ui_icons(
        args.catalog, args.image_root, args.config, args.output_root, args.mask_root
    )
    print(json.dumps(stats, ensure_ascii=False, indent=4))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
