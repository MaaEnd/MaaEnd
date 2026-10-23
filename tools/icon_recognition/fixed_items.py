"""加载上游物品表未收录的固定物品。"""

from pathlib import Path
from typing import Any

import json5

FIXED_ITEMS_PATH = Path(__file__).with_name("fixed_items.json")
#: 固定物品必须自带的多语言名字（key 与物品表 ``names`` 一致）。
NAME_LANGUAGES = ("CN", "TC", "EN", "JP", "KR")


def load_fixed_items(path: str | Path = FIXED_ITEMS_PATH) -> dict[str, dict[str, Any]]:
    source = json5.loads(Path(path).read_text(encoding="utf-8"))
    if not isinstance(source, dict):
        raise ValueError("fixed_items.json 顶层必须是对象")
    result: dict[str, dict[str, Any]] = {}
    for item_id, payload in source.items():
        if not isinstance(item_id, str) or not isinstance(payload, dict):
            raise ValueError("fixed_items.json 必须使用物品 ID 到对象的映射")
        required = {
            "name",
            "names",
            "iconId",
            "rarity",
            "storageKind",
            "categoryType",
            "category",
        }
        missing = required - payload.keys()
        if missing:
            raise ValueError(f"{item_id} 缺少字段: {', '.join(sorted(missing))}")
        names = payload["names"]
        if not isinstance(names, dict) or any(
            not isinstance(names.get(language), str) or not names[language]
            for language in NAME_LANGUAGES
        ):
            raise ValueError(f"{item_id}.names 必须是 {', '.join(NAME_LANGUAGES)} 的非空映射")
        result[item_id] = dict(payload)
    return result


FIXED_ITEMS = load_fixed_items()
