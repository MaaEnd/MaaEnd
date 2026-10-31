"""AutoCollect 采集路线的数据源（``collect_points.json``）。

``points`` 是 ``WorldEntityRegistry`` 里 ``detailId`` 以 ``int_doodad_`` 开头、且能按 ID 前缀定位到关卡的
可交互采集物，按实体 ID 的数值升序（定位不到关卡的那几条不收，因为不知道挂哪张底图）。

``types`` 是按 ``detailId`` 汇总的类型表，只收能拾取或挖掘的（基建矿点等不进）：

- ``names``：采集物的五语名字；它自己有名字就用它，没有就用产物名；
- ``action``：MapNavigator 的采集动作，能拾取是 ``COLLECT``，只能打碎取 ``DIG``；
- ``item`` / ``item_names`` / ``icon``：主产物 id、主产物五语名字、主产物在
  ``assets/resource/image/UI/Item/`` 下的图标名（没有图标 id 时退回物品 id）；
- ``item_type`` / ``showing_type`` / ``rarity``：主产物的物品类型、背包页签与稀有度。

只发布游戏世界坐标（``x`` / ``y`` / ``z``，其中 ``y`` 是世界高度）：投影参数属于 BaseNav pack，换算由
MaaEnd 侧的入库步骤（``tools/zmdmap/utils/project.py``）完成，所以这里不做任何底图筛选。
"""

from typing import Any

from ..utils.game_data import level_of, map_of_level, read_xyz
from ..utils.inputs import JsonData, TableCfg
from ..utils.log import logger
from ..utils.tables import (
    ComponentProperty,
    DoodadRow,
    ItemRow,
    RewardRow,
    WorldEntityRegistry,
)
from .base import Dataset, DatasetError

DOODAD_PREFIX = "int_doodad_"
DOODAD_ID_KEY = "doodad_id"


def template_doodad_id(props: list[ComponentProperty]) -> str:
    """一组模板参数里的 ``doodad_id``；没有就是空串。"""
    for prop in props:
        if prop.key == DOODAD_ID_KEY and prop.value.value_array:
            return prop.value.value_array[0].value_string or ""
    return ""


def primary_item(doodad: DoodadRow, reward_table: dict[str, RewardRow]) -> str:
    """主产物：通用配置直接写了就用它，否则取拾取或破坏掉落里的第一个物品。"""
    if doodad.item_id:
        return doodad.item_id
    for reward_id in doodad.pickable_reward_id or doodad.breaked_drop_reward_id:
        reward = reward_table.get(reward_id)
        if reward is None:
            continue
        bundles = reward.item_bundles or reward.prob_item_bundles
        if bundles:
            return bundles[0].id
    return ""


class CollectPoints(Dataset):
    """AutoCollect 采集路线的采集物（``collect_points.json``）。"""

    needs_projection = True
    projection_containers = ("points",)

    @classmethod
    def build(cls, table_cfg: TableCfg, json_data: JsonData) -> dict[str, Any]:
        """全部可交互采集物都收，按实体 ID 数值升序。"""
        brief_infos = json_data.config("WorldEntityRegistry", WorldEntityRegistry).world_entity_brief_infos
        if not brief_infos:
            raise DatasetError("WorldEntityRegistry 缺少 worldEntityBriefInfos")
        levels = json_data.level_index()

        points: list[dict[str, Any]] = []
        for entity_id in sorted(brief_infos, key=lambda key: (len(key), key)):
            entity = brief_infos[entity_id]
            detail_id = entity.detail_id
            if not detail_id or not detail_id.startswith(DOODAD_PREFIX):
                continue
            level_id = level_of(levels, entity_id)
            if level_id is None:
                continue

            x, y, z = read_xyz(entity.position)
            points.append(
                {
                    "id": entity_id,
                    "detail_id": detail_id,
                    "map": map_of_level(level_id),
                    "x": x,
                    "y": round(y, 3),
                    "z": z,
                }
            )
        if not points:
            raise DatasetError("WorldEntityRegistry 里一个可交互采集物都没有")

        types = cls.build_types(table_cfg, json_data, {point["detail_id"] for point in points})
        logger.info(f"Built maaend collect_points: {len(points)} points, {len(types)} pickable types")
        return {"types": types, "points": points}

    @classmethod
    def build_types(cls, table_cfg: TableCfg, json_data: JsonData, detail_ids: set[str]) -> dict[str, Any]:
        doodad_ids: dict[str, str] = {}
        for template in json_data.interactive_templates(DOODAD_PREFIX):
            for template_id, variant in template.template_variant.items():
                doodad_id = template_doodad_id(variant.diff_properties)
                if doodad_id:
                    doodad_ids[template_id] = doodad_id
        if not doodad_ids:
            raise DatasetError("JsonData 的 InteractiveData 里没有找到采集物的 doodad_id")

        doodad_table = table_cfg.table("DoodadGeneralTable", DoodadRow)
        reward_table = table_cfg.table("RewardTable", RewardRow)
        item_table = table_cfg.table("ItemTable", ItemRow)

        types: dict[str, Any] = {}
        for detail_id in sorted(detail_ids):
            doodad = doodad_table.get(doodad_ids.get(detail_id, ""))
            if doodad is None or not (doodad.is_pickable or doodad.is_breakable):
                continue
            item_id = primary_item(doodad, reward_table)
            item = item_table.get(item_id)
            if item is None:
                raise DatasetError(f"采集物 {detail_id} 的产物 {item_id!r} 不在 ItemTable 里")

            label = f"采集物 {detail_id}"
            item_label = f"物品 {item_id}"
            item_names = cls.plain_names(cls.localized_names(item.name, table_cfg, item_label), item_label)
            names = item_names
            if doodad.label.id:
                names = cls.plain_names(cls.localized_names(doodad.label, table_cfg, label), label)
            types[detail_id] = {
                "names": names,
                "action": "COLLECT" if doodad.is_pickable else "DIG",
                "item": item_id,
                "item_names": item_names,
                "icon": item.icon_id or item_id,
                "item_type": item.type,
                "showing_type": item.showing_type,
                "rarity": item.rarity,
            }
        return types
