"""物品 mini table（``item_mini_table.json``）：背包和餐厅物品 → 分类的轻量映射。

以「会出现在背包里的物品」为基础，排掉其他仓库页签物品，再补入指定仓库页签及餐厅表中的菜品 / 饮品：

- 珍贵物品（``ValuableDepot.type == "CommercialItem"``）：抽卡券、理智/食物补给这类真正的获得物；
- 培养素材（``SpecialItem``）：种子与养成材料；
- 客栈菜品与未归入背包分类的饮品（``InnDishTable`` / ``InnDrinkTable``）。

配方类（配方/配方升级/装备配方）是制造说明而不是可用物品，名字 id 为 0 的（如 ``item_port_*`` 工厂摆件）
没有名字，两类都不收。每个条目带这些字段：

- ``name`` / ``nameId`` / ``names{五语}``：中文名、名字的文本 id、五语名字表。直接给五语，消费端不必再下
  ``lang_*.json``（每份约 950 KB）还原名字。按「名字 + 图标」去重时用 ``nameId`` —— 上游那套去重哈希就是
  文本 id 的函数，只按名字去重会把「同一句文案挂在两个文本 id 上」的条目误并。
- ``category`` / ``storageKind`` / ``categoryType``：展示用的分类名，这条来自背包（``Normal``）、仓库页签
  （``ValuableDepot``）还是餐厅（``Restaurant``），以及对应的分类 key。
- ``iconId`` / ``rarity`` / ``sortId1`` / ``sortId2``：图标名、稀有度、页签内排序与次级排序。
- ``fluidType`` / ``fluid`` / ``fullContainers`` / ``emptyContainers``：这条是气体还是液体、满容器里装的是
  哪种流体、这种流体的满/空容器物品 id；与流体无关的条目这几项是 ``null`` 或空数组。
- ``regionRestricted``：仅当物品由「只在某些地区存在」的工厂建筑产出时才写 ``true``。
"""

from typing import Any

from ..utils.game_data import text_id_of
from ..utils.inputs import TableCfg
from ..utils.log import logger
from ..utils.tables import (
    FactoryBuildingItemReverseRow,
    FactoryBuildingRow,
    GasRow,
    InnItemRow,
    ItemRow,
    ItemShowingTypeRow,
    LiquidRow,
    ValuableDepotRow,
)
from .base import Dataset, DatasetError

# 配方 / 配方升级 / 装备配方：制造说明，不是可用物品。
_FORMULA_TYPES = frozenset({"EquipFormula", "FormulaUnlock", "FormulaLevelUp"})

# 仓库页签里重新收进来的两个（珍贵物品 / 培养素材）：category 用页签名而不是背包页签名。
_REINCLUDED_DEPOT_TYPES = frozenset({"CommercialItem", "SpecialItem"})


class ItemMiniTable(Dataset):
    """图标识别的物品表，带多语言名字（``item_mini_table.json``）。"""

    @classmethod
    def build(cls, table_cfg: TableCfg) -> dict[str, dict[str, Any]]:
        """背包及餐厅物品与分类、图标、稀有度、排序和五语名字。"""
        items = table_cfg.table("ItemTable", ItemRow)
        showing_types = table_cfg.table("ItemShowingTypeTable", ItemShowingTypeRow)
        depots = table_cfg.table("ValuableDepot", ValuableDepotRow)

        # 背包页签（showingType -> 中文名）。
        showing_type_names = {row.type: table_cfg.ref_text(row.name) for row in showing_types.values()}
        showing_type_names = {key: name for key, name in showing_type_names.items() if name}

        # 仓库页签：除了重新收进来的两个，其余（武器/装备/任务物品…）一律不收。
        depot_names: dict[str, str] = {}
        non_hidden_depot_types: set[str] = set()
        for depot in depots.values():
            if depot.is_hidden:
                continue
            non_hidden_depot_types.add(depot.type)
            depot_names[depot.type] = table_cfg.ref_text(depot.name) or depot.type
        excluded_depot_types = non_hidden_depot_types - _REINCLUDED_DEPOT_TYPES

        fluid_type_by_item, fluid_by_full_container, fluid_containers = _fluid_maps(table_cfg)
        region_restricted_items = _region_restricted_items(table_cfg)

        output: dict[str, dict[str, Any]] = {}
        for item_id, item in items.items():
            category = showing_type_names.get(item.showing_type)
            valuable_tab = item.valuable_tab_type
            if valuable_tab in _REINCLUDED_DEPOT_TYPES:
                # 仓库页签收进来的条目：分类用页签名，不是背包页签名。
                category = depot_names.get(valuable_tab, category)
            if category is None or valuable_tab in excluded_depot_types:
                # 没有背包页签（或页签未登记），或者同时出现在仓库里。
                continue
            if item.type in _FORMULA_TYPES:
                continue
            name_ref = item.name
            if text_id_of(name_ref) == 0:
                # 没有名字（如工厂摆件）不是真正的背包物品。
                continue

            from_depot = valuable_tab in _REINCLUDED_DEPOT_TYPES
            containers = fluid_containers.get(item_id)
            output[item_id] = {
                "name": table_cfg.ref_text(name_ref),
                "nameId": text_id_of(name_ref),
                "names": table_cfg.texts(text_id_of(name_ref)),
                "category": category,
                "storageKind": "ValuableDepot" if from_depot else "Normal",
                "categoryType": valuable_tab if from_depot else item.showing_type,
                "iconId": item.icon_id,
                "rarity": item.rarity,
                "sortId1": item.sort_id1,
                "sortId2": item.sort_id2,
                "fluidType": fluid_type_by_item.get(item_id),
                # 纯流体是自己，满容器是里面装的流体。
                "fluid": item_id if containers is not None else fluid_by_full_container.get(item_id),
                "fullContainers": containers["full"] if containers is not None else [],
                "emptyContainers": containers["empty"] if containers is not None else [],
            }
            if item_id in region_restricted_items:
                output[item_id]["regionRestricted"] = True

        output.update(_restaurant_items(table_cfg, items, output))
        logger.info(f"物品 mini table {len(output)} 条")
        return output


def _restaurant_items(
    table_cfg: TableCfg,
    items: dict[str, ItemRow],
    normal_items: dict[str, dict[str, Any]],
) -> dict[str, dict[str, Any]]:
    """补入菜单物品；普通 ``Normal:Usable`` 饮品继续沿用原分类。"""
    dishes = table_cfg.table("InnDishTable", InnItemRow)
    drinks = table_cfg.table("InnDrinkTable", InnItemRow)
    item_ids = dishes.keys() | drinks.keys()
    missing_ids = item_ids - items.keys()
    if missing_ids:
        raise DatasetError(f"客栈菜品或饮品在 ItemTable 中缺失：{', '.join(sorted(missing_ids))}")

    records: dict[str, dict[str, Any]] = {}
    for item_id in sorted(item_ids):
        item = items[item_id]
        row = dishes.get(item_id) or drinks[item_id]
        if row.id != item_id:
            raise DatasetError(f"客栈物品表 ID 不匹配：{item_id} != {row.id}")
        category_type = "Dish" if item_id in dishes else "Drink"
        if category_type == "Drink" and item_id in normal_items:
            normal_item = normal_items[item_id]
            if normal_item["storageKind"] != "Normal" or normal_item["categoryType"] != "Usable":
                raise DatasetError(f"客栈饮品 {item_id} 的普通分类不是 Normal:Usable")
            continue

        name_id = text_id_of(item.name)
        if name_id == 0:
            raise DatasetError(f"客栈物品 {item_id} 缺少名称文本 ID")
        records[item_id] = {
            "name": table_cfg.ref_text(item.name),
            "nameId": name_id,
            "names": table_cfg.ref_texts(item.name),
            "category": "客栈菜品" if category_type == "Dish" else "客栈饮品",
            "storageKind": "Restaurant",
            "categoryType": category_type,
            "iconId": item.icon_id,
            "rarity": item.rarity,
            "sortId1": item.sort_id1,
            "sortId2": item.sort_id2,
            "fluidType": None,
            "fluid": None,
            "fullContainers": [],
            "emptyContainers": [],
        }
    return records


def _fluid_maps(
    table_cfg: TableCfg,
) -> tuple[dict[str, str], dict[str, str], dict[str, dict[str, list[str]]]]:
    """气体的罐与液体的瓶：物品 -> 流体种类 / 满容器 -> 里面的流体 / 流体 -> 容器名单。"""
    fluid_type_by_item: dict[str, str] = {}
    fluid_by_full_container: dict[str, str] = {}
    fluid_containers: dict[str, dict[str, list[str]]] = {}

    gases = table_cfg.table("GasTable", GasRow)
    liquids = table_cfg.table("LiquidTable", LiquidRow)
    families: tuple[tuple[str, dict[str, tuple[list[str], list[str]]]], ...] = (
        ("gas", {key: (row.full_gas_jar_items, row.empty_gas_jar_items) for key, row in gases.items()}),
        ("liquid", {key: (row.full_bottle_items, row.empty_bottle_items) for key, row in liquids.items()}),
    )
    for fluid_type, table in families:
        for fluid_id, (full, empty) in table.items():
            fluid_type_by_item[fluid_id] = fluid_type
            for container_id in full:
                fluid_type_by_item[container_id] = fluid_type
                fluid_by_full_container[container_id] = fluid_id
            for container_id in empty:
                fluid_type_by_item[container_id] = fluid_type
            fluid_containers[fluid_id] = {"full": full, "empty": empty}
    return fluid_type_by_item, fluid_by_full_container, fluid_containers


def _region_restricted_items(table_cfg: TableCfg) -> set[str]:
    """只在某些地区存在的工厂建筑所产出的物品（``recommendDomains`` 非空）。"""
    buildings = table_cfg.table("FactoryBuildingTable", FactoryBuildingRow)
    restricted_buildings = {building_id for building_id, building in buildings.items() if building.recommend_domains}
    reverse = table_cfg.table("FactoryBuildingItemReverseTable", FactoryBuildingItemReverseRow)

    item_ids: set[str] = set()
    for building_id, row in reverse.items():
        if building_id not in restricted_buildings:
            continue
        # 这个字段有的版本是字符串、有的是数组。
        produced = row.item_id
        item_ids.update([produced] if isinstance(produced, str) else produced)
    return item_ids
