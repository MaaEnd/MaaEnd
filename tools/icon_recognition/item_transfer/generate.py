"""从 IconRecognition catalog 生成库存转移任务选项。"""

import json
from copy import deepcopy
from dataclasses import dataclass
from pathlib import Path


CATEGORY_TYPE_ORDER = (
    "Ore",
    "Plant",
    "Product",
    "Doodad",
    "Nurturance",
    "Usable",
    "Producer",
    "PortableDevice",
)
CATEGORY_TYPE_INDEX = {
    category_type: index for index, category_type in enumerate(CATEGORY_TYPE_ORDER)
}
ALLOWED_CATEGORY_TYPES = {"Ore", "Plant", "Product", "Nurturance", "Usable"}
ORE_ALLOWLIST = {
    "item_copper_ore",
    "item_iron_ore",
    "item_quartz_sand",
    "item_originium_ore",
}


@dataclass(frozen=True)
class TransferNodes:
    category_node: str
    repo_find_node: str
    bag_find_node: str


FORWARD_NODES = TransferNodes(
    category_node="ItemTransferClickForwardItemCategory",
    repo_find_node="ItemTransferFindForwardItemInRepo",
    bag_find_node="ItemTransferFindForwardItemInBag",
)
RETURN_NODES = TransferNodes(
    category_node="ItemTransferClickReturnItemCategory",
    repo_find_node="ItemTransferFindReturnItemInRepo",
    bag_find_node="ItemTransferFindReturnItemInBag",
)


def select_transfer_items(catalog: dict) -> list[dict]:
    selected = []
    for item_id, item in catalog.items():
        if item.get("storageKind") != "Normal":
            continue
        category_type = item.get("categoryType")
        if category_type not in ALLOWED_CATEGORY_TYPES:
            continue
        if category_type == "Ore" and item_id not in ORE_ALLOWLIST:
            continue
        selected.append({"id": item_id, **item})

    # Python 排序稳定，按分类重排时会保留分类内部的三字段降序结果。
    selected.sort(
        key=lambda item: (item["sortId1"], item["sortId2"], item["id"]),
        reverse=True,
    )
    return sorted(
        selected,
        key=lambda item: CATEGORY_TYPE_INDEX[item["categoryType"]],
    )


def _item_id_override(item_id: str, item_filter: str) -> dict:
    return {
        "recognition": {
            "param": {
                "custom_recognition_param": {
                    "grid_type": "transfer",
                    "item_ids": [item_id],
                    "item_filters": [item_filter],
                },
            },
        },
    }


def build_transfer_cases(catalog: dict, zh_cn: dict, nodes: TransferNodes) -> list[dict]:
    cases = []
    for item in select_transfer_items(catalog):
        locale_key = f"iconRecognition.name.{item['id']}"
        name = zh_cn.get(locale_key)
        if not isinstance(name, str) or not name:
            raise ValueError(f"missing zh_cn locale: {locale_key}")

        cases.append(
            {
                "name": name,
                "label": f"${locale_key}",
                "pipeline_override": {
                    nodes.category_node: {
                        "template": f"ItemTransfer/{item['categoryType']}.png",
                    },
                    nodes.repo_find_node: _item_id_override(
                        item["id"],
                        f"{item['storageKind']}:{item['categoryType']}",
                    ),
                    nodes.bag_find_node: _item_id_override(
                        item["id"],
                        f"{item['storageKind']}:{item['categoryType']}",
                    ),
                },
            }
        )
    return cases


def update_item_transfer_task(
    task: dict,
    forward_cases: list[dict],
    return_cases: list[dict],
) -> dict:
    updated = deepcopy(task)
    updated["option"]["WhatToTransfer"]["cases"] = forward_cases
    updated["option"]["ReturnWhatToTransfer"]["cases"] = return_cases
    return updated


def generate_item_transfer_task(catalog_path: Path, locale_path: Path, task_path: Path) -> int:
    catalog = json.loads(catalog_path.read_text(encoding="utf-8"))
    zh_cn = json.loads(locale_path.read_text(encoding="utf-8"))
    task = json.loads(task_path.read_text(encoding="utf-8"))

    forward_cases = build_transfer_cases(catalog, zh_cn, FORWARD_NODES)
    return_cases = build_transfer_cases(catalog, zh_cn, RETURN_NODES)
    updated = update_item_transfer_task(task, forward_cases, return_cases)
    task_path.write_text(
        json.dumps(updated, ensure_ascii=False, indent=4) + "\n",
        encoding="utf-8",
    )
    return len(forward_cases)


if __name__ == "__main__":
    repo_root = Path(__file__).resolve().parents[3]
    count = generate_item_transfer_task(
        repo_root / "assets" / "data" / "IconRecognition" / "recognition_items.json",
        repo_root / "assets" / "locales" / "interface" / "zh_cn.json",
        repo_root / "assets" / "tasks" / "ItemTransfer.json",
    )
    print(f"generated {count} ItemTransfer cases")
