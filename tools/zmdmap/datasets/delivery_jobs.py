"""运送委托（``delivery_jobs.json``）：地区、仓储节点及其可装箱物品，都取自 TableCfg。"""

from dataclasses import dataclass
from typing import Any

from ..utils.inputs import TableCfg
from ..utils.log import logger
from ..utils.tables import (
    DomainDepotLevelRow,
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


class DeliveryJobs(Dataset):
    """地区、仓储节点及其可装箱物品（``delivery_jobs.json``）。"""

    @classmethod
    def build(cls, table_cfg: TableCfg) -> dict[str, Any]:
        """地区、仓储节点，以及每个节点每一级能装哪些物品。

        装配范围是「物品能装的运送类型」与「这一级能装的运送类型」的交集；物品还要能转运到这个节点所在的
        地区（``transferDomainIds``）。物品名、地区名都取五语。
        """
        factory_item = table_cfg.table("FactoryItemTable", FactoryItemRow)
        depot_table = table_cfg.table("DomainDepotTable", DomainDepotRow)
        depot_level_table = table_cfg.table("DomainDepotLevelTable", DomainDepotLevelRow)
        domain_table = table_cfg.table("DomainDataTable", DomainRow)
        item_table = table_cfg.table("ItemTable", ItemRow)

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
