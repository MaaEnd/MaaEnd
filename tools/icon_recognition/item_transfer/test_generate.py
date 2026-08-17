import json
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

import json5

from item_transfer import generate
from item_transfer.generate import (
    FORWARD_NODES,
    RETURN_NODES,
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

    def test_ctrl_click_uses_common_cross_platform_action(self) -> None:
        repo_root = Path(__file__).resolve().parents[3]
        pipeline = json5.loads(
            (repo_root / "assets/resource/pipeline/ItemTransfer.json").read_text(
                encoding="utf-8"
            )
        )
        ctrl_click_nodes = (
            "ItemTransferTransferForwardToBag",
            "ItemTransferTransferReturnToBag",
            "ItemTransferTransferForwardToRepo",
            "ItemTransferTransferReturnToRepo",
            "ItemTransferTransferToRepoReturn",
        )

        for node_name in ctrl_click_nodes:
            self.assertEqual(
                pipeline[node_name]["custom_action"],
                "AutoCtrlClickAction",
            )

    def test_ctrl_click_pipeline_nodes_generate_macos_key_mapping(self) -> None:
        repo_root = Path(__file__).resolve().parents[3]
        common_actions = json5.loads(
            (
                repo_root
                / "assets/resource/pipeline/Common/Private/AutoAltClick/Action.json"
            ).read_text(encoding="utf-8")
        )
        macos_keymap = json5.loads(
            (repo_root / "assets/resource_macos/pipeline/MacOSKeyMap.json").read_text(
                encoding="utf-8"
            )
        )

        expected_actions = {
            "__AutoCtrlClickCtrlKeyDownAction": ("KeyDown", 17),
            "__AutoCtrlClickCtrlKeyUpAction": ("KeyUp", 17),
            "__AutoCtrlClickMouseClickAction": ("Click", None),
        }
        for node_name, (action, key) in expected_actions.items():
            self.assertEqual(common_actions[node_name]["action"], action)
            if key is not None:
                self.assertEqual(common_actions[node_name]["key"], key)

        self.assertEqual(
            macos_keymap["__AutoCtrlClickCtrlKeyDownAction"]["action"]["param"]["key"],
            59,
        )
        self.assertEqual(
            macos_keymap["__AutoCtrlClickCtrlKeyUpAction"]["action"]["param"]["key"],
            59,
        )
        self.assertFalse(
            any(node_name.startswith("ItemTransferCtrlKey") for node_name in macos_keymap)
        )

    def test_ctrl_click_custom_action_is_registered_as_common_component(self) -> None:
        repo_root = Path(__file__).resolve().parents[3]
        schema = json.loads(
            (repo_root / "tools/schema/custom.action.schema.json").read_text(
                encoding="utf-8"
            )
        )
        custom_actions = schema["properties"]["custom_action"]["enum"]

        self.assertIn("AutoCtrlClickAction", custom_actions)
        self.assertNotIn("ItemTransferCtrlClickAction", custom_actions)

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

    def test_build_transfer_cases_uses_direction_specific_nodes(self) -> None:
        catalog = {
            "item_product": make_item("Product", -60, 1),
        }
        zh_cn = {
            "iconRecognition.name.item_product": "测试产物",
        }

        forward = build_transfer_cases(catalog, zh_cn, FORWARD_NODES)[0]
        backward = build_transfer_cases(catalog, zh_cn, RETURN_NODES)[0]

        self.assertEqual(forward["name"], "测试产物")
        self.assertEqual(forward["label"], "$iconRecognition.name.item_product")
        self.assertEqual(
            forward["pipeline_override"],
            {
                "ItemTransferClickForwardItemCategory": {
                    "template": "ItemTransfer/Product.png",
                },
                "ItemTransferFindForwardItemInRepo": self.item_id_override("item_product", "Normal:Product"),
                "ItemTransferFindForwardItemInBag": self.item_id_override("item_product", "Normal:Product"),
            },
        )
        self.assertEqual(
            backward["pipeline_override"],
            {
                "ItemTransferClickReturnItemCategory": {
                    "template": "ItemTransfer/Product.png",
                },
                "ItemTransferFindReturnItemInRepo": self.item_id_override("item_product", "Normal:Product"),
                "ItemTransferFindReturnItemInBag": self.item_id_override("item_product", "Normal:Product"),
            },
        )

    def test_build_transfer_cases_rejects_missing_zh_cn_name(self) -> None:
        catalog = {
            "item_product": make_item("Product", -81, 1),
        }

        with self.assertRaisesRegex(
            ValueError,
            r"missing zh_cn locale: iconRecognition\.name\.item_product",
        ):
            build_transfer_cases(catalog, {}, FORWARD_NODES)

    def test_update_item_transfer_task_only_replaces_cases(self) -> None:
        task = {
            "task": {"name": "ItemTransfer"},
            "option": {
                "WhatToTransfer": {
                    "type": "select",
                    "default_case": "旧物品",
                    "cases": [{"name": "旧物品"}],
                },
                "ReturnWhatToTransfer": {
                    "type": "select",
                    "default_case": "旧返程物品",
                    "cases": [{"name": "旧返程物品"}],
                },
                "TransferAll": {"type": "switch"},
            },
        }
        forward_cases = [{"name": "新物品"}]
        return_cases = [{"name": "新返程物品"}]

        self.assertEqual(
            update_item_transfer_task(task, forward_cases, return_cases),
            {
                "task": {"name": "ItemTransfer"},
                "option": {
                    "WhatToTransfer": {
                        "type": "select",
                        "default_case": "旧物品",
                        "cases": forward_cases,
                    },
                    "ReturnWhatToTransfer": {
                        "type": "select",
                        "default_case": "旧返程物品",
                        "cases": return_cases,
                    },
                    "TransferAll": {"type": "switch"},
                },
            },
        )
        self.assertEqual(task["option"]["WhatToTransfer"]["cases"], [{"name": "旧物品"}])
        self.assertEqual(
            task["option"]["ReturnWhatToTransfer"]["cases"], [{"name": "旧返程物品"}]
        )

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
                            "ReturnWhatToTransfer": {"cases": [{"name": "旧返程物品"}]},
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
            self.assertEqual(
                generated["option"]["ReturnWhatToTransfer"]["cases"][0]["name"], "测试产物"
            )
            self.assertEqual(generated["option"]["TransferAll"], {"type": "switch"})

    def test_tracked_task_matches_real_generated_output(self) -> None:
        repo_root = Path(__file__).resolve().parents[3]
        tracked_task_path = repo_root / "assets" / "tasks" / "ItemTransfer.json"

        with tempfile.TemporaryDirectory() as temp_dir:
            generated_task_path = Path(temp_dir) / "ItemTransfer.json"
            generated_task_path.write_bytes(tracked_task_path.read_bytes())
            generate_item_transfer_task(
                repo_root / "assets" / "data" / "IconRecognition" / "recognition_items.json",
                repo_root / "assets" / "locales" / "interface" / "zh_cn.json",
                generated_task_path,
            )

            self.assertEqual(
                json.loads(generated_task_path.read_text(encoding="utf-8")),
                json.loads(tracked_task_path.read_text(encoding="utf-8")),
            )

    def test_generated_resources_pass_maa_tools_check(self) -> None:
        repo_root = Path(__file__).resolve().parents[3]
        pnpm = "pnpm.cmd" if os.name == "nt" else "pnpm"
        subprocess.run([pnpm, "check"], cwd=repo_root, check=True)

    def test_generated_task_passes_schema_validation(self) -> None:
        repo_root = Path(__file__).resolve().parents[3]
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            resource_dir = root / "resource"
            task_dir = root / "tasks"
            resource_dir.mkdir()
            task_dir.mkdir()
            shutil.copy2(
                repo_root / "assets/tasks/ItemTransfer.json",
                task_dir / "ItemTransfer.json",
            )

            uv = "uv.exe" if os.name == "nt" else "uv"
            env = os.environ.copy()
            env["PYTHONIOENCODING"] = "utf-8"
            subprocess.run(
                [
                    uv,
                    "run",
                    "--frozen",
                    "--only-group",
                    "schema",
                    "tools/validate_schema.py",
                    "--resource-dirs",
                    str(resource_dir),
                    "--task-dirs",
                    str(task_dir),
                ],
                cwd=repo_root,
                check=True,
                env=env,
            )

    @staticmethod
    def item_id_override(item_id: str, item_filter: str) -> dict:
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

if __name__ == "__main__":
    unittest.main()
