"""加载上游物品表未收录的固定物品。"""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any


FIXED_ITEMS_PATH = Path(__file__).with_name("fixed_items.json")


def load_fixed_items(path: str | Path = FIXED_ITEMS_PATH) -> dict[str, dict[str, Any]]:
    source = json.loads(Path(path).read_text(encoding="utf-8"))
    if not isinstance(source, dict):
        raise ValueError("fixed_items.json 顶层必须是对象")
    result: dict[str, dict[str, Any]] = {}
    for item_id, payload in source.items():
        if not isinstance(item_id, str) or not isinstance(payload, dict):
            raise ValueError("fixed_items.json 必须使用物品 ID 到对象的映射")
        required = {
            "name",
            "iconId",
            "i18nKey",
            "rarity",
            "storageKind",
            "categoryType",
            "category",
        }
        missing = required - payload.keys()
        if missing:
            raise ValueError(f"{item_id} 缺少字段: {', '.join(sorted(missing))}")
        result[item_id] = dict(payload)
    return result


FIXED_ITEMS = load_fixed_items()
FIXED_NAME_KEYS = {
    item_id: payload["i18nKey"] for item_id, payload in FIXED_ITEMS.items()
}
