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

    def test_transfer_uses_common_inventory_action(self) -> None:
        repo_root = Path(__file__).resolve().parents[3]
        pipeline = json5.loads(
            (repo_root / "assets/resource/pipeline/ItemTransfer.json").read_text(
                encoding="utf-8"
            )
        )
        transfer_nodes = (
            "ItemTransferTransferForwardToBag",
            "ItemTransferTransferReturnToBag",
            "ItemTransferTransferForwardToRepo",
            "ItemTransferTransferReturnToRepo",
            "ItemTransferTransferToRepoReturn",
        )

        for node_name in transfer_nodes:
            self.assertEqual(
                pipeline[node_name]["custom_action"],
                "InventoryTransferAllAction",
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
        schema = json5.loads(
            (repo_root / "tools/schema/custom.action.schema.json").read_text(
                encoding="utf-8"
            )
        )
        custom_actions = schema["properties"]["custom_action"]["enum"]

        self.assertIn("AutoCtrlClickAction", custom_actions)
        self.assertNotIn("ItemTransferCtrlClickAction", custom_actions)

    def test_inventory_transfer_platform_contract(self) -> None:
        repo_root = Path(__file__).resolve().parents[3]

        def read_json(relative_path: str) -> dict:
            return json5.loads((repo_root / relative_path).read_text(encoding="utf-8"))

        desktop = read_json("assets/resource/pipeline/Common/Private/Inventory/Action.json")
        adb = read_json("assets/resource_adb/pipeline/Common/Private/Inventory/Action.json")
        macos = read_json("assets/resource_macos/pipeline/MacOSKeyMap.json")
        schema = read_json("tools/schema/custom.action.schema.json")
        register = (repo_root / "agent/go-service/common/inventory/register.go").read_text(
            encoding="utf-8"
        )
        for mode, win_key, mac_key in (("All", 17, 59), ("Stack", 16, 56), ("Half", 18, 58)):
            with self.subTest(mode=mode):
                action = f"InventoryTransfer{mode}Action"
                self.assertIn(action, schema["properties"]["custom_action"]["enum"])
                self.assertIn(f'"{action}"', register)
                prefix = f"__InventoryTransfer{mode}"
                for stage, action_type in (("Begin", "KeyDown"), ("End", "KeyUp")):
                    node = f"{prefix}{stage}Action"
                    self.assertEqual(desktop[node]["action"], action_type)
                    self.assertEqual(desktop[node]["key"], win_key)
                    self.assertEqual(macos[node]["action"]["param"]["key"], mac_key)
                self.assertNotIn(f"{prefix}ExecuteAction", desktop)
                self.assertNotIn(f"{prefix}ExecuteAction", adb)
                # 完整手势负责触点清理，外层不能继承桌面端的按键动作。
                self.assertEqual(adb[f"{prefix}BeginAction"]["action"], "DoNothing")
                self.assertEqual(adb[f"{prefix}EndAction"]["action"], "DoNothing")

        click_node = "__InventoryTransferClickAction"
        self.assertEqual(
            [name for name, node in desktop.items() if node.get("action") == "Click"],
            [click_node],
        )
        self.assertEqual(desktop[click_node]["pre_delay"], 400)
        self.assertEqual(desktop[click_node]["post_delay"], 100)
        execute = adb[click_node]
        self.assertEqual(execute["action"], "Custom")
        self.assertEqual(execute["custom_action"], "InventoryTransferTouchAction")
        self.assertEqual(execute["custom_action_param"], {"mode": "stack"})
        self.assertEqual(execute["pre_delay"], 0)
        self.assertEqual(execute["post_delay"], 0)

        # feat 分支的背包流程也必须接入公共动作，避免只迁移库存转移而漏掉旧 Shift 调用。
        stash = read_json("assets/resource/pipeline/StashBackpack.json")
        retrieve = read_json("assets/resource/pipeline/StashBackpack/Retrieve.json")
        for pipeline, name, target in (
            (stash, "StashBackpackManualStoreItem", "StashBackpackBagPageItem"),
            (stash, "StoreNewItemsWithStashBackpackStoreItem", "StashBackpackBagPageItem"),
            (retrieve, "RetrieveBackpackStoreNewItem", "StashBackpackBagPageItem"),
            (retrieve, "RetrieveBackpackMoveItemToBag", "StashBackpackFindCurrentItemInRepo"),
        ):
            with self.subTest(node=name):
                self.assertEqual(pipeline[name]["custom_action"], "InventoryTransferStackAction")
                self.assertEqual(pipeline[name]["target"], target)
                self.assertEqual(pipeline[name]["target_offset"], [26, 25, -52, -50])

        for node, contact in (("__InventoryTransferSourceTouchDown", 0), ("__InventoryTransferButtonTouchDown", 1)):
            self.assertEqual(adb[node]["action"], "TouchDown")
            self.assertEqual(adb[node]["contact"], contact)
            self.assertEqual(adb[node]["pressure"], 1)
        recognition = read_json("assets/resource_adb/pipeline/Common/Private/Inventory/Recognition.json")
        for mode in ("All", "Stack", "Half"):
            for side, x in (("Left", 150), ("Right", 1030)):
                node = recognition[f"__InventoryTransfer{mode}Button{side}"]
                self.assertEqual(node["recognition"], "TemplateMatch")
                self.assertEqual(node["roi"], [x, 175, 100, 370])
                self.assertEqual(node["method"], 5)
                self.assertEqual(node["threshold"], 0.85)
                self.assertEqual(node["template"], f"Common/Inventory/Transfer{mode}.png")
                self.assertTrue((repo_root / "assets/resource_adb/image" / node["template"]).is_file())

    def test_stash_backpack_adb_scroll_overrides(self) -> None:
        repo_root = Path(__file__).resolve().parents[3]

        def read_json(path: str) -> dict:
            return json5.loads((repo_root / path).read_text(encoding="utf-8"))

        desktop = read_json("assets/resource/pipeline/Common/Private/Inventory/Scroll.json")
        adb = read_json("assets/resource_adb/pipeline/Common/Private/Inventory/Scroll.json")
        expected = {
            f"Inventory{side}Scroll{direction}"
            for side in ("Bag", "Repo") for direction in ("Upward", "Downward")
        }
        self.assertEqual(set(desktop), expected)
        self.assertEqual(set(adb), expected)
        for name, source in desktop.items():
            with self.subTest(node=name):
                self.assertEqual(source["action"], "Scroll")
                override = adb[name]
                self.assertEqual(override["action"], "Swipe")
                self.assertNotIn("next", source)
                begin, end = override["begin"], override["end"]
                self.assertEqual(begin[0], end[0])
                self.assertGreater((end[1] - begin[1]) * source["dy"], 0)
                self.assertLess(abs(end[1] - begin[1]), 342)

        search = read_json("assets/resource/pipeline/StashBackpack/Search.json")
        overrides = read_json("assets/resource_adb/pipeline/StashBackpack/Search.json")
        for name, node in search.items():
            with self.subTest(node=name):
                self.assertNotEqual(node.get("action"), "Scroll")
                if "MouseMoveResetAnchor" in node.get("anchor", {}):
                    self.assertEqual(node["anchor"]["MouseMoveResetAnchor"], "MouseMoveReset")
                if node.get("custom_action") == "SubTask":
                    self.assertEqual(len(node["custom_action_param"]["sub"]), 1)
                    self.assertIn(node["custom_action_param"]["sub"][0], expected)
                # 平台覆盖不能丢掉翻页计数、边界状态、退出分支和命中上限。
                self.assertLessEqual(set(overrides.get(name, {})), {
                    "roi", "recognition", "custom_recognition_param", "pre_wait_freezes", "post_wait_freezes",
                })

        snapshot = read_json("assets/resource/pipeline/StashBackpack/Snapshot.json")
        prepare = snapshot["__StashBackpackSnapshotPrepareRecognitionStep"]
        self.assertEqual(prepare["custom_action"], "SubTask")
        self.assertEqual(prepare["custom_action_param"]["sub"], ["MouseMoveReset"])
        self.assertNotIn("StashBackpackMouseMoveReset", snapshot)
        self.assertEqual(
            snapshot["__StashBackpackSnapshotItemRecognition"]["custom_recognition_param"],
            {"grid_type": "transfer", "item_filters": ["Normal:*"], "debug": True},
        )
        snapshot_adb = read_json("assets/resource_adb/pipeline/StashBackpack/Snapshot.json")
        self.assertEqual(snapshot_adb["__StashBackpackSnapshotPrepareRecognitionStep"]["action"], "DoNothing")
        self.assertNotIn("StashBackpackMouseMoveReset", snapshot_adb)
        self.assertEqual(
            snapshot_adb["__StashBackpackSnapshotScrollbarRecognition"]["roi"],
            overrides["StashBackpackBagBatchBottomReached"]["roi"],
        )
        task = read_json("assets/tasks/StashBackpack.json")
        self.assertEqual(task["task"][0]["controller"], ["Win32-Front", "ADB", "CloudADB"])
        replenish = read_json("assets/resource_adb/pipeline/StashBackpack.json")["StashBackpackReplenishDragItem"]
        self.assertEqual(replenish["custom_action"], "InventoryDragTouchAction")
        self.assertEqual(replenish["target"], "StashBackpackFindCurrentItemInRepo")
        self.assertEqual(replenish["custom_action_param"]["end"], "StashBackpackFindCurrentItemInBag")
        self.assertEqual(snapshot_adb["__StashBackpackSnapshotItemRecognition"]["roi"], [780, 160, 470, 370])
        self.assertEqual(overrides["StashBackpackFindCurrentItemInRepo"]["recognition"]["param"]["roi"], [30, 160, 710, 370])

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

    def test_transfer_case_order_does_not_depend_on_catalog_order(self) -> None:
        items = {
            "item_a": make_item("Product", -81, 1),
            "item_b": make_item("Product", -60, 1),
            "item_c": make_item("Nurturance", 90, 1),
        }
        zh_cn = {
            f"iconRecognition.name.{item_id}": item_id
            for item_id in items
        }

        forward = build_transfer_cases(items, zh_cn, FORWARD_NODES)
        reversed_forward = build_transfer_cases(
            dict(reversed(list(items.items()))),
            zh_cn,
            FORWARD_NODES,
        )

        self.assertEqual(reversed_forward, forward)

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
                "// catalog JSONC\n"
                + json.dumps({"item_product": make_item("Product", -81, 1)}),
                encoding="utf-8",
            )
            locale_path.write_text(
                "// locale JSONC\n"
                + json.dumps({"iconRecognition.name.item_product": "测试产物"}, ensure_ascii=False),
                encoding="utf-8",
            )
            task_path.write_text(
                "// task JSONC\n"
                + json.dumps(
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
                json5.loads(tracked_task_path.read_text(encoding="utf-8")),
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
                        "item_recheck_filters": [item_filter],
                        "deduplicate": True,
                    },
                },
            },
        }

if __name__ == "__main__":
    unittest.main()
