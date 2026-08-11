import json
import tempfile
import unittest
from pathlib import Path

from item_transfer import generate
from item_transfer.generate import (
    build_transfer_cases,
    generate_item_transfer_task,
    select_transfer_items,
    update_item_transfer_task,
)


def make_item(category_type: str, sort_id1: int, sort_id2: int, storage_kind: str = "Normal") -> dict:
    return {
        "storageKind": storage_kind,
        "categoryType": category_type,
        "sortId1": sort_id1,
        "sortId2": sort_id2,
    }


class ItemTransferGeneratorTest(unittest.TestCase):
    def test_category_type_order_covers_future_transfer_categories(self) -> None:
        self.assertEqual(
            getattr(generate, "CATEGORY_TYPE_ORDER", None),
            (
                "Ore",
                "Plant",
                "Product",
                "Doodad",
                "Nurturance",
                "Usable",
                "Producer",
                "PortableDevice",
            ),
        )

    def test_task_overrides_supply_complete_icon_recognition_param(self) -> None:
        repo_root = Path(__file__).resolve().parents[3]
        pipeline = json.loads(
            (repo_root / "assets" / "resource" / "pipeline" / "ItemTransfer.json").read_text(
                encoding="utf-8"
            )
        )
        task = json.loads(
            (repo_root / "assets" / "tasks" / "ItemTransfer.json").read_text(encoding="utf-8")
        )
        node_names = (
            "ItemTransferFindItemInRepo",
            "ItemTransferFindItemInBag",
            "ItemTransferFindItemInBagReturn",
        )

        for node_name in node_names:
            custom_param = pipeline[node_name]["recognition"]["param"]["custom_recognition_param"]
            self.assertEqual(custom_param, {"grid_type": "transfer"})
            self.assertNotIn("item_ids", custom_param)

        for case in task["option"]["WhatToTransfer"]["cases"]:
            for node_name in node_names:
                custom_param = case["pipeline_override"][node_name]["recognition"]["param"][
                    "custom_recognition_param"
                ]
                self.assertEqual(custom_param["grid_type"], "transfer")
                self.assertEqual(len(custom_param["item_ids"]), 1)

    def test_pipeline_starts_directly_in_dijiang_without_camera_adjustment(self) -> None:
        pipeline = self.load_item_transfer_pipeline()

        self.assertEqual(
            pipeline["ItemTransferStart"]["next"],
            [
                "ItemTransferStartOpenRepo",
                "[JumpBack]SceneEnterWorldDijiang2",
            ],
        )
        self.assertNotIn("ItemTransferStartAtReceptionRoom", pipeline)
        self.assertNotIn("ItemTransferStartMoveCamera", pipeline)
        self.assertNotIn("ItemTransferStartMoveCamera2", pipeline)

    def test_icon_recognition_waits_for_mouse_reset_and_stable_grid(self) -> None:
        pipeline = self.load_item_transfer_pipeline()
        expected_rois = {
            "ItemTransferFindItemInRepo": [154, 202, 585, 291],
            "ItemTransferFindItemInBag": [739, 202, 398, 291],
            "ItemTransferFindItemInBagReturn": [739, 202, 398, 291],
        }
        mouse_reset = "[JumpBack][Anchor]MouseMoveResetAnchor"

        for node_name, roi in expected_rois.items():
            self.assertEqual(
                pipeline[node_name]["pre_wait_freezes"],
                {"time": 400, "target": roi},
            )

            incoming_nodes = [
                node
                for node in pipeline.values()
                if node_name in node.get("next", [])
            ]
            self.assertTrue(incoming_nodes)
            for incoming_node in incoming_nodes:
                next_nodes = incoming_node["next"]
                index = next_nodes.index(node_name)
                self.assertGreater(index, 0)
                self.assertEqual(next_nodes[index - 1], mouse_reset)
                self.assertEqual(
                    incoming_node["anchor"]["MouseMoveResetAnchor"],
                    "MouseMoveReset",
                )

    def test_select_transfer_items_filters_categories_and_ore_allowlist(self) -> None:
        catalog = {
            "item_copper_ore": make_item("Ore", -80, 1),
            "item_unlisted_ore": make_item("Ore", -80, 2),
            "item_product": make_item("Product", -81, 1),
            "item_doodad": make_item("Doodad", -70, 1),
            "item_valuable": make_item("Nurturance", -60, 1, "ValuableDepot"),
        }

        self.assertEqual(
            [item["id"] for item in select_transfer_items(catalog)],
            ["item_copper_ore", "item_product"],
        )

    def test_select_transfer_items_sorts_by_sort_ids_and_id_descending(self) -> None:
        catalog = {
            "item_a": make_item("Product", -81, 1),
            "item_b": make_item("Product", -60, 1),
            "item_c": make_item("Product", -60, 2),
            "item_d": make_item("Product", -60, 2),
        }

        self.assertEqual(
            [item["id"] for item in select_transfer_items(catalog)],
            ["item_d", "item_c", "item_b", "item_a"],
        )

    def test_select_transfer_items_sorts_by_category_before_sort_ids(self) -> None:
        catalog = {
            "item_usable": make_item("Usable", 100, 1),
            "item_nurturance": make_item("Nurturance", 90, 1),
            "item_product_a": make_item("Product", -81, 1),
            "item_product_b": make_item("Product", -60, 1),
            "item_plant": make_item("Plant", 1000, 1),
            "item_copper_ore": make_item("Ore", -1000, 1),
        }

        self.assertEqual(
            [item["id"] for item in select_transfer_items(catalog)],
            [
                "item_copper_ore",
                "item_plant",
                "item_product_b",
                "item_product_a",
                "item_nurturance",
                "item_usable",
            ],
        )

    def test_build_transfer_cases_uses_localized_name_template_and_item_id(self) -> None:
        catalog = {
            "item_nurturance": make_item("Nurturance", -60, 1),
        }
        zh_cn = {
            "iconRecognition.name.item_nurturance": "培养素材",
        }

        self.assertEqual(
            build_transfer_cases(catalog, zh_cn),
            [
                {
                    "name": "培养素材",
                    "label": "$iconRecognition.name.item_nurturance",
                    "pipeline_override": {
                        "ItemTransferClickItemCategory": {
                            "template": "ItemTransfer/Nurturance.png",
                        },
                        "ItemTransferFindItemInRepo": self.item_id_override("item_nurturance"),
                        "ItemTransferFindItemInBag": self.item_id_override("item_nurturance"),
                        "ItemTransferFindItemInBagReturn": self.item_id_override("item_nurturance"),
                    },
                },
            ],
        )

    def test_build_transfer_cases_rejects_missing_zh_cn_name(self) -> None:
        catalog = {
            "item_product": make_item("Product", -81, 1),
        }

        with self.assertRaisesRegex(
            ValueError,
            r"missing zh_cn locale: iconRecognition\.name\.item_product",
        ):
            build_transfer_cases(catalog, {})

    def test_update_item_transfer_task_only_replaces_cases(self) -> None:
        task = {
            "task": {"name": "ItemTransfer"},
            "option": {
                "WhatToTransfer": {
                    "type": "select",
                    "default_case": "旧物品",
                    "cases": [{"name": "旧物品"}],
                },
                "TransferAll": {"type": "switch"},
            },
        }
        cases = [{"name": "新物品"}]

        self.assertEqual(
            update_item_transfer_task(task, cases),
            {
                "task": {"name": "ItemTransfer"},
                "option": {
                    "WhatToTransfer": {
                        "type": "select",
                        "default_case": "旧物品",
                        "cases": cases,
                    },
                    "TransferAll": {"type": "switch"},
                },
            },
        )
        self.assertEqual(task["option"]["WhatToTransfer"]["cases"], [{"name": "旧物品"}])

    def test_generate_item_transfer_task_reads_sources_and_writes_task(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            catalog_path = root / "recognition_items.json"
            locale_path = root / "zh_cn.json"
            task_path = root / "ItemTransfer.json"
            catalog_path.write_text(
                json.dumps({"item_product": make_item("Product", -81, 1)}),
                encoding="utf-8",
            )
            locale_path.write_text(
                json.dumps({"iconRecognition.name.item_product": "测试产物"}, ensure_ascii=False),
                encoding="utf-8",
            )
            task_path.write_text(
                json.dumps(
                    {
                        "task": {"name": "ItemTransfer"},
                        "option": {
                            "WhatToTransfer": {"cases": [{"name": "旧物品"}]},
                            "TransferAll": {"type": "switch"},
                        },
                    },
                    ensure_ascii=False,
                ),
                encoding="utf-8",
            )

            case_count = generate_item_transfer_task(catalog_path, locale_path, task_path)

            generated = json.loads(task_path.read_text(encoding="utf-8"))
            self.assertEqual(case_count, 1)
            self.assertEqual(generated["option"]["WhatToTransfer"]["cases"][0]["name"], "测试产物")
            self.assertEqual(generated["option"]["TransferAll"], {"type": "switch"})

    @staticmethod
    def item_id_override(item_id: str) -> dict:
        return {
            "recognition": {
                "param": {
                    "custom_recognition_param": {
                        "grid_type": "transfer",
                        "item_ids": [item_id],
                    },
                },
            },
        }

    @staticmethod
    def load_item_transfer_pipeline() -> dict:
        repo_root = Path(__file__).resolve().parents[3]
        return json.loads(
            (repo_root / "assets" / "resource" / "pipeline" / "ItemTransfer.json").read_text(
                encoding="utf-8"
            )
        )


if __name__ == "__main__":
    unittest.main()
