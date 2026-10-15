"""MaaEnd 精简游戏数据 substep: teleport_anchors.json.

营地，也就是地图上的传送锚点：取 ``LevelMapMark`` 中
``templateId == mark_sp_campfire`` 的标记，``detailedData.teleportValidationId``
即传送到该点时用的锚点 ID。任何要「先传送再走路」的任务都吃这份数据，
``collect_points`` 也用它把采集物分圈。

只发布游戏世界坐标 x/z：投影参数属于 BaseNav pack，换算由 MaaEnd 侧的投影步骤
（``tools/zmdmap/project.py``）完成，所以这里不做任何底图筛选，全部营地主都收。
"""

from pathlib import Path
from typing import Any

from ..utils.game_data import logger, read_gameplay_config
from ..utils.coords import COORD_TEXT, read_xz
from ..utils.levels import build_level_index, level_of, map_of_level

CAMPFIRE_TEMPLATE_ID = "mark_sp_campfire"

class TeleportAnchorError(ValueError):
    """teleport_anchors 数据组装错误。"""

def build_anchors(json_data_dir: Path) -> list[dict[str, Any]]:
    """营地列表（按 markInstId 数值升序）。"""
    level_map_mark = read_gameplay_config(json_data_dir, "LevelMapMark")
    levels = build_level_index(read_gameplay_config(json_data_dir, "LevelBasicInfoTable"))

    anchors: list[dict[str, Any]] = []
    for group_key in sorted(level_map_mark):
        level_id = level_of(levels, group_key)
        if level_id is None:
            logger.warning(f"LevelMapMark 分组 {group_key} 没有对应关卡，整组跳过")
            continue
        map_id = map_of_level(level_id)

        for mark in level_map_mark[group_key]:
            basic = mark.basicData
            if basic.templateId != CAMPFIRE_TEMPLATE_ID:
                continue

            label = f"营地 {basic.markInstId}"
            x, z = read_xz(basic.pos, f"LevelMapMark[{basic.markInstId}].pos")

            detailed = mark.detailedData or {}
            teleport_id = detailed.get("teleportValidationId")
            if not isinstance(teleport_id, str) or not teleport_id:
                logger.warning(f"{label} 缺少 teleportValidationId")
                teleport_id = ""

            anchors.append(
                {
                    "id": basic.markInstId,
                    "teleport_id": teleport_id,
                    "map": map_id,
                    "level": level_id,
                    "x": x,
                    "z": z,
                }
            )

    if not anchors:
        raise TeleportAnchorError("LevelMapMark 里一个营地都没有")
    anchors.sort(key=lambda item: int(item["id"]))
    return anchors

def build_teleport_anchors(table_cfg_dir: Path, json_data_dir: Path) -> dict[str, Any]:
    """Data chain: LevelMapMark 的营地标记 -> 传送锚点；LevelBasicInfoTable 提供
    ID 前缀到关卡的对照。
    """
    anchors = build_anchors(json_data_dir)
    logger.info(f"Built maaend teleport_anchors: {len(anchors)} anchors")
    return {
        "text": {
            "anchors": (
                "营地，取自 LevelMapMark 中 templateId=mark_sp_campfire 的标记；x/z 即它在大世界里的位置"
            ),
            "teleport_id": "传送到该营地时用的锚点 ID，取自标记的 detailedData.teleportValidationId",
            "level": "所属关卡 ID，取自 LevelBasicInfoTable；ID 除以 10^8 即关卡 idNum",
        },
        "coord": COORD_TEXT,
        "count": len(anchors),
        "anchors": anchors,
    }
