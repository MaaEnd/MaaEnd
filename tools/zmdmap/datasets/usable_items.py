"""主动持续增幅道具表（``usable_items.json``），只读取本地 TableCfg。

``UseItemTable`` 的 ``Buff`` / ``Alive`` 与 ``isPersistentBuff`` 一起区分主动增幅道具，
排除治疗、复活和被动触发的回复道具。物品名称按 ``ItemTable.name`` 的文本引用查询五语文本表，
图标与稀有度沿用物品表。
"""

from typing import Any

import msgspec

from ..utils.inputs import TableCfg
from ..utils.log import logger
from ..utils.tables import ItemRow
from .base import Dataset, DatasetError


class UseItemRow(msgspec.Struct, frozen=True, rename="camel"):
    """``UseItemTable`` 中区分持续增幅道具的字段。"""

    effect_type: str = ""
    ui_type: str = ""
    is_persistent_buff: bool = False


class UsableItems(Dataset):
    """只包含可主动使用的持续增幅道具，不收被动回复与其他消耗品。"""

    @classmethod
    def build(cls, table_cfg: TableCfg) -> dict[str, Any]:
        use_items = table_cfg.table("UseItemTable", UseItemRow)
        items = table_cfg.table("ItemTable", ItemRow)

        output: dict[str, dict[str, Any]] = {}
        for item_id, use_item in sorted(use_items.items()):
            if not (use_item.is_persistent_buff and use_item.effect_type == "Buff" and use_item.ui_type == "Alive"):
                continue

            item = items.get(item_id)
            if item is None:
                raise DatasetError(f"UseItemTable 中的主动增幅道具 {item_id} 不在 ItemTable 中")
            if not item.icon_id:
                raise DatasetError(f"主动增幅道具 {item_id} 缺少图标")
            names = cls.plain_names(table_cfg.ref_texts(item.name), f"主动增幅道具 {item_id}")

            output[item_id] = {
                "name": names["CN"],
                "names": names,
                "category": "ActiveBuff",
                "iconId": item.icon_id,
                "rarity": item.rarity,
            }

        if not output:
            raise DatasetError("UseItemTable 中没有可主动使用的持续增幅道具")
        logger.info(f"主动持续增幅道具 {len(output)} 条")
        return {"items": output}
