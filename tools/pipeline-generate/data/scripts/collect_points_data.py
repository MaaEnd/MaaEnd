"""生成 AutoCollect 使用的 collect_points.json。"""

from __future__ import annotations

import argparse
import math
import struct
import sys
import urllib.error
import zlib
from collections.abc import Sequence
from pathlib import Path
from typing import Any

from navzone_utils import (
    COORD_TEXT,
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
from teleport_anchors_data import (
    build_anchors,
    build_level_index,
    level_of,
    map_of_level,
)

LABEL = "CollectPoints"
OUTPUT_PATH = DATA_DIR / "collect_points.json"
DEFAULT_GAMEPLAY_CONFIG_DIR = DEFAULT_JSON_DATA_DIR / "GameplayConfig"
GAMEPLAY_CONFIG_NAMES = (
    "WorldEntityRegistry.json",
    "LevelMapMark.json",
    "LevelBasicInfoTable.json",
)

DATA_BASE_URL = "https://assets.fz.wiki/output_maaend"
GAMEPLAY_CONFIG_BASE_URL: str | None = DATA_BASE_URL

DOODAD_PREFIX = "int_doodad_"
# 采集路线一律从营地传送出发，离所有营地都超过这个距离的采集物不属于任何采集圈。
CAMPFIRE_RADIUS_PX = 120.0
# 当前接入采集任务的地图；覆盖新地图时把它的 map ID 加进来。
COLLECT_MAPS = ("map02",)


def build_doodads(
    registry: dict[str, Any],
    levels: dict[int, str],
    zones: dict[str, dict[str, Any]],
    used_zones: dict[str, dict[str, Any]],
) -> list[dict[str, Any]]:
    """COLLECT_MAPS 上的可交互采集物，按实体 ID 数值升序。"""
    brief_infos = assert_record(
        registry.get("worldEntityBriefInfos"),
        "WorldEntityRegistry.worldEntityBriefInfos",
    )
    doodads: list[dict[str, Any]] = []
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
        if map_id not in COLLECT_MAPS:
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

    if out_of_bounds:
        print(f"[{LABEL}] {out_of_bounds} 个采集物投影越界，已跳过", file=sys.stderr)
    return doodads


def nearest_anchor(point: dict[str, Any], anchors: list[dict[str, Any]]) -> str | None:
    """同图内距离最近且在采集圈内的营地 ID。"""
    best_id: str | None = None
    best_distance = CAMPFIRE_RADIUS_PX
    for anchor in anchors:
        if anchor["map"] != point["map"]:
            continue
        distance = math.hypot(anchor["u"] - point["u"], anchor["v"] - point["v"])
        if distance <= best_distance:
            best_id, best_distance = anchor["id"], distance
    return best_id


def build_collect_points_data(
    gameplay_config: dict[str, Any], zones: dict[str, dict[str, Any]]
) -> dict[str, Any]:
    levels = build_level_index(
        assert_record(
            gameplay_config["LevelBasicInfoTable.json"], "LevelBasicInfoTable"
        )
    )
    anchors, _ = build_anchors(
        assert_record(gameplay_config["LevelMapMark.json"], "LevelMapMark"),
        levels,
        zones,
    )
    used_zones: dict[str, dict[str, Any]] = {}
    doodads = build_doodads(
        assert_record(
            gameplay_config["WorldEntityRegistry.json"], "WorldEntityRegistry"
        ),
        levels,
        zones,
        used_zones,
    )

    points: list[dict[str, Any]] = []
    used_maps: set[str] = set()
    for point in doodads:
        anchor_id = nearest_anchor(point, anchors)
        if anchor_id is None:
            continue
        used_maps.add(point["map"])
        points.append({**point, "anchor_id": anchor_id})
    if not points:
        raise TableCfgError("没有任何采集物落在营地附近，请确认几份输入取自同一版本")

    return {
        "text": {
            "points": (
                "可交互采集物，detailId 以 int_doodad_ 开头；"
                f"只保留距最近同图营地 {CAMPFIRE_RADIUS_PX:.0f} 像素以内的点位"
            ),
            "anchor_id": "该点位所属营地的 ID，对应 teleport_anchors.json 的 anchors[].id",
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
