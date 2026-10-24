"""营地，也就是地图上的传送锚点（``teleport_anchors.json``）：取 ``LevelMapMark`` 中
``templateId == mark_sp_campfire`` 的标记，``detailedData.teleportValidationId``
即传送到该点时用的锚点 ID。任何要「先传送再走路」的任务都吃这份数据，
``collect_points`` 也用它把采集物分圈。

``area_names`` 是营地所在区域的五语名字：营地在有名字的地下层里取层名，否则取同关卡地图上离它最近的
地名标签；关卡没有地名时为空。``levels`` 给出各关卡的五语名字。

只发布游戏世界坐标 x/z：投影参数属于 BaseNav pack，换算由 MaaEnd 侧的投影步骤
（``tools/zmdmap/utils/project.py``）完成，所以这里不做任何底图筛选，全部营地主都收。
"""

from pathlib import Path
from typing import Any

from ..utils.game_data import level_of, map_of_level
from ..utils.log import logger
from ..utils.tables import (
    LevelDescRow,
    LevelMap,
    LevelMark,
    LevelMarkBasic,
    TextRef,
    load_config,
    load_level,
    load_table,
)
from .base import Dataset, DatasetError

CAMPFIRE_TEMPLATE_ID = "mark_sp_campfire"
# 关卡地图配置 staticElements 里地名标签的 type
PLACE_LABEL_TYPE = 2


def area_text_key(level_map: LevelMap, basic: LevelMarkBasic, x: float, z: float) -> str:
    """营地所在区域名的文本 key；关卡没有地名时是空串。"""
    tier_key = level_map.tier_names.get(str(basic.tier_map))
    if tier_key:
        return tier_key

    labels = [element for element in level_map.static_elements.values() if element.type == PLACE_LABEL_TYPE]
    if not labels:
        return ""
    nearest = min(
        labels, key=lambda element: (element.position.x - x) ** 2 + (element.position.z - z) ** 2
    )
    # 任务推进后标签会换字，取最后一段
    return nearest.text_phases[-1].text_id if nearest.text_phases else nearest.text_id


class TeleportAnchors(Dataset):
    """营地（地图上的传送锚点）（``teleport_anchors.json``）。"""

    needs_projection = True
    requires_json_data = True
    projection_containers = ("anchors",)

    @classmethod
    def build(cls, table_cfg_dir: Path, json_data_dir: Path) -> dict[str, Any]:
        """营地（传送锚点）：坐标、所属关卡与底图、区域与关卡的名称。

        只认 ``templateId`` 是 ``mark_sp_campfire`` 的标记，按 ``markInstId`` 的数值升序。区域名优先取
        地下层名（``LevelMapLoadConfig`` 的 ``tierNames`` 对上标记的 ``tierMap``），没有就取同关卡地图上
        离它最近的地名标签，两者都没有时为空。
        """
        level_map_mark = load_config(json_data_dir, "LevelMapMark", dict[str, list[LevelMark]])
        levels = cls.level_index(json_data_dir)
        text_keys = load_table(table_cfg_dir, "TextTable", TextRef)
        level_desc = load_table(table_cfg_dir, "LevelDescTable", LevelDescRow)

        anchors: list[dict[str, Any]] = []
        for group_key in sorted(level_map_mark):
            level_id = level_of(levels, group_key)
            if level_id is None:
                logger.warning(f"LevelMapMark 分组 {group_key} 没有对应关卡，整组跳过")
                continue
            map_id = map_of_level(level_id)

            for mark in level_map_mark[group_key]:
                basic = mark.basic_data
                if basic.template_id != CAMPFIRE_TEMPLATE_ID:
                    continue

                label = f"营地 {basic.mark_inst_id}"
                x, z = basic.pos.x, basic.pos.z

                teleport_id = mark.detailed_data.teleport_validation_id
                if not teleport_id:
                    logger.warning(f"{label} 缺少 teleportValidationId")
                    teleport_id = ""

                area_names: dict[str, str] = {}
                area_key = area_text_key(load_level(json_data_dir, level_id, LevelMap), basic, x, z)
                if area_key:
                    if area_key not in text_keys:
                        raise DatasetError(f"{label} 的区域名 {area_key} 不在 TextTable 里")
                    area_label = f"{label} 的区域名"
                    area_names = cls.plain_names(
                        cls.localized_names(text_keys[area_key], table_cfg_dir, area_label),
                        area_label,
                    )

                anchors.append(
                    {
                        "id": basic.mark_inst_id,
                        "teleport_id": teleport_id,
                        "map": map_id,
                        "level": level_id,
                        "x": x,
                        "z": z,
                        "area_names": area_names,
                    }
                )

        if not anchors:
            raise DatasetError("LevelMapMark 里一个营地都没有")
        anchors.sort(key=lambda item: int(item["id"]))

        level_names: dict[str, Any] = {}
        for level_id in sorted({anchor["level"] for anchor in anchors}):
            entry = level_desc.get(level_id)
            if entry is None:
                raise DatasetError(f"关卡 {level_id} 不在 LevelDescTable 里")
            level_label = f"关卡 {level_id}"
            level_names[level_id] = {
                "names": cls.plain_names(
                    cls.localized_names(entry.show_name, table_cfg_dir, level_label), level_label
                )
            }

        logger.info(f"Built maaend teleport_anchors: {len(anchors)} anchors")
        return {"levels": level_names, "anchors": anchors}
