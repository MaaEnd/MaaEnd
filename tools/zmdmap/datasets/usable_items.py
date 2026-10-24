"""主动可用道具表（``usable_items.json``）。"""

from pathlib import Path
from typing import Any

import msgspec

from ..utils.tables import load_table
from .base import Dataset, DatasetError


class UseItemRow(msgspec.Struct, frozen=True, rename="camel"):
    """``UseItemTable`` 中区分持续增幅道具的字段。"""

    effect_type: str | int = ""
    ui_type: str | int = ""
    is_persistent_buff: bool = False


class ItemNameRow(msgspec.Struct, frozen=True):
    """``ItemTable.name`` 中的中英文名称。"""

    cn: str = ""
    en: str = ""


class ItemDisplayRow(msgspec.Struct, frozen=True, rename="camel"):
    """生成下拉列表所需的物品显示字段。"""

    name: ItemNameRow = ItemNameRow()
    icon_id: str = ""
    rarity: str | int = ""


class UsableItems(Dataset):
    """只包含可主动使用的持续增幅道具，不收被动回复与其他消耗品。"""

    @classmethod
    def build(cls, table_cfg_dir: Path, json_data_dir: Path) -> dict[str, Any]:
        use_items = load_table(table_cfg_dir, "UseItemTable", UseItemRow)
        items = load_table(table_cfg_dir, "ItemTable", ItemDisplayRow)

        output: dict[str, dict[str, Any]] = {}
        for item_id, use_item in sorted(use_items.items()):
            if not (
                use_item.is_persistent_buff
                and str(use_item.effect_type) == "2"
                and str(use_item.ui_type) == "3"
            ):
                continue

            item = items.get(item_id)
            if item is None:
                raise DatasetError(f"UseItemTable 中的主动增幅道具 {item_id} 不在 ItemTable 中")
            if not item.name.cn or not item.name.en or not item.icon_id:
                raise DatasetError(f"主动增幅道具 {item_id} 缺少中英文名称或图标")

            output[item_id] = {
                "name": item.name.cn,
                "names": {"CN": item.name.cn, "EN": item.name.en},
                "category": "ActiveBuff",
                "iconId": item.icon_id,
                "rarity": str(item.rarity),
            }

        return {"items": output}
