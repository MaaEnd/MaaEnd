"""MaaEnd 精简游戏数据 substep: delivery_jobs.json.

从 TableCfg 生成地区、仓储节点及其可装箱物品数据。
"""

from pathlib import Path
from typing import Any

from ..constants import MAAEND_LOCALES
from ..utils.game_data import Record, localized_text, logger, read_table_cfg

def build_delivery_jobs(table_cfg_dir: Path, json_data_dir: Path) -> dict[str, Any]:
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
            raise ValueError(
                f"DomainDepotTable 键 {depot_id} 与 domainDepotId {entry.domainDepotId} 不一致"
            )
        region_id = entry.domainId
        if region_id not in domain_table:
            raise ValueError(f"仓储节点 {depot_id} 引用了未知地区 {region_id}")
        used_region_ids.add(region_id)

        level_record = depot_level_table.get(depot_id)
        if level_record is None:
            raise ValueError(f"仓储节点 {depot_id} 缺少等级配置")
        level_list = level_record.levelList
        if not level_list:
            raise ValueError(f"仓储节点 {depot_id} 没有等级配置")

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
            "names": _build_localized_names(entry.depotName, table_cfg_dir, f"仓储节点 {depot_id}"),
            "fillable_items": fillable_items,
        }

    # ── 地区 ─────────────────────────────────────────────────────
    regions: dict[str, Any] = {}
    for region_id in sorted(used_region_ids):
        entry = domain_table[region_id]
        if entry.domainName is None:
            raise ValueError(f"地区 {region_id} 缺少 domainName")
        regions[region_id] = {
            "names": _build_localized_names(entry.domainName, table_cfg_dir, f"地区 {region_id}"),
        }

    # ── 物品 ─────────────────────────────────────────────────────
    items: dict[str, Any] = {}
    for item_id in sorted(used_item_ids):
        item_entry = item_table.get(item_id)
        if item_entry is None:
            raise ValueError(f"ItemTable 缺少可装箱物品 {item_id}")
        items[item_id] = {
            "names": _build_localized_names(item_entry.name, table_cfg_dir, f"物品 {item_id}"),
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

def _build_localized_names(
    ref: Record,
    table_cfg_dir: Path,
    label: str,
) -> dict[str, str]:
    """把 ``ref`` 里的文本 id 解析成 MaaEnd 需要的五语言字典。"""
    if ref.text:
        return {locale: ref.text for locale, _ in MAAEND_LOCALES}
    text_id = ref.id
    if not text_id:
        raise ValueError(f"{label} 缺少 i18n id")

    names: dict[str, str] = {}
    for locale, lang_code in MAAEND_LOCALES:
        text = localized_text(table_cfg_dir, text_id, lang_code)
        if not text:
            raise ValueError(f"{label} 在 {locale} 中缺少文本 {text_id}")
        names[locale] = text
    return names
