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
)


TABLE_NAMES = (
    "SettlementBasicDataTable.json",
    "SettlementTagTable.json",
    "CharacterTable.json",
    "CharacterTagTable.json",
    "ItemTable.json",
    *LOCALE_TABLES,
)
OUTPUT_PATH = DATA_DIR / "sell_product.json"

SETTLEMENT_BONUS_FIELDS = {
    "enhanceExpProfitRate": "expProfit",
    "enhanceMoneyProfitRate": "moneyProfit",
    "enhanceMoneyProduceSpeedRate": "moneyProduceSpeed",
}


def build_character_tags(entry: dict[str, Any]) -> list[Any]:
    tags = [
        entry.get("blocTagId"),
        *assert_list(
            entry.get("expertTagIds"), f"干员 {entry.get('charId')} expertTagIds"
        ),
        *assert_list(
            entry.get("hobbyTagIds"), f"干员 {entry.get('charId')} hobbyTagIds"
        ),
    ]
    return [tag for tag in tags if tag]


def build_sell_product_data(tables: dict[str, Any]) -> dict[str, Any]:
    settlement_table = assert_record(
        tables["SettlementBasicDataTable.json"], "SettlementBasicDataTable"
    )
    settlement_tag_table = assert_record(
        tables["SettlementTagTable.json"], "SettlementTagTable"
    )
    character_table = assert_record(tables["CharacterTable.json"], "CharacterTable")
    character_tag_table = assert_record(
        tables["CharacterTagTable.json"], "CharacterTagTable"
    )
    item_table = assert_record(tables["ItemTable.json"], "ItemTable")
    locale_tables = build_locale_tables(tables)

    operator_tags = []
    for operator_id, tag_value in sorted_entries(character_tag_table):
        tag_entry = assert_record(tag_value, f"干员 {operator_id} 标签")
        if "charId" in tag_entry and tag_entry["charId"] != operator_id:
            raise ValueError(
                f"CharacterTagTable 键 {operator_id} 与 charId {tag_entry['charId']} 不一致"
            )
        character = assert_record(
            character_table.get(operator_id), f"CharacterTable 干员 {operator_id}"
        )
        operator_tags.append(
            {
                "id": operator_id,
                "tags": build_character_tags(tag_entry),
                "names": build_localized_names(
                    character.get("name"), locale_tables, f"干员 {operator_id}"
                ),
            }
        )
    operators = {
        operator["id"]: {
            "id": operator["id"],
            "names": operator["names"],
        }
        for operator in operator_tags
    }

    used_item_ids: set[str] = set()
    settlements = {}
    for settlement_id, settlement_value in settlement_table.items():
        settlement = assert_record(settlement_value, f"据点 {settlement_id}")
        if "settlementId" in settlement and settlement["settlementId"] != settlement_id:
            raise ValueError(
                f"SettlementBasicDataTable 键 {settlement_id} 与 settlementId {settlement['settlementId']} 不一致"
            )

        settlement_level_map = assert_record(
            settlement.get("settlementLevelMap"),
            f"据点 {settlement_id} settlementLevelMap",
        )
        prosperity_levels = []
        for level, level_value in sorted(
            settlement_level_map.items(), key=lambda entry: int(entry[0])
        ):
            level_entry = assert_record(
                level_value, f"据点 {settlement_id} 等级 {level}"
            )
            trade_item_map = assert_record(
                level_entry.get("settlementTradeItemMap"),
                f"据点 {settlement_id} 等级 {level} 货品",
            )
            trade_items = []
            for trade_value in trade_item_map.values():
                trade_item = assert_record(
                    trade_value, f"据点 {settlement_id} 等级 {level} 货品"
                )
                item_id = trade_item.get("itemId")
                if item_id not in item_table:
                    raise ValueError(
                        f"据点 {settlement_id} 引用了 ItemTable 中不存在的物品 {item_id}"
                    )
                used_item_ids.add(item_id)
                trade_items.append(
                    {
                        "item_id": item_id,
                        "unit_price": trade_item.get("rewardMoneyCount"),
                    }
                )
            trade_items.sort(key=lambda item: item["unit_price"], reverse=True)
            prosperity_levels.append(
                {
                    "level": int(level),
                    "trade_items": trade_items,
                }
            )

        features = []
        for settlement_tag_id in assert_list(
            settlement.get("wantTagIdGroup"), f"据点 {settlement_id} wantTagIdGroup"
        ):
            tag_entry = assert_record(
                settlement_tag_table.get(settlement_tag_id),
                f"据点特性 {settlement_tag_id}",
            )
            required_tags = assert_list(
                tag_entry.get("enhanceCharTagId"),
                f"据点特性 {settlement_tag_id} enhanceCharTagId",
            )
            features.append(
                {
                    "bonus_types": [
                        bonus_type
                        for field, bonus_type in SETTLEMENT_BONUS_FIELDS.items()
                        if isinstance(tag_entry.get(field), (int, float))
                        and tag_entry[field] > 0
                    ],
                    "operator_ids": [
                        operator["id"]
                        for operator in operator_tags
                        if intersects(operator["tags"], required_tags)
                    ],
                }
            )

        settlements[settlement_id] = {
            "id": settlement_id,
            "region_id": settlement.get("domainId"),
            "names": build_localized_names(
                settlement.get("settlementName"), locale_tables, f"据点 {settlement_id}"
            ),
            "prosperity_levels": prosperity_levels,
            "features": features,
        }

    items = {}
    for item_id in sorted(used_item_ids):
        entry = assert_record(item_table[item_id], f"售卖物品 {item_id}")
        items[item_id] = {
            "id": item_id,
            "names": build_localized_names(
                entry.get("name"), locale_tables, f"售卖物品 {item_id}"
            ),
            "rarity": entry.get("rarity"),
        }

    return {
        "items": items,
        "operators": operators,
        "settlements": settlements,
    }


def main() -> int:
    return run_cli(
        label="SellProduct",
        description="从本地 TableCfg 生成售卖产品数据",
        table_names=TABLE_NAMES,
        output_path=OUTPUT_PATH,
        build_data=build_sell_product_data,
    )


if __name__ == "__main__":
    raise SystemExit(main())
