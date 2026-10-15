"""MaaEnd 精简游戏数据 substep: collect_points.json.

AutoCollect 采集路线的数据源：``WorldEntityRegistry`` 中 ``detailId`` 以
``int_doodad_`` 开头的可交互采集物，全量输出。

只发布游戏世界坐标 x/z：投影参数属于 BaseNav pack，换算由 MaaEnd 侧的投影步骤
（``tools/zmdmap/project.py``）完成，所以这里不做任何底图筛选。
"""

from pathlib import Path
from typing import Any

from ..utils.game_data import Record, logger, read_gameplay_config
from ..utils.coords import COORD_TEXT, read_xz
from ..utils.levels import build_level_index, level_of, map_of_level

DOODAD_PREFIX = "int_doodad_"

class CollectPointError(ValueError):
    """collect_points 数据组装错误。"""

def _build_doodads(
    json_data_dir: Path,
    registry: Record,
) -> list[dict[str, Any]]:
    """全部可交互采集物，按实体 ID 数值升序。"""
    brief_infos = registry.worldEntityBriefInfos
    if not brief_infos:
        raise CollectPointError("WorldEntityRegistry 缺少 worldEntityBriefInfos")
    levels = build_level_index(read_gameplay_config(json_data_dir, "LevelBasicInfoTable"))

    doodads: list[dict[str, Any]] = []
    for entity_id in sorted(brief_infos, key=lambda key: (len(key), key)):
        entity = brief_infos[entity_id]
        if not entity.detailId.startswith(DOODAD_PREFIX):
            continue
        level_id = level_of(levels, entity_id)
        if level_id is None:
            continue

        x, z = read_xz(entity.position, f"世界实体 {entity_id}.position")
        doodads.append(
            {
                "id": entity_id,
                "detail_id": entity.detailId,
                "map": map_of_level(level_id),
                "x": x,
                "z": z,
            }
        )
    return doodads

def build_collect_points(table_cfg_dir: Path, json_data_dir: Path) -> dict[str, Any]:
    """Data chain: WorldEntityRegistry -> 采集物，LevelBasicInfoTable 提供 ID 前缀
    到关卡的对照。
    """
    points = _build_doodads(json_data_dir, read_gameplay_config(json_data_dir, "WorldEntityRegistry"))
    if not points:
        raise CollectPointError("WorldEntityRegistry 里一个可交互采集物都没有")

    logger.info(f"Built maaend collect_points: {len(points)} points")
    return {
        "text": {
            "points": "可交互采集物，detailId 以 int_doodad_ 开头",
        },
        "coord": COORD_TEXT,
        "count": len(points),
        "points": points,
    }
