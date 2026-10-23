"""稳定需求物资：地区商店中的周期补货商品及其五语名称。

DomainDataTable -> ShopGroupTable -> ShopTable -> ShopGoodsTable ->
RewardTable -> ItemTable；分类来自 ShopGoodsTagTable。渠道等级的供货量
来自 ShopChannelDevelopmentTable，不把同一物品的不同售卖档合并成一个价格。
只读 TableCfg，不依赖其他精简数据、网站页面或人工商品名单。
"""

import math
from pathlib import Path
from typing import Any

import msgspec

from ..utils.tables import (
    DomainRow,
    ItemRow,
    RewardRow,
    ShopChannelRow,
    ShopGoodsRow,
    ShopGoodsTagRow,
    ShopGroupRow,
    ShopRow,
    load_table,
)
from .base import Dataset, DatasetError


def _entry[T](table: dict[str, T], key: str, label: str) -> T:
    if key not in table:
        raise DatasetError(f"{label} 引用了不存在的 {key}")
    return table[key]


class StockStaple(Dataset):
    """常规地区商店的可补货商品（不含一次性商品和弹性需求物资）。"""

    @classmethod
    def build(cls, table_cfg_dir: Path, json_data_dir: Path) -> dict[str, Any]:
        domains = load_table(table_cfg_dir, "DomainDataTable", DomainRow)
        groups = load_table(table_cfg_dir, "ShopGroupTable", ShopGroupRow)
        shops = load_table(table_cfg_dir, "ShopTable", ShopRow)
        goods = load_table(table_cfg_dir, "ShopGoodsTable", ShopGoodsRow)
        rewards = load_table(table_cfg_dir, "RewardTable", RewardRow)
        items = load_table(table_cfg_dir, "ItemTable", ItemRow)
        tags = load_table(table_cfg_dir, "ShopGoodsTagTable", ShopGoodsTagRow)
        channels = load_table(table_cfg_dir, "ShopChannelDevelopmentTable", ShopChannelRow)

        supplies: dict[str, list[dict[str, Any]]] = {}
        for channel_id, channel in sorted(channels.items()):
            for level, row in sorted(channel.channel_level_map.items(), key=lambda x: int(x[0])):
                if len(row.new_goods_list) != len(row.new_goods_num):
                    raise DatasetError(f"渠道 {channel_id}/{level} 商品与数量不对应")
                for goods_id, count in zip(row.new_goods_list, row.new_goods_num, strict=True):
                    supplies.setdefault(goods_id, []).append({
                        "channel_id": channel_id,
                        "level": int(level),
                        "count": count,
                        "version_start": row.version_start,
                        "time_id": row.time_id,
                    })

        output: dict[str, dict[str, Any]] = {
            "regions": {},
            "categories": {},
            "items": {},
            "goods": {},
        }
        for region_id, domain in sorted(domains.items(), key=lambda x: (x[1].sort_id, x[0])):
            group_id = domain.domain_shop_group_id
            if not group_id:
                continue
            group = _entry(groups, group_id, f"地区 {region_id}")
            region_goods: list[str] = []
            for shop_id in group.shop_ids:
                shop = _entry(shops, shop_id, f"商店组 {group_id}")
                # TableCfg 的枚举可以序列化为名字或整数；None=0，RefreshRandom=2。
                if shop.shop_refresh_type not in ("None", 0):
                    continue
                for goods_id in shop.shop_goods_ids:
                    row = _entry(goods, goods_id, f"商店 {shop_id}")
                    if row.shop_id != shop_id:
                        raise DatasetError(f"商品 {goods_id} 的 shopId 与商店目录不一致")
                    if row.limit_count_refresh_type in ("None", 0):
                        continue
                    reward = _entry(rewards, row.reward_id, f"商品 {goods_id}")
                    if reward.prob_item_bundles or len(reward.item_bundles) != 1:
                        raise DatasetError(f"商品 {goods_id} 不是单物品固定奖励")
                    bundle = reward.item_bundles[0]
                    item_id = bundle.id
                    item = _entry(items, item_id, f"奖励 {row.reward_id}")
                    tag = _entry(tags, row.goods_tag_id, f"商品 {goods_id}")
                    currency = _entry(items, row.money_id, f"商品 {goods_id} 币种")
                    values: tuple[float, float, int] = (row.price, row.cn_discount, bundle.count)
                    if any(not math.isfinite(value) for value in values):
                        raise DatasetError(f"商品 {goods_id} 的价格、折扣或奖励数量不是有效数值")
                    if row.price < 0 or not 0 < row.cn_discount <= 1 or bundle.count <= 0:
                        raise DatasetError(f"商品 {goods_id} 的价格、折扣或奖励数量无效")
                    if goods_id in output["goods"]:
                        raise DatasetError(f"商品 {goods_id} 被多个地区重复引用")
                    output["categories"][row.goods_tag_id] = {
                        "names": cls.localized_names(
                            tag.tag_name, table_cfg_dir, f"分类 {row.goods_tag_id}"
                        ),
                        "sort_id": tag.sort_id,
                    }
                    output["items"][item_id] = {
                        "names": cls.localized_names(item.name, table_cfg_dir, f"物品 {item_id}"),
                        "icon_id": item.icon_id,
                    }
                    output["goods"][goods_id] = {
                        "region_id": region_id,
                        "shop_id": shop_id,
                        "category_id": row.goods_tag_id,
                        "item_id": item_id,
                        "count": bundle.count,
                        "price": row.price,
                        "discount": row.cn_discount,
                        "currency_id": row.money_id,
                        "currency_names": cls.localized_names(
                            currency.name, table_cfg_dir, f"币种 {row.money_id}"
                        ),
                        "refresh_type": row.limit_count_refresh_type,
                        "limit_count": row.limit_count,
                        "sort_id": row.sort_id,
                        "unlock_conditions": msgspec.to_builtins(row.unlock_conditions),
                        "channel_supply": supplies.get(goods_id, []),
                    }
                    region_goods.append(goods_id)
            if region_goods:
                output["regions"][region_id] = {
                    "names": cls.localized_names(
                        domain.domain_name, table_cfg_dir, f"地区 {region_id}"
                    ),
                    "shop_group_id": group_id,
                    "sort_id": domain.sort_id,
                    "goods_ids": region_goods,
                }
        if not output["goods"]:
            raise DatasetError("原始商店表没有周期补货商品")
        return output
