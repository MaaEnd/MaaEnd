"""AutoCollect 采集路线的数据源（``collect_points.json``）。

``WorldEntityRegistry`` 中 ``detailId`` 以 ``int_doodad_`` 开头的可交互采集物，全量输出。

``types`` 只收能拾取或挖掘的采集物类型：``names`` 是采集物的五语名字（没有就用产物名），``action`` 是
MapNavigator 的采集动作（拾取 ``COLLECT`` / 挖掘 ``DIG``），``item`` 是主产物，``icon`` 是它在
``assets/resource/image/UI/Item/`` 下的图标名；``item_type`` / ``showing_type`` / ``rarity`` 是主产物
的物品类型、展示分类与稀有度。基建矿点等不进 ``types``。

只发布游戏世界坐标 x/z（另有世界高度 y）：投影参数属于 BaseNav pack，换算由 MaaEnd 侧的投影步骤
（``tools/zmdmap/utils/project.py``）完成，所以这里不做任何底图筛选。
"""

from pathlib import Path
from typing import Any

from ..utils.game_data import (
    level_of,
    load_json_dict,
    logger,
    map_of_level,
    read_gameplay_config,
    read_table_cfg,
    read_xyz,
)
from .base import Dataset, DatasetError

DOODAD_PREFIX = "int_doodad_"
DOODAD_ID_KEY = "doodad_id"


def doodad_ids_in(node: Any, found: dict[str, str]) -> None:
    """交互数据里每个 ``int_doodad_*`` 模板 -> 它的 ``doodad_id`` 参数；模板表是对象或键值对数组都认。"""
    if isinstance(node, dict):
        for key, value in node.items():
            if key.startswith(DOODAD_PREFIX) and isinstance(value, dict):
                record_doodad_id(key, value, found)
            doodad_ids_in(value, found)
    elif isinstance(node, list):
        if (
            len(node) == 2
            and isinstance(node[0], str)
            and node[0].startswith(DOODAD_PREFIX)
            and isinstance(node[1], dict)
        ):
            record_doodad_id(node[0], node[1], found)
        for child in node:
            doodad_ids_in(child, found)


def record_doodad_id(template_id: str, template: dict[str, Any], found: dict[str, str]) -> None:
    for prop in template.get("diffProperties") or []:
        if isinstance(prop, dict) and prop.get("key") == DOODAD_ID_KEY:
            values = (prop.get("value") or {}).get("valueArray") or []
            if values and values[0].get("valueString"):
                found[template_id] = values[0]["valueString"]


def primary_item(doodad: Any, reward_table: Any) -> str:
    """主产物：通用配置直接写了就用它，否则取拾取或破坏掉落里的第一个物品。"""
    if doodad.itemId:
        return doodad.itemId
    for reward_id in doodad.pickableRewardId or doodad.breakedDropRewardId:
        reward = reward_table.get(reward_id)
        if reward is None:
            continue
        bundles = reward.itemBundles or reward.probItemBundles
        if bundles:
            return bundles[0].id
    return ""


class CollectPoints(Dataset):
    """AutoCollect 采集路线的采集物（``collect_points.json``）。"""

    needs_projection = True
    projection_containers = ("points",)

    @classmethod
    def build(cls, table_cfg_dir: Path, json_data_dir: Path) -> dict[str, Any]:
        """全部可交互采集物都收，按实体 ID 数值升序。"""
        brief_infos = read_gameplay_config(json_data_dir, "WorldEntityRegistry").worldEntityBriefInfos
        if not brief_infos:
            raise DatasetError("WorldEntityRegistry 缺少 worldEntityBriefInfos")
        levels = cls.level_index(json_data_dir)

        points: list[dict[str, Any]] = []
        for entity_id in sorted(brief_infos, key=lambda key: (len(key), key)):
            entity = brief_infos[entity_id]
            if not entity.detailId.startswith(DOODAD_PREFIX):
                continue
            level_id = level_of(levels, entity_id)
            if level_id is None:
                continue

            x, y, z = read_xyz(entity.position, f"世界实体 {entity_id}.position")
            points.append(
                {
                    "id": entity_id,
                    "detail_id": entity.detailId,
                    "map": map_of_level(level_id),
                    "x": x,
                    "y": round(y, 3),
                    "z": z,
                }
            )
        if not points:
            raise DatasetError("WorldEntityRegistry 里一个可交互采集物都没有")

        types = cls.build_types(table_cfg_dir, json_data_dir, {point["detail_id"] for point in points})
        logger.info(f"Built maaend collect_points: {len(points)} points, {len(types)} pickable types")
        return {"types": types, "points": points}

    @classmethod
    def build_types(
        cls, table_cfg_dir: Path, json_data_dir: Path, detail_ids: set[str]
    ) -> dict[str, Any]:
        interactive_dir = json_data_dir / "Interactive" / "InteractiveData"
        doodad_ids: dict[str, str] = {}
        for path in sorted(interactive_dir.glob(f"data_{DOODAD_PREFIX}*.json")):
            doodad_ids_in(load_json_dict(path), doodad_ids)
        if not doodad_ids:
            raise DatasetError(f"{interactive_dir} 里没有找到采集物的 doodad_id")

        doodad_table = read_table_cfg(table_cfg_dir, "DoodadGeneralTable")
        reward_table = read_table_cfg(table_cfg_dir, "RewardTable")
        item_table = read_table_cfg(table_cfg_dir, "ItemTable")

        types: dict[str, Any] = {}
        for detail_id in sorted(detail_ids):
            doodad = doodad_table.get(doodad_ids.get(detail_id, ""))
            if doodad is None or not (doodad.isPickable or doodad.isBreakable):
                continue
            item_id = primary_item(doodad, reward_table)
            item = item_table.get(item_id)
            if item is None:
                raise DatasetError(f"采集物 {detail_id} 的产物 {item_id!r} 不在 ItemTable 里")

            label = f"采集物 {detail_id}"
            item_label = f"物品 {item_id}"
            item_names = cls.plain_names(
                cls.localized_names(item.name, table_cfg_dir, item_label), item_label
            )
            names = item_names
            if doodad.label.id:
                names = cls.plain_names(
                    cls.localized_names(doodad.label, table_cfg_dir, label), label
                )
            types[detail_id] = {
                "names": names,
                "action": "COLLECT" if doodad.isPickable else "DIG",
                "item": item_id,
                "item_names": item_names,
                "icon": item.iconId or item_id,
                "item_type": item.type,
                "showing_type": item.showingType,
                "rarity": item.rarity,
            }
        return types
