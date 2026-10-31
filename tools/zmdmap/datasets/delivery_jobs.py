"""运送委托（``delivery_jobs.json``）：地区、仓储节点、最高基础报价及可装箱物品，都取自 TableCfg。

``depots[id].max_base_quote`` 是正整数，单位为调度券，表示该节点配置允许的最高基础报价。
计算使用以下游戏表：

- ``DomainDepotLevelTable``：各等级实际允许的货物类型与货箱大小；两者必须在同一级同时允许。
- ``DomainDepotPackValueTable``：对应货物与货箱组合的装箱价值上限 ``maxLimit``。
- ``DomainDepotDeliverItemTypeTable``：货物类型的价格系数 ``priceFactor``。
- ``DomainDataTable``：所在地区的报价倍率 ``domainDepotOfferPriceRatio``。

对每个允许的组合计算 ``floor(maxLimit * priceFactor * domainDepotOfferPriceRatio)``，再取最大值。
计算不固定等级、货物类型或货箱大小，新增等级或节点时按配置自动重算。

该字段不含买家随机倍率或转交后的尾款比例。买家档位选择和界面显示精度的取整由下游任务生成器处理。
"""

from dataclasses import dataclass
from math import floor, isfinite
from typing import Any

from ..utils.inputs import TableCfg
from ..utils.log import logger
from ..utils.tables import (
    DomainDepotDeliverItemTypeRow,
    DomainDepotLevel,
    DomainDepotLevelRow,
    DomainDepotPackValueRow,
    DomainDepotRow,
    DomainRow,
    FactoryItemRow,
    ItemRow,
)
from .base import Dataset, DatasetError


@dataclass(frozen=True)
class DeliverableItem:
    """一件可装箱物品：能装它的运送类型与可转运到的地区。

    这两样只用来跟仓储节点的等级配置做交集，不进产物，所以用集合；产物里出现的是它的 id。
    """

    item_types: frozenset[str]
    regions: frozenset[str]


def _max_base_quote(
    depot_id: str,
    levels: list[DomainDepotLevel],
    pack_values: DomainDepotPackValueRow,
    price_factors: dict[str, float],
    domain_ratio: float,
) -> int:
    """按装箱页公式取配置可达到的最高预计收益，不含随机买家倍率与尾款。"""
    if not isfinite(domain_ratio) or domain_ratio <= 0:
        raise DatasetError(f"仓储节点 {depot_id} 的地区报价倍率无效：{domain_ratio}")
    allowed = {
        (item_type, pack_type)
        for level in levels
        for item_type in level.deliver_item_type_list
        for pack_type in level.deliver_pack_type_list
    }
    maximum = 0
    for value in pack_values.pack_value_list:
        if (value.deliver_item_type, value.deliver_pack_type) not in allowed:
            continue
        factor = price_factors.get(value.deliver_item_type)
        if factor is None or not isfinite(factor) or factor <= 0:
            raise DatasetError(f"仓储节点 {depot_id} 的货物类型 {value.deliver_item_type} 缺少有效价格系数")
        if value.max_limit <= 0:
            raise DatasetError(f"仓储节点 {depot_id} 的货箱 {value.deliver_pack_type} 价值上限无效")
        quote = value.max_limit * factor * domain_ratio
        if not isfinite(quote):
            raise DatasetError(f"仓储节点 {depot_id} 的基础报价超出有效范围")
        maximum = max(maximum, floor(quote))
    if maximum <= 0:
        raise DatasetError(f"仓储节点 {depot_id} 没有可用的基础报价配置")
    return maximum


class DeliveryJobs(Dataset):
    """地区、仓储节点、最高基础报价及可装箱物品（``delivery_jobs.json``）。"""

    @classmethod
    def build(cls, table_cfg: TableCfg) -> dict[str, Any]:
        """地区、仓储节点、配置允许的最高基础报价，以及每一级能装哪些物品。

        装配范围是「物品能装的运送类型」与「这一级能装的运送类型」的交集；物品还要能转运到这个节点所在的
        地区（``transferDomainIds``）。物品名、地区名都取五语；基础报价取允许的货物与货箱组合的最高预计收益。
        """
        factory_item = table_cfg.table("FactoryItemTable", FactoryItemRow)
        depot_table = table_cfg.table("DomainDepotTable", DomainDepotRow)
        depot_level_table = table_cfg.table("DomainDepotLevelTable", DomainDepotLevelRow)
        pack_value_table = table_cfg.table("DomainDepotPackValueTable", DomainDepotPackValueRow)
        item_type_table = table_cfg.table("DomainDepotDeliverItemTypeTable", DomainDepotDeliverItemTypeRow)
        domain_table = table_cfg.table("DomainDataTable", DomainRow)
        item_table = table_cfg.table("ItemTable", ItemRow)
        price_factors: dict[str, float] = {}
        for item_type in item_type_table.values():
            if item_type.deliver_item_type in price_factors:
                raise DatasetError(f"货物类型 {item_type.deliver_item_type} 的价格系数重复")
            price_factors[item_type.deliver_item_type] = item_type.price_factor

        # ── 可装箱物品 ───────────────────────────────────────────────
        deliverable_items: dict[str, DeliverableItem] = {}
        for item_id in sorted(factory_item.keys()):
            entry = factory_item[item_id]
            if not entry.deliver_item_type_list:
                continue
            deliverable_items[item_id] = DeliverableItem(
                item_types=frozenset(entry.deliver_item_type_list),
                regions=frozenset(entry.transfer_domain_ids),
            )

        # ── 仓储节点 ─────────────────────────────────────────────────
        used_region_ids: set[str] = set()
        used_item_ids: set[str] = set()
        depots: dict[str, Any] = {}

        for depot_id in sorted(depot_table.keys()):
            entry = depot_table[depot_id]
            if entry.domain_depot_id and entry.domain_depot_id != depot_id:
                raise DatasetError(f"DomainDepotTable 键 {depot_id} 与 domainDepotId {entry.domain_depot_id} 不一致")
            region_id = entry.domain_id
            if region_id not in domain_table:
                raise DatasetError(f"仓储节点 {depot_id} 引用了未知地区 {region_id}")
            used_region_ids.add(region_id)

            level_record = depot_level_table.get(depot_id)
            if level_record is None:
                raise DatasetError(f"仓储节点 {depot_id} 缺少等级配置")
            level_list = level_record.level_list
            if not level_list:
                raise DatasetError(f"仓储节点 {depot_id} 没有等级配置")
            pack_values = pack_value_table.get(depot_id)
            if pack_values is None:
                raise DatasetError(f"仓储节点 {depot_id} 缺少货箱价值配置")
            max_base_quote = _max_base_quote(
                depot_id,
                list(level_list.values()),
                pack_values,
                price_factors,
                domain_table[region_id].domain_depot_offer_price_ratio,
            )

            fillable_item_ids: set[str] = set()
            for level_value in sorted(
                level_list.values(),
                key=lambda value: value.level,
            ):
                level_item_types = frozenset(level_value.deliver_item_type_list)
                fillable_item_ids.update(
                    item_id
                    for item_id, item in deliverable_items.items()
                    if region_id in item.regions and level_item_types & item.item_types
                )

            fillable_items = sorted(fillable_item_ids)
            used_item_ids.update(fillable_items)

            depots[depot_id] = {
                "region_id": region_id,
                "names": cls.localized_names(entry.depot_name, table_cfg, f"仓储节点 {depot_id}"),
                "max_base_quote": max_base_quote,
                "fillable_items": fillable_items,
            }

        # ── 地区 ─────────────────────────────────────────────────────
        regions: dict[str, Any] = {}
        for region_id in sorted(used_region_ids):
            entry = domain_table[region_id]
            if not entry.domain_name:
                raise DatasetError(f"地区 {region_id} 缺少 domainName")
            regions[region_id] = {
                "names": cls.localized_names(entry.domain_name, table_cfg, f"地区 {region_id}"),
            }

        # ── 物品 ─────────────────────────────────────────────────────
        items: dict[str, Any] = {}
        for item_id in sorted(used_item_ids):
            item_entry = item_table.get(item_id)
            if item_entry is None:
                raise DatasetError(f"ItemTable 缺少可装箱物品 {item_id}")
            items[item_id] = {
                "names": cls.localized_names(item_entry.name, table_cfg, f"物品 {item_id}"),
            }

        logger.info(
            f"Built maaend delivery_jobs: {len(regions)} regions, " + f"{len(depots)} depots, {len(items)} items"
        )
        return {
            "regions": regions,
            "depots": depots,
            "items": items,
        }
