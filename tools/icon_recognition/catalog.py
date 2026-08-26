"""从合并后的 item.json 和已下载图标生成运行时识别目录。"""

from __future__ import annotations

import argparse
import json
from collections import OrderedDict
from pathlib import Path
from typing import Any, Mapping

from text import clean_text, validate_identifier


def _icon_path(item_id: str, item: Mapping[str, Any], image_root: Path) -> Path | None:
    raw_icon_id = item.get("iconId")
    if not isinstance(raw_icon_id, str):
        raise ValueError(f"{item_id}.iconId 必须是字符串")
    if not raw_icon_id:
        return None
    icon_id = validate_identifier(raw_icon_id, field=f"{item_id}.iconId")
    rarity = item.get("rarity")
    if isinstance(rarity, bool) or not isinstance(rarity, int) or not 1 <= rarity <= 6:
        raise ValueError(f"{item_id}.rarity 必须位于 1..6")
    path = image_root / str(rarity) / f"{icon_id}.png"
    return path if path.is_file() else None


def build_catalog(source: Mapping[str, Any], image_root: str | Path) -> OrderedDict[str, dict[str, Any]]:
    image_root = Path(image_root)
    items = OrderedDict(sorted(source.items()))
    references = {
        container
        for item in items.values()
        if isinstance(item, Mapping)
        for container in item.get("fullContainers", [])
    }
    composites: dict[str, dict[str, Any]] = {}
    for fluid_id, fluid in items.items():
        if not isinstance(fluid, Mapping) or not fluid.get("fullContainers"):
            continue
        fluid_path = _icon_path(fluid_id, fluid, image_root)
        if fluid_path is None:
            continue
        fluid_name = clean_text(fluid.get("name"), field=f"{fluid_id}.name")
        for container_id in fluid["fullContainers"]:
            if container_id in composites:
                raise ValueError(f"容器被多个灌装物引用: {container_id}")
            container = items.get(container_id)
            if not isinstance(container, Mapping):
                raise ValueError(f"fullContainers 找不到顶级条目: {container_id}")
            container_path = _icon_path(container_id, container, image_root)
            if container_path is None:
                continue
            record = _record(container_id, container, container_path, fluid_path.name[:-4])
            container_name = clean_text(container.get("name"), field=f"{container_id}.name")
            record["name"] = f"{container_name}({fluid_name})"
            composites[container_id] = record

    result: OrderedDict[str, dict[str, Any]] = OrderedDict()
    for item_id, item in items.items():
        if not isinstance(item, Mapping):
            raise ValueError(f"{item_id} 必须是对象")
        if item.get("fullContainers"):
            continue
        if item_id in references:
            if item_id in composites:
                result[item_id] = composites[item_id]
            continue
        path = _icon_path(item_id, item, image_root)
        if path is not None:
            result[item_id] = _record(item_id, item, path)
    return result


def _record(item_id: str, item: Mapping[str, Any], path: Path, fluid_icon_id: str = "") -> dict[str, Any]:
    icon_id = path.stem
    record = {
        "name": clean_text(item.get("name"), field=f"{item_id}.name"),
        "category": clean_text(item.get("category"), field=f"{item_id}.category"),
        "storageKind": clean_text(item.get("storageKind"), field=f"{item_id}.storageKind"),
        "categoryType": clean_text(item.get("categoryType"), field=f"{item_id}.categoryType"),
        "rarity": int(item["rarity"]),
        "iconId": icon_id,
        "fluidIconId": fluid_icon_id,
    }
    for field in ("sortId1", "sortId2"):
        value = item.get(field)
        if value is None:
            continue
        if isinstance(value, bool) or not isinstance(value, int):
            raise ValueError(f"{item_id}.{field} 必须是整数")
        record[field] = value
    region_restricted = item.get("regionRestricted", False)
    if not isinstance(region_restricted, bool):
        raise ValueError(f"{item_id}.regionRestricted 必须是布尔值")
    if region_restricted:
        record["regionRestricted"] = True
    return record


def write_catalog(catalog: Mapping[str, Any], output: str | Path) -> None:
    ordered = OrderedDict(sorted(catalog.items()))
    Path(output).write_text(json.dumps(ordered, ensure_ascii=False, indent=4) + "\n", encoding="utf-8")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--image-root", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    source = json.loads(args.source.read_text(encoding="utf-8"), object_pairs_hook=OrderedDict)
    result = build_catalog(source, args.image_root)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    write_catalog(result, args.output)
    print(f"generated {len(result)} recognition items")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
