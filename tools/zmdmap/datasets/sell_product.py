"""据点交易（``sell_product.json``）：据点、可售物品、据点特性与匹配干员，都取自 TableCfg。"""

from pathlib import Path
from typing import Any

from ..utils.log import logger
from ..utils.tables import CharacterRow, CharacterTagRow, ItemRow, SettlementRow, SettlementTagRow, load_table
from .base import Dataset, DatasetError


class SellProduct(Dataset):
    """据点、可售物品、据点特性与匹配干员（``sell_product.json``）。"""

    @classmethod
    def build(cls, table_cfg_dir: Path, json_data_dir: Path) -> dict[str, Any]:
        """据点、各级繁荣度能卖的物品、据点特性，以及按标签匹配上的干员。

        干员的标签来自 ``CharacterTagTable``，据点特性按 ``wantTagIdGroup`` 去匹配；能卖什么由
        ``SettlementBasicDataTable`` 的各级 ``settlementTradeItemMap`` 给出，并排掉限定活动的物品。
        """
        settlement_table = load_table(table_cfg_dir, "SettlementBasicDataTable", SettlementRow)
        settlement_tag_table = load_table(table_cfg_dir, "SettlementTagTable", SettlementTagRow)
        character_table = load_table(table_cfg_dir, "CharacterTable", CharacterRow)
        character_tag_table = load_table(table_cfg_dir, "CharacterTagTable", CharacterTagRow)
        item_table = load_table(table_cfg_dir, "ItemTable", ItemRow)

        # ── 干员 ─────────────────────────────────────────────────────
        operator_tags: list[dict[str, Any]] = []
        for operator_id in sorted(character_tag_table.keys()):
            tag_entry = character_tag_table[operator_id]
            character = character_table.get(operator_id)
            if character is None:
                raise DatasetError(f"CharacterTable 缺少干员 {operator_id}")

            tags = [tag_entry.bloc_tag_id, *tag_entry.expert_tag_ids, *tag_entry.hobby_tag_ids]
            operator_tags.append(
                {
                    "id": operator_id,
                    "tags": [tag for tag in tags if tag],
                    "names": cls.localized_names(
                        character.name, table_cfg_dir, f"干员 {operator_id}"
                    ),
                }
            )

        operators: dict[str, Any] = {
            operator["id"]: {
                "id": operator["id"],
                "names": operator["names"],
            }
            for operator in operator_tags
        }

        # ── 据点 ─────────────────────────────────────────────────────
        used_item_ids: set[str] = set()
        settlements: dict[str, Any] = {}

        for settlement_id in sorted(settlement_table.keys()):
            settlement = settlement_table[settlement_id]

            settlement_level_map = settlement.settlement_level_map
            prosperity_levels: list[dict[str, Any]] = []
            for level, level_value in sorted(
                settlement_level_map.items(), key=lambda entry: int(entry[0])
            ):
                trade_item_map = level_value.settlement_trade_item_map
                trade_items: list[dict[str, Any]] = []
                for trade_key, trade_value in trade_item_map.items():
                    # itemId 优先取 value 字段，缺失时 fallback 到 map key。
                    item_id = trade_value.item_id or trade_key
                    if item_id not in item_table:
                        raise DatasetError(
                            f"据点 {settlement_id} 引用了 ItemTable 中不存在的物品 {item_id}"
                        )
                    used_item_ids.add(item_id)
                    trade_items.append(
                        {
                            "item_id": item_id,
                            "unit_price": trade_value.reward_money_count,
                            "activity_id": trade_value.activity_id,
                        }
                    )
                trade_items.sort(key=lambda item: item["unit_price"], reverse=True)
                prosperity_levels.append(
                    {
                        "level": int(level),
                        "trade_items": trade_items,
                    }
                )

            features: list[dict[str, Any]] = []
            for settlement_tag_id in settlement.want_tag_id_group:
                tag_entry = settlement_tag_table.get(settlement_tag_id)
                if tag_entry is None:
                    raise DatasetError(f"据点特性 {settlement_tag_id} 不存在")
                required_tags = set(tag_entry.enhance_char_tag_id)
                features.append(
                    {
                        "bonus_types": _bonus_types(tag_entry),
                        "operator_ids": [
                            operator["id"]
                            for operator in operator_tags
                            if bool(set(operator["tags"]) & required_tags)
                        ],
                    }
                )

            settlements[settlement_id] = {
                "id": settlement_id,
                "region_id": settlement.domain_id,
                "names": cls.localized_names(
                    settlement.settlement_name, table_cfg_dir, f"据点 {settlement_id}"
                ),
                "prosperity_levels": prosperity_levels,
                "features": features,
            }

        # ── 物品 ─────────────────────────────────────────────────────
        items: dict[str, Any] = {}
        for item_id in sorted(used_item_ids):
            entry = item_table[item_id]
            items[item_id] = {
                "id": item_id,
                "names": cls.localized_names(entry.name, table_cfg_dir, f"售卖物品 {item_id}"),
                "rarity": entry.rarity,
            }

        logger.info(
            f"Built maaend sell_product: {len(items)} items, "
            + f"{len(operators)} operators, {len(settlements)} settlements"
        )
        return {
            "items": items,
            "operators": operators,
            "settlements": settlements,
        }


def _bonus_types(tag: SettlementTagRow) -> list[str]:
    """这个据点特性的三项收益里，哪几项是「有」的。"""
    bonuses: list[str] = []
    if tag.enhance_exp_profit_rate > 0:
        bonuses.append("expProfit")
    if tag.enhance_money_profit_rate > 0:
        bonuses.append("moneyProfit")
    if tag.enhance_money_produce_speed_rate > 0:
        bonuses.append("moneyProduceSpeed")
    return bonuses
