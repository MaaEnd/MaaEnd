"""物品 mini table（``item_mini_table.json``）：背包物品 → 分类的轻量映射。

只收「会出现在背包里的物品」，再排掉也在仓库页签里出现的那些，例外是两个仓库页签按页签名收回来：

- 珍贵物品（``ValuableDepot.type == "CommercialItem"``）：抽卡券、理智/食物补给这类真正的获得物；
- 培养素材（``SpecialItem``）：种子与养成材料。

每个条目带一组固定字段（``storageKind`` / ``categoryType`` 决定 ``category``），外加：

- ``names{五语}``：物品名，直接给五语文本，消费端不必再下 ``lang_*.json``（每份约 950 KB）还原名字。
- ``nameId``：名字的文本 id。消费端按「名字 + 图标」去重时用得上——拿哈希去重时用的就是它，而哈希就是
  文本 id 的函数，所以按文本 id 去重与上游结果一致；只按文本去重会把「同一句文案挂在两个文本 id 上」
  的条目误并。
- ``regionRestricted``：仅当物品由「只在某些地区存在」的工厂建筑产出时才写 ``true``。

配方类（配方/配方升级/装备配方）是制造说明而不是可用物品，名字 id 为 0 的（如 ``item_port_*`` 工厂摆件）
没有名字，两类都不收。
"""

from __future__ import annotations

from pathlib import Path
from typing import Any

from ..utils.game_data import as_text, logger, read_table_cfg, ref_text, texts_of, text_id_of
from .base import Dataset

# 配方 / 配方升级 / 装备配方：制造说明，不是可用物品。
_FORMULA_TYPES = frozenset({"EquipFormula", "FormulaUnlock", "FormulaLevelUp"})

# 仓库页签里重新收进来的两个（珍贵物品 / 培养素材）：category 用页签名而不是背包页签名。
_REINCLUDED_DEPOT_TYPES = frozenset({"CommercialItem", "SpecialItem"})


class ItemMiniTable(Dataset):
    """图标识别的物品表，带多语言名字（``item_mini_table.json``）。"""

    @classmethod
    def build(cls, table_cfg_dir: Path, json_data_dir: Path) -> dict[str, dict[str, Any]]:
        """背包物品与它们所在的页签、图标、稀有度、排序与五语名字。"""
        items = read_table_cfg(table_cfg_dir, "ItemTable")
        showing_types = read_table_cfg(table_cfg_dir, "ItemShowingTypeTable")
        depots = read_table_cfg(table_cfg_dir, "ValuableDepot")

        # 背包页签（showingType -> 中文名）。
        showing_type_names = {
            as_text(row.get("type")): ref_text(table_cfg_dir, row.get("name"))
            for row in showing_types.values()
        }
        showing_type_names = {key: name for key, name in showing_type_names.items() if name}

        # 仓库页签：除了重新收进来的两个，其余（武器/装备/任务物品…）一律不收。
        depot_names: dict[str, str] = {}
        non_hidden_depot_types: set[str] = set()
        for depot in depots.values():
            if depot.get("isHidden"):
                continue
            depot_type = as_text(depot.get("type"))
            non_hidden_depot_types.add(depot_type)
            depot_names[depot_type] = ref_text(table_cfg_dir, depot.get("name")) or depot_type
        excluded_depot_types = non_hidden_depot_types - _REINCLUDED_DEPOT_TYPES

        fluid_type_by_item, fluid_by_full_container, fluid_containers = _fluid_maps(table_cfg_dir)
        region_restricted_items = _region_restricted_items(table_cfg_dir)

        output: dict[str, dict[str, Any]] = {}
        for item_id, item in items.items():
            category = showing_type_names.get(as_text(item.get("showingType")))
            valuable_tab = as_text(item.get("valuableTabType"))
            if valuable_tab in _REINCLUDED_DEPOT_TYPES:
                # 仓库页签收进来的条目：分类用页签名，不是背包页签名。
                category = depot_names.get(valuable_tab, category)
            if category is None or valuable_tab in excluded_depot_types:
                # 没有背包页签（或页签未登记），或者同时出现在仓库里。
                continue
            if as_text(item.get("type")) in _FORMULA_TYPES:
                continue
            name_ref = item.get("name")
            if text_id_of(name_ref) == 0:
                # 没有名字（如工厂摆件）不是真正的背包物品。
                continue

            from_depot = valuable_tab in _REINCLUDED_DEPOT_TYPES
            containers = fluid_containers.get(item_id)
            output[item_id] = {
                "name": ref_text(table_cfg_dir, name_ref),
                "nameId": text_id_of(name_ref),
                "names": texts_of(table_cfg_dir, text_id_of(name_ref)),
                "category": category,
                "storageKind": "ValuableDepot" if from_depot else "Normal",
                "categoryType": valuable_tab if from_depot else as_text(item.get("showingType")),
                "iconId": as_text(item.get("iconId")),
                "rarity": item.get("rarity"),
                "sortId1": item.get("sortId1"),
                "sortId2": item.get("sortId2"),
                "fluidType": fluid_type_by_item.get(item_id),
                # 纯流体是自己，满容器是里面装的流体。
                "fluid": item_id if containers is not None else fluid_by_full_container.get(item_id),
                "fullContainers": containers["full"] if containers is not None else [],
                "emptyContainers": containers["empty"] if containers is not None else [],
            }
            if item_id in region_restricted_items:
                output[item_id]["regionRestricted"] = True

        logger.info(f"物品 mini table {len(output)} 条")
        return output


def _fluid_maps(
    table_cfg_dir: Path,
) -> tuple[dict[str, str], dict[str, str], dict[str, dict[str, list[str]]]]:
    """气体的罐与液体的瓶：物品 -> 流体种类 / 满容器 -> 里面的流体 / 流体 -> 容器名单。"""
    fluid_type_by_item: dict[str, str] = {}
    fluid_by_full_container: dict[str, str] = {}
    fluid_containers: dict[str, dict[str, list[str]]] = {}

    families = (
        (read_table_cfg(table_cfg_dir, "GasTable"), "gas", "fullGasJarItems", "emptyGasJarItems"),
        (read_table_cfg(table_cfg_dir, "LiquidTable"), "liquid", "fullBottleItems", "emptyBottleItems"),
    )
    for table, fluid_type, full_field, empty_field in families:
        for fluid_id, fluid in table.items():
            full = [as_text(container) for container in fluid.get(full_field) or []]
            empty = [as_text(container) for container in fluid.get(empty_field) or []]
            fluid_type_by_item[fluid_id] = fluid_type
            for container_id in full:
                fluid_type_by_item[container_id] = fluid_type
                fluid_by_full_container[container_id] = fluid_id
            for container_id in empty:
                fluid_type_by_item[container_id] = fluid_type
            fluid_containers[fluid_id] = {"full": full, "empty": empty}
    return fluid_type_by_item, fluid_by_full_container, fluid_containers


def _region_restricted_items(table_cfg_dir: Path) -> set[str]:
    """只在某些地区存在的工厂建筑所产出的物品（``recommendDomains`` 非空）。"""
    buildings = read_table_cfg(table_cfg_dir, "FactoryBuildingTable")
    restricted_buildings = {
        building_id
        for building_id, building in buildings.items()
        if building.get("recommendDomains")
    }
    reverse = read_table_cfg(table_cfg_dir, "FactoryBuildingItemReverseTable")

    item_ids: set[str] = set()
    for building_id, row in reverse.items():
        if building_id not in restricted_buildings:
            continue
        # 这个字段有的版本是字符串、有的是数组。
        produced = row.get("itemId")
        item_ids.update([as_text(produced)] if isinstance(produced, str) else produced or [])
    return item_ids
