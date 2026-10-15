"""MaaEnd 精简游戏数据 substep: sell_product.json.

从 TableCfg 生成据点、可售物品、据点特性与匹配干员。
"""

from typing import Any

from local_context import I18nText, LanguageCode, RunContext, logger

MAAEND_LOCALES: tuple[tuple[str, LanguageCode], ...] = (
    ("zh_cn", LanguageCode.ZH_CN),
    ("zh_tw", LanguageCode.ZH_TW),
    ("en_us", LanguageCode.EN_US),
    ("ja_jp", LanguageCode.JA_JP),
    ("ko_kr", LanguageCode.KO_KR),
)

# settlement tag 增强字段 -> 输出中的 bonus 类型名。
SETTLEMENT_BONUS_FIELDS: dict[str, str] = {
    "enhanceExpProfitRate": "expProfit",
    "enhanceMoneyProfitRate": "moneyProfit",
    "enhanceMoneyProduceSpeedRate": "moneyProduceSpeed",
}

def build_sell_product(ctx: RunContext) -> dict[str, Any]:
    """Data chain: SettlementBasicDataTable -> settlements/prosperity levels/
    wantTagIdGroup, SettlementTagTable -> feature tags + enhance rates,
    CharacterTable + CharacterTagTable -> operator tags/names,
    ItemTable -> item names/rarity."""
    tcfg = ctx.table_cfg
    settlement_table = tcfg.SettlementBasicDataTable
    settlement_tag_table = tcfg.SettlementTagTable
    character_table = tcfg.CharacterTable
    character_tag_table = tcfg.CharacterTagTable
    item_table = tcfg.ItemTable

    # ── 干员 ─────────────────────────────────────────────────────
    operator_tags: list[dict[str, Any]] = []
    for operator_id in sorted(character_tag_table.keys()):
        tag_entry = character_tag_table[operator_id]
        character = character_table.get(operator_id)
        if character is None:
            raise ValueError(f"CharacterTable 缺少干员 {operator_id}")

        tags = [
            tag_entry.blocTagId,
            *tag_entry.expertTagIds,
            *tag_entry.hobbyTagIds,
        ]
        operator_tags.append(
            {
                "id": operator_id,
                "tags": [tag for tag in tags if tag],
                "names": _build_localized_names(
                    character.name, ctx, f"干员 {operator_id}"
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

        settlement_level_map = settlement.settlementLevelMap
        prosperity_levels: list[dict[str, Any]] = []
        for level, level_value in sorted(
            settlement_level_map.items(), key=lambda entry: int(entry[0])
        ):
            trade_item_map = level_value.settlementTradeItemMap
            trade_items: list[dict[str, Any]] = []
            for trade_key, trade_value in trade_item_map.items():
                # itemId 优先取 value 字段，缺失时 fallback 到 map key。
                item_id = trade_value.itemId or trade_key
                if item_id not in item_table:
                    raise ValueError(
                        f"据点 {settlement_id} 引用了 ItemTable 中不存在的物品 {item_id}"
                    )
                used_item_ids.add(item_id)
                trade_items.append(
                    {
                        "item_id": item_id,
                        "unit_price": trade_value.rewardMoneyCount,
                        "activity_id": trade_value.activityId or "",
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
        for settlement_tag_id in settlement.wantTagIdGroup:
            tag_entry = settlement_tag_table.get(settlement_tag_id)
            if tag_entry is None:
                raise ValueError(f"据点特性 {settlement_tag_id} 不存在")
            required_tags = tag_entry.enhanceCharTagId
            features.append(
                {
                    "bonus_types": [
                        bonus_type
                        for field, bonus_type in SETTLEMENT_BONUS_FIELDS.items()
                        if getattr(tag_entry, field, 0) > 0
                    ],
                    "operator_ids": [
                        operator["id"]
                        for operator in operator_tags
                        if bool(set(operator["tags"]) & set(required_tags))
                    ],
                }
            )

        settlements[settlement_id] = {
            "id": settlement_id,
            "region_id": settlement.domainId,
            "names": _build_localized_names(
                settlement.settlementName, ctx, f"据点 {settlement_id}"
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
            "names": _build_localized_names(entry.name, ctx, f"售卖物品 {item_id}"),
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

def _build_localized_names(
    ref: I18nText,
    ctx: RunContext,
    label: str,
) -> dict[str, str]:
    """Resolve an I18nText id into the 5-language dict MaaEnd expects."""
    if ref.text:
        return {locale: ref.text for locale, _ in MAAEND_LOCALES}
    text_id = ref.id
    if not text_id:
        raise ValueError(f"{label} 缺少 i18n id")

    names: dict[str, str] = {}
    for locale, lang_code in MAAEND_LOCALES:
        text = ctx.i18n.get_localized_text(text_id, lang_code)
        if not text:
            raise ValueError(f"{label} 在 {locale} 中缺少文本 {text_id}")
        names[locale] = text
    return names
