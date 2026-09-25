"""生成 AutoCollect 使用的 collect_points.json。"""

from __future__ import annotations

import argparse
import struct
import sys
import urllib.error
import zlib
from collections.abc import Sequence
from pathlib import Path
from typing import Any

from navzone_utils import (
    COORD_TEXT,
    ZONE_MAPS,
    describe_zones,
    load_nav_zones,
    project_to_pixel,
    read_xz,
    resolve_zone,
)
from tablecfg_utils import (
    DATA_DIR,
    DEFAULT_JSON_DATA_DIR,
    TableCfgError,
    assert_record,
    load_json_group,
    should_skip,
    write_dataset,
)
from teleport_anchors_data import build_level_index, level_of, map_of_level

LABEL = "CollectPoints"
OUTPUT_PATH = DATA_DIR / "collect_points.json"
DEFAULT_GAMEPLAY_CONFIG_DIR = DEFAULT_JSON_DATA_DIR / "GameplayConfig"
GAMEPLAY_CONFIG_NAMES = ("WorldEntityRegistry.json", "LevelBasicInfoTable.json")

DATA_BASE_URL = "https://assets.fz.wiki/output_maaend"
GAMEPLAY_CONFIG_BASE_URL: str | None = DATA_BASE_URL

DOODAD_PREFIX = "int_doodad_"


def build_doodads(
    registry: dict[str, Any],
    levels: dict[int, str],
    zones: dict[str, dict[str, Any]],
    used_zones: dict[str, dict[str, Any]],
) -> list[dict[str, Any]]:
    """CI 侧登记了 zone 参数的底图上的可交互采集物，按实体 ID 数值升序。"""
    brief_infos = assert_record(
        registry.get("worldEntityBriefInfos"),
        "WorldEntityRegistry.worldEntityBriefInfos",
    )
    doodads: list[dict[str, Any]] = []
    skipped_maps: set[str] = set()
    out_of_bounds = 0
    for entity_id in sorted(brief_infos, key=lambda key: (len(key), key)):
        entity = assert_record(brief_infos[entity_id], f"世界实体 {entity_id}")
        detail_id = entity.get("detailId")
        if not isinstance(detail_id, str) or not detail_id.startswith(DOODAD_PREFIX):
            continue
        level_id = level_of(levels, entity_id)
        if level_id is None:
            continue
        map_id = map_of_level(level_id)
        if map_id not in ZONE_MAPS:
            skipped_maps.add(map_id)
            continue
        zone = used_zones.get(map_id)
        if zone is None:
            zone = resolve_zone(zones, map_id)
            used_zones[map_id] = zone

        x, z = read_xz(entity.get("position"), f"世界实体 {entity_id}.position")
        try:
            u, v = project_to_pixel(zone, x, z, f"采集物 {entity_id}")
        except TableCfgError as error:
            # 单个点位越界不该让整条流水线停下，记一笔丢掉即可。
            out_of_bounds += 1
            print(f"[{LABEL}] {error}", file=sys.stderr)
            continue
        doodads.append(
            {
                "id": entity_id,
                "detail_id": detail_id,
                "map": map_id,
                "u": u,
                "v": v,
            }
        )

    if skipped_maps:
        print(
            f"[{LABEL}] 以下底图在 CI 侧未登记 zone 参数，"
            f"其采集物未纳入：{sorted(skipped_maps)}",
            file=sys.stderr,
        )
    if out_of_bounds:
        print(f"[{LABEL}] {out_of_bounds} 个采集物投影越界，已跳过", file=sys.stderr)
    return doodads


def build_collect_points_data(
    gameplay_config: dict[str, Any], zones: dict[str, dict[str, Any]]
) -> dict[str, Any]:
    levels = build_level_index(
        assert_record(
            gameplay_config["LevelBasicInfoTable.json"], "LevelBasicInfoTable"
        )
    )
    used_zones: dict[str, dict[str, Any]] = {}
    points = build_doodads(
        assert_record(
            gameplay_config["WorldEntityRegistry.json"], "WorldEntityRegistry"
        ),
        levels,
        zones,
        used_zones,
    )

    if not points:
        raise TableCfgError("WorldEntityRegistry 里一个可交互采集物都没有")
    used_maps = {point["map"] for point in points}

    return {
        "text": {
            "points": "可交互采集物，detailId 以 int_doodad_ 开头",
        },
        "maps": describe_zones({map_id: used_zones[map_id] for map_id in used_maps}),
        "coord": COORD_TEXT,
        "count": len(points),
        "points": points,
    }


def parse_arguments(args: Sequence[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="从 BeyondMemoryPack 和本地 BaseNav 生成采集点数据"
    )
    parser.add_argument(
        "--gameplay-config-dir",
        type=Path,
        default=DEFAULT_GAMEPLAY_CONFIG_DIR,
        help=(
            "BeyondMemoryPack 的 GameplayConfig 目录"
            f"（默认：{DEFAULT_GAMEPLAY_CONFIG_DIR}）"
        ),
    )
    parser.add_argument("--nav", default=None, help="nav 数据的本地路径或 URL")
    parser.add_argument("--output", type=Path, default=OUTPUT_PATH, help="输出文件")
    parser.add_argument("--force", action="store_true", help="强制重写输出文件")
    return parser.parse_args(args)


def main(args: Sequence[str] | None = None) -> int:
    options = parse_arguments(args)
    try:
        gameplay_config = load_json_group(
            GAMEPLAY_CONFIG_NAMES,
            options.gameplay_config_dir,
            GAMEPLAY_CONFIG_BASE_URL,
            "GameplayConfig",
            "--gameplay-config-dir",
        )
        zones = load_nav_zones(options.nav)
        data = build_collect_points_data(gameplay_config, zones)
        if should_skip(options.output, data, options.force):
            print(f"[{LABEL}] 生成结果未变化，跳过写入；可使用 --force 强制重写")
            return 0
        write_dataset(options.output, data)
        print(f"[{LABEL}] 已生成 {data['count']} 个采集点：{options.output}")
    except (
        OSError,
        ValueError,
        struct.error,
        zlib.error,
        urllib.error.URLError,
    ) as error:
        print(f"[{LABEL}] {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
