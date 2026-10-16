"""稳定需求物资：地区商店中的周期补货商品及其五语名称。

DomainDataTable -> ShopGroupTable -> ShopTable -> ShopGoodsTable ->
RewardTable -> ItemTable；分类来自 ShopGoodsTagTable。渠道等级的供货量
来自 ShopChannelDevelopmentTable，不把同一物品的不同售卖档合并成一个价格。
只读 TableCfg，不依赖其他精简数据、网站页面或人工商品名单。
"""

import math
from pathlib import Path
from typing import Any

from ..utils.game_data import read_table_cfg
from .base import Dataset, DatasetError


def _entry(table: dict, key: str, label: str) -> dict:
    if key not in table:
        raise DatasetError(f"{label} 引用了不存在的 {key}")
    return table[key]


class StockStaple(Dataset):
    """常规地区商店的可补货商品（不含一次性商品和弹性需求物资）。"""

    requires_json_data = False

    @classmethod
    def build(cls, table_cfg_dir: Path, json_data_dir: Path) -> dict[str, Any]:
        domains = read_table_cfg(table_cfg_dir, "DomainDataTable")
        groups = read_table_cfg(table_cfg_dir, "ShopGroupTable")
        shops = read_table_cfg(table_cfg_dir, "ShopTable")
        goods = read_table_cfg(table_cfg_dir, "ShopGoodsTable")
        rewards = read_table_cfg(table_cfg_dir, "RewardTable")
        items = read_table_cfg(table_cfg_dir, "ItemTable")
        tags = read_table_cfg(table_cfg_dir, "ShopGoodsTagTable")
        channels = read_table_cfg(table_cfg_dir, "ShopChannelDevelopmentTable")

        supplies: dict[str, list[dict]] = {}
        for channel_id, channel in sorted(channels.items()):
            for level, row in sorted(channel.channelLevelMap.items(), key=lambda x: int(x[0])):
                if len(row.newGoodsList) != len(row.newGoodsNum):
                    raise DatasetError(f"渠道 {channel_id}/{level} 商品与数量不对应")
                for goods_id, count in zip(row.newGoodsList, row.newGoodsNum, strict=True):
                    supplies.setdefault(goods_id, []).append({
                        "channel_id": channel_id,
                        "level": int(level),
                        "count": count,
                        "version_start": row.versionStart,
                        "time_id": row.timeId,
                    })

        output: dict[str, dict] = {"regions": {}, "categories": {}, "items": {}, "goods": {}}
        for region_id, domain in sorted(domains.items(), key=lambda x: (x[1].sortId, x[0])):
            group_id = domain.domainShopGroupId
            if not group_id:
                continue
            group = _entry(groups, group_id, f"地区 {region_id}")
            region_goods: list[str] = []
            for shop_id in group.shopIds:
                shop = _entry(shops, shop_id, f"商店组 {group_id}")
                # TableCfg 的枚举可以序列化为名字或整数；None=0，RefreshRandom=2。
                if shop.shopRefreshType not in ("None", 0):
                    continue
                for goods_id in shop.shopGoodsIds:
                    row = _entry(goods, goods_id, f"商店 {shop_id}")
                    if row.shopId != shop_id:
                        raise DatasetError(f"商品 {goods_id} 的 shopId 与商店目录不一致")
                    if row.limitCountRefreshType in ("None", 0):
                        continue
                    reward = _entry(rewards, row.rewardId, f"商品 {goods_id}")
                    if reward.probItemBundles or len(reward.itemBundles) != 1:
                        raise DatasetError(f"商品 {goods_id} 不是单物品固定奖励")
                    bundle = reward.itemBundles[0]
                    item_id = bundle.id
                    item = _entry(items, item_id, f"奖励 {row.rewardId}")
                    tag = _entry(tags, row.goodsTagId, f"商品 {goods_id}")
                    currency = _entry(items, row.moneyId, f"商品 {goods_id} 币种")
                    values = (row.price, row.cnDiscount, bundle.count)
                    if any(not isinstance(v, (int, float)) or isinstance(v, bool) or not math.isfinite(v) for v in values):
                        raise DatasetError(f"商品 {goods_id} 的价格、折扣或奖励数量不是有效数值")
                    if row.price < 0 or not 0 < row.cnDiscount <= 1 or bundle.count <= 0:
                        raise DatasetError(f"商品 {goods_id} 的价格、折扣或奖励数量无效")
                    if goods_id in output["goods"]:
                        raise DatasetError(f"商品 {goods_id} 被多个地区重复引用")
                    output["categories"][row.goodsTagId] = {
                        "names": cls.localized_names(tag.tagName, table_cfg_dir, f"分类 {row.goodsTagId}"),
                        "sort_id": tag.sortId,
                    }
                    output["items"][item_id] = {
                        "names": cls.localized_names(item.name, table_cfg_dir, f"物品 {item_id}"),
                        "icon_id": item.iconId,
                    }
                    output["goods"][goods_id] = {
                        "region_id": region_id,
                        "shop_id": shop_id,
                        "category_id": row.goodsTagId,
                        "item_id": item_id,
                        "count": bundle.count,
                        "price": row.price,
                        "discount": row.cnDiscount,
                        "currency_id": row.moneyId,
                        "currency_names": cls.localized_names(currency.name, table_cfg_dir, f"币种 {row.moneyId}"),
                        "refresh_type": row.limitCountRefreshType,
                        "limit_count": row.limitCount,
                        "sort_id": row.sortId,
                        "unlock_conditions": row.unlockConditions,
                        "channel_supply": supplies.get(goods_id, []),
                    }
                    region_goods.append(goods_id)
            if region_goods:
                output["regions"][region_id] = {
                    "names": cls.localized_names(domain.domainName, table_cfg_dir, f"地区 {region_id}"),
                    "shop_group_id": group_id,
                    "sort_id": domain.sortId,
                    "goods_ids": region_goods,
                }
        if not output["goods"]:
            raise DatasetError("原始商店表没有周期补货商品")
        return output
