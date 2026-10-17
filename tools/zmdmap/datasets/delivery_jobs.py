"""运送委托（``delivery_jobs.json``）：地区、仓储节点及其可装箱物品，都取自 TableCfg。"""

from pathlib import Path
from typing import Any

from ..utils.game_data import logger, read_table_cfg
from .base import Dataset, DatasetError

class DeliveryJobs(Dataset):
    """地区、仓储节点及其可装箱物品（``delivery_jobs.json``）。"""

    @classmethod
    def build(cls, table_cfg_dir: Path, json_data_dir: Path) -> dict[str, Any]:
        """Data chain: FactoryItemTable -> deliverable items (deliverItemTypeList +
        transferDomainIds), DomainDepotTable -> depot region/name,
        DomainDepotLevelTable -> per-level deliverItemTypeList,
        DomainDataTable -> region names, ItemTable -> item names."""
        factory_item = read_table_cfg(table_cfg_dir, "FactoryItemTable")
        depot_table = read_table_cfg(table_cfg_dir, "DomainDepotTable")
        depot_level_table = read_table_cfg(table_cfg_dir, "DomainDepotLevelTable")
        domain_table = read_table_cfg(table_cfg_dir, "DomainDataTable")
        item_table = read_table_cfg(table_cfg_dir, "ItemTable")

        # ── 可装箱物品 ───────────────────────────────────────────────
        deliverable_items: list[dict[str, Any]] = []
        for item_id in sorted(factory_item.keys()):
            entry = factory_item[item_id]
            deliver_item_types = entry.deliverItemTypeList
            if not deliver_item_types:
                continue
            deliverable_items.append(
                {
                    "id": item_id,
                    "deliver_item_types": sorted(set(deliver_item_types)),
                    "transfer_regions": set(entry.transferDomainIds),
                }
            )

        # ── 仓储节点 ─────────────────────────────────────────────────
        used_region_ids: set[str] = set()
        used_item_ids: set[str] = set()
        depots: dict[str, Any] = {}

        for depot_id in sorted(depot_table.keys()):
            entry = depot_table[depot_id]
            if entry.domainDepotId and entry.domainDepotId != depot_id:
                raise DatasetError(
                    f"DomainDepotTable 键 {depot_id} 与 domainDepotId {entry.domainDepotId} 不一致"
                )
            region_id = entry.domainId
            if region_id not in domain_table:
                raise DatasetError(f"仓储节点 {depot_id} 引用了未知地区 {region_id}")
            used_region_ids.add(region_id)

            level_record = depot_level_table.get(depot_id)
            if level_record is None:
                raise DatasetError(f"仓储节点 {depot_id} 缺少等级配置")
            level_list = level_record.levelList
            if not level_list:
                raise DatasetError(f"仓储节点 {depot_id} 没有等级配置")

            fillable_item_ids: set[str] = set()
            for level_value in sorted(
                level_list.values(),
                key=lambda value: value.level,
            ):
                deliver_item_types = set(level_value.deliverItemTypeList)
                fillable_item_ids.update(
                    item["id"]
                    for item in deliverable_items
                    if region_id in item["transfer_regions"]
                    and bool(deliver_item_types & set(item["deliver_item_types"]))
                )

            fillable_items = sorted(fillable_item_ids)
            used_item_ids.update(fillable_items)

            depots[depot_id] = {
                "region_id": region_id,
                "names": cls.localized_names(entry.depotName, table_cfg_dir, f"仓储节点 {depot_id}"),
                "fillable_items": fillable_items,
            }

        # ── 地区 ─────────────────────────────────────────────────────
        regions: dict[str, Any] = {}
        for region_id in sorted(used_region_ids):
            entry = domain_table[region_id]
            if entry.domainName is None:
                raise DatasetError(f"地区 {region_id} 缺少 domainName")
            regions[region_id] = {
                "names": cls.localized_names(entry.domainName, table_cfg_dir, f"地区 {region_id}"),
            }

        # ── 物品 ─────────────────────────────────────────────────────
        items: dict[str, Any] = {}
        for item_id in sorted(used_item_ids):
            item_entry = item_table.get(item_id)
            if item_entry is None:
                raise DatasetError(f"ItemTable 缺少可装箱物品 {item_id}")
            items[item_id] = {
                "names": cls.localized_names(item_entry.name, table_cfg_dir, f"物品 {item_id}"),
            }

        logger.info(
            f"Built maaend delivery_jobs: {len(regions)} regions, "
            + f"{len(depots)} depots, {len(items)} items"
        )
        return {
            "regions": regions,
            "depots": depots,
            "items": items,
        }

