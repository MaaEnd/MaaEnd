"""生成传送锚点数据 teleport_anchors.json。"""

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
    assert_list,
    assert_record,
    load_json_group,
    should_skip,
    sorted_entries,
    write_dataset,
)

LABEL = "TeleportAnchors"
OUTPUT_PATH = DATA_DIR / "teleport_anchors.json"
DEFAULT_GAMEPLAY_CONFIG_DIR = DEFAULT_JSON_DATA_DIR / "GameplayConfig"
GAMEPLAY_CONFIG_NAMES = ("LevelMapMark.json", "LevelBasicInfoTable.json")

DATA_BASE_URL = "https://assets.fz.wiki/output_maaend"
GAMEPLAY_CONFIG_BASE_URL: str | None = DATA_BASE_URL

CAMPFIRE_TEMPLATE_ID = "mark_sp_campfire"
ID_LEVEL_FACTOR = 10**8
# indie_dgXXX 各自一张底图，其余关卡按 mapXX / base01 / dung01 归图。
INDIE_LEVEL_HEAD = "indie"


def map_of_level(level_id: str) -> str:
    """关卡 ID -> 底图 key。"""
    head = level_id.split("_")[0]
    return level_id if head == INDIE_LEVEL_HEAD else head


def build_level_index(level_basic_info: dict[str, Any]) -> dict[int, str]:
    """idNum -> levelId；地图标记分组与世界实体 ID 除以 10^8 就是 idNum。"""
    levels: dict[int, str] = {}
    for level_id, entry_value in sorted_entries(level_basic_info):
        entry = assert_record(entry_value, f"LevelBasicInfoTable[{level_id}]")
        id_num = entry.get("idNum")
        if not isinstance(id_num, int) or isinstance(id_num, bool):
            raise TableCfgError(f"关卡 {level_id} 的 idNum {id_num!r} 不是整数")
        if id_num in levels:
            raise TableCfgError(f"idNum {id_num} 同时属于 {levels[id_num]} 和 {level_id}")
        levels[id_num] = level_id
    if not levels:
        raise TableCfgError("LevelBasicInfoTable 里一个关卡都没有")
    return levels


def level_of(levels: dict[int, str], raw_id: str) -> str | None:
    """按 ID 前缀定位关卡；非数字 ID 与未登记的前缀都返回 None。"""
    if not raw_id.isdigit():
        return None
    return levels.get(int(raw_id) // ID_LEVEL_FACTOR)


def build_anchors(
    level_map_mark: dict[str, Any],
    levels: dict[int, str],
    zones: dict[str, dict[str, Any]],
) -> tuple[list[dict[str, Any]], dict[str, dict[str, Any]]]:
    """营地列表（按 markInstId 数值升序）与用到的 zone。"""
    anchors: list[dict[str, Any]] = []
    used_zones: dict[str, dict[str, Any]] = {}
    skipped_maps: set[str] = set()
    for group_key, marks in sorted_entries(level_map_mark):
        level_id = level_of(levels, group_key)
        if level_id is None:
            print(
                f"[{LABEL}] LevelMapMark 分组 {group_key} 没有对应关卡，整组跳过",
                file=sys.stderr,
            )
            continue
        map_id = map_of_level(level_id)

        for offset, mark_value in enumerate(
            assert_list(marks, f"LevelMapMark[{group_key}]")
        ):
            mark = assert_record(mark_value, f"LevelMapMark[{group_key}][{offset}]")
            basic = assert_record(
                mark.get("basicData"), f"LevelMapMark[{group_key}][{offset}].basicData"
            )
            if basic.get("templateId") != CAMPFIRE_TEMPLATE_ID:
                continue
            if map_id not in ZONE_MAPS:
                skipped_maps.add(map_id)
                continue

            mark_id = basic.get("markInstId")
            if not isinstance(mark_id, str) or not mark_id.isdigit():
                raise TableCfgError(
                    f"LevelMapMark[{group_key}][{offset}] 的 markInstId "
                    f"{mark_id!r} 不是数字 ID"
                )
            label = f"营地 {mark_id}"
            zone = used_zones.get(map_id)
            if zone is None:
                zone = resolve_zone(zones, map_id)
                used_zones[map_id] = zone
            x, z = read_xz(basic.get("pos"), f"{label}.pos")
            u, v = project_to_pixel(zone, x, z, label)

            detailed = assert_record(
                mark.get("detailedData") or {}, f"{label}.detailedData"
            )
            teleport_id = detailed.get("teleportValidationId")
            if not isinstance(teleport_id, str) or not teleport_id:
                print(f"[{LABEL}] {label} 缺少 teleportValidationId", file=sys.stderr)
                teleport_id = ""

            anchors.append(
                {
                    "id": mark_id,
                    "teleport_id": teleport_id,
                    "map": map_id,
                    "level": level_id,
                    "u": u,
                    "v": v,
                }
            )

    if skipped_maps:
        print(
            f"[{LABEL}] 以下底图在 CI 侧未登记 zone 参数，"
            f"其营地未纳入传送锚点：{sorted(skipped_maps)}",
            file=sys.stderr,
        )
    if not anchors:
        raise TableCfgError("LevelMapMark 里一个营地都没有")
    anchors.sort(key=lambda item: int(item["id"]))
    return anchors, used_zones


def build_teleport_anchors_data(
    gameplay_config: dict[str, Any], zones: dict[str, dict[str, Any]]
) -> dict[str, Any]:
    levels = build_level_index(
        assert_record(
            gameplay_config["LevelBasicInfoTable.json"], "LevelBasicInfoTable"
        )
    )
    anchors, used_zones = build_anchors(
        assert_record(gameplay_config["LevelMapMark.json"], "LevelMapMark"),
        levels,
        zones,
    )

    return {
        "text": {
            "anchors": (
                "营地，取自 LevelMapMark 中 templateId=mark_sp_campfire 的标记；u/v 即它在底图上的位置"
            ),
            "teleport_id": "传送到该营地时用的锚点 ID，取自标记的 detailedData.teleportValidationId",
            "level": "所属关卡 ID，取自 LevelBasicInfoTable；ID 除以 10^8 即关卡 idNum",
        },
        "maps": describe_zones(used_zones),
        "coord": COORD_TEXT,
        "count": len(anchors),
        "anchors": anchors,
    }


def parse_arguments(args: Sequence[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="从 BeyondMemoryPack 和本地 BaseNav 生成传送锚点数据"
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
        data = build_teleport_anchors_data(gameplay_config, zones)
        if should_skip(options.output, data, options.force):
            print(f"[{LABEL}] 生成结果未变化，跳过写入；可使用 --force 强制重写")
            return 0
        write_dataset(options.output, data)
        print(f"[{LABEL}] 已生成 {data['count']} 个传送锚点：{options.output}")
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
