from __future__ import annotations

from typing import Any

from tablecfg_utils import (
    DATA_DIR,
    LOCALE_TABLES,
    assert_list,
    assert_record,
    build_locale_tables,
    build_localized_names,
    intersects,
    run_cli,
    sorted_entries,
    unique_sorted_numbers,
)


TABLE_NAMES = (
    "DomainDataTable.json",
    "DomainDepotTable.json",
    "DomainDepotLevelTable.json",
    "FactoryItemTable.json",
    "ItemTable.json",
    *LOCALE_TABLES,
)
OUTPUT_PATH = DATA_DIR / "delivery_jobs.json"


def build_delivery_jobs_data(tables: dict[str, Any]) -> dict[str, Any]:
    domain_table = assert_record(tables["DomainDataTable.json"], "DomainDataTable")
    depot_table = assert_record(tables["DomainDepotTable.json"], "DomainDepotTable")
    depot_level_table = assert_record(
        tables["DomainDepotLevelTable.json"], "DomainDepotLevelTable"
    )
    factory_item_table = assert_record(
        tables["FactoryItemTable.json"], "FactoryItemTable"
    )
    item_table = assert_record(tables["ItemTable.json"], "ItemTable")
    locale_tables = build_locale_tables(tables)

    deliverable_items = []
    for item_id, entry_value in sorted_entries(factory_item_table):
        entry = assert_record(entry_value, f"物品 {item_id}")
        deliver_item_type_list = entry.get("deliverItemTypeList")
        if not isinstance(deliver_item_type_list, list) or not deliver_item_type_list:
            continue
        deliverable_items.append(
            {
                "id": item_id,
                "deliver_item_types": unique_sorted_numbers(
                    deliver_item_type_list,
                    f"物品 {item_id} deliverItemTypeList",
                ),
                "show_in_regions": set(
                    assert_list(
                        entry.get("showInHubDomainIds"),
                        f"物品 {item_id} showInHubDomainIds",
                    )
                ),
            }
        )

    used_region_ids: set[str] = set()
    used_item_ids: set[str] = set()
    depots: dict[str, Any] = {}
    for depot_id, entry_value in sorted_entries(depot_table):
        entry = assert_record(entry_value, f"仓储节点 {depot_id}")
        if "domainDepotId" in entry and entry["domainDepotId"] != depot_id:
            raise ValueError(
                f"DomainDepotTable 键 {depot_id} 与 domainDepotId {entry['domainDepotId']} 不一致"
            )
        region_id = entry.get("domainId")
        if region_id not in domain_table:
            raise ValueError(f"仓储节点 {depot_id} 引用了未知地区 {region_id}")
        used_region_ids.add(region_id)

        level_record = assert_record(
            depot_level_table.get(depot_id), f"仓储节点 {depot_id} 等级配置"
        )
        level_list = assert_record(
            level_record.get("levelList"), f"仓储节点 {depot_id} levelList"
        )
        fillable_item_ids: set[str] = set()
        has_level = False
        for level_value in sorted(
            level_list.values(),
            key=lambda value: assert_record(value, f"仓储节点 {depot_id} 等级").get(
                "level"
            ),
        ):
            has_level = True
            level = assert_record(level_value, f"仓储节点 {depot_id} 等级")
            deliver_item_types = unique_sorted_numbers(
                level.get("deliverItemTypeList"),
                f"仓储节点 {depot_id} 等级 {level.get('level')} deliverItemTypeList",
            )
            fillable_item_ids.update(
                item["id"]
                for item in deliverable_items
                if region_id in item["show_in_regions"]
                and intersects(item["deliver_item_types"], deliver_item_types)
            )
        if not has_level:
            raise ValueError(f"仓储节点 {depot_id} 没有等级配置")
        fillable_items = sorted(fillable_item_ids)
        used_item_ids.update(fillable_items)

        depots[depot_id] = {
            "region_id": region_id,
            "names": build_localized_names(
                entry.get("depotName"), locale_tables, f"仓储节点 {depot_id}"
            ),
            "fillable_items": fillable_items,
        }

    regions = {}
    for region_id in sorted(used_region_ids):
        entry = assert_record(domain_table[region_id], f"地区 {region_id}")
        regions[region_id] = {
            "names": build_localized_names(
                entry.get("domainName"), locale_tables, f"地区 {region_id}"
            ),
        }

    items = {}
    for item_id in sorted(used_item_ids):
        item_entry = assert_record(
            item_table.get(item_id), f"ItemTable 可装箱物品 {item_id}"
        )
        items[item_id] = {
            "names": build_localized_names(
                item_entry.get("name"), locale_tables, f"物品 {item_id}"
            ),
        }

    return {
        "regions": regions,
        "depots": depots,
        "items": items,
    }


def main() -> int:
    return run_cli(
        label="DeliveryJobs",
        description="从本地 TableCfg 生成转交委托数据",
        table_names=TABLE_NAMES,
        output_path=OUTPUT_PATH,
        build_data=build_delivery_jobs_data,
    )


if __name__ == "__main__":
    raise SystemExit(main())
