import json
import tempfile
import unittest
from pathlib import Path

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

    def test_pipeline_leaves_item_ids_to_direction_options(self) -> None:
        repo_root = Path(__file__).resolve().parents[3]
        pipeline = json.loads(
            (repo_root / "assets" / "resource" / "pipeline" / "ItemTransfer.json").read_text(
                encoding="utf-8"
            )
        )
        node_names = (
            "ItemTransferFindForwardItemInRepo",
            "ItemTransferFindReturnItemInRepo",
            "ItemTransferFindForwardItemInBag",
            "ItemTransferFindReturnItemInBag",
        )

        for node_name in node_names:
            custom_param = pipeline[node_name]["recognition"]["param"]["custom_recognition_param"]
            self.assertEqual(custom_param, {"grid_type": "transfer"})
            self.assertNotIn("item_ids", custom_param)

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
            "ItemTransferFindForwardItemInRepo": [154, 202, 585, 291],
            "ItemTransferFindReturnItemInRepo": [154, 202, 585, 291],
            "ItemTransferFindForwardItemInBag": [739, 202, 398, 291],
            "ItemTransferFindReturnItemInBag": [739, 202, 398, 291],
        }

        for node_name, roi in expected_rois.items():
            self.assertEqual(
                pipeline[node_name]["pre_wait_freezes"],
                {"time": 400, "target": roi},
            )

    def test_pipeline_dispatches_two_independent_directions(self) -> None:
        pipeline = self.load_item_transfer_pipeline()

        self.assertEqual(
            pipeline["ItemTransfer"]["next"],
            [
                "ItemTransferSkipSameRegionValleyIV",
                "ItemTransferSkipSameRegionWuling",
                "ItemTransferSkipSameItem",
                "ItemTransferStart",
            ],
        )
        self.assertEqual(pipeline["ItemTransferForwardGate"]["recognition"], "DirectHit")
        self.assertEqual(pipeline["ItemTransferReturnGate"]["recognition"], "DirectHit")
        self.assertFalse(pipeline["ItemTransferReturnGate"]["enabled"])
        self.assertEqual(
            pipeline["ItemTransferOriginDispatch"]["next"],
            [
                "ItemTransferForwardGate",
                "ItemTransferReturnGate",
                "ItemTransferStop",
            ],
        )
        self.assertEqual(
            pipeline["ItemTransferDestinationDispatch"]["next"],
            [
                "ItemTransferReturnGate",
                "ItemTransferForwardGate",
                "ItemTransferStop",
            ],
        )

    def test_same_item_check_is_before_opening_backpack(self) -> None:
        pipeline = self.load_item_transfer_pipeline()
        same_item = pipeline["ItemTransferSkipSameItem"]

        self.assertFalse(same_item["enabled"])
        self.assertEqual(
            same_item["recognition"]["param"]["custom_recognition"],
            "ItemTransferSameItemRecognition",
        )
        self.assertEqual(
            same_item["recognition"]["param"]["custom_recognition_param"],
            {
                "forward_item_node": "ItemTransferFindForwardItemInRepo",
                "return_item_node": "ItemTransferFindReturnItemInRepo",
            },
        )
        self.assertEqual(same_item["action"], "StopTask")

    def test_task_places_return_transfer_before_regions(self) -> None:
        repo_root = Path(__file__).resolve().parents[3]
        task = json.loads(
            (repo_root / "assets" / "tasks" / "ItemTransfer.json").read_text(
                encoding="utf-8"
            )
        )
        item_transfer = next(item for item in task["task"] if item["name"] == "ItemTransfer")

        self.assertEqual(
            item_transfer["option"][:5],
            [
                "WhatToTransfer",
                "TransferAll",
                "EnableReturnTransfer",
                "OriginRegion",
                "DestinationRegion",
            ],
        )

    def test_searches_current_page_before_resetting_scroll_boundaries(self) -> None:
        pipeline = self.load_item_transfer_pipeline()

        self.assertEqual(
            pipeline["ItemTransferRepoSearchCurrentPage"]["next"],
            [
                "[Anchor]ItemTransferCurrentRepoFind",
                "ItemTransferResetRepoScanBoundaries",
            ],
        )
        self.assertEqual(
            pipeline["ItemTransferBagSearchCurrentPage"]["next"],
            [
                "[Anchor]ItemTransferCurrentBagFind",
                "ItemTransferResetBagScanBoundaries",
            ],
        )

    def test_full_scan_uses_independent_top_and_bottom_boundaries(self) -> None:
        pipeline = self.load_item_transfer_pipeline()
        boundary_groups = {
            "Repo": ("ItemTransferRepoTopReached", "ItemTransferRepoBottomReached"),
            "Bag": ("ItemTransferBagTopReached", "ItemTransferBagBottomReached"),
        }

        for side, boundary_nodes in boundary_groups.items():
            reset = pipeline[f"ItemTransferReset{side}ScanBoundaries"]
            patch = reset["custom_action_param"]["patch"]
            for node_name in boundary_nodes:
                self.assertEqual(patch[node_name]["attach"], {"ready": False})
                self.assertEqual(
                    pipeline[node_name]["custom_recognition"],
                    "ListCompleteRecognition",
                )

    def test_item_not_found_disables_only_its_direction(self) -> None:
        pipeline = self.load_item_transfer_pipeline()
        expected = {
            "ItemTransferForwardItemNotFound": "ItemTransferForwardGate",
            "ItemTransferReturnItemNotFound": "ItemTransferReturnGate",
        }

        for node_name, gate_name in expected.items():
            patch = pipeline[node_name]["custom_action_param"]["patch"]
            self.assertEqual(patch, {gate_name: {"enabled": False}})

    def test_unverified_target_storage_disables_only_its_direction(self) -> None:
        pipeline = self.load_item_transfer_pipeline()
        expected = {
            "forward": {
                "still_in_bag": "ItemTransferForwardItemStillInBag",
                "full_check": "ItemTransferCheckIfFull",
                "fallback": "ItemTransferForwardStoreUnverified",
                "gate": "ItemTransferForwardGate",
                "anchor": {
                    "ItemTransferOriginArrivedNext": "ItemTransferForwardReturnToOriginRepo"
                },
                "switch": "ItemTransferSwitchRepoToOrigin",
                "focus": "$task.ItemTransfer.forward_store_unverified",
            },
            "return": {
                "still_in_bag": "ItemTransferReturnItemStillInBag",
                "full_check": "ItemTransferCheckIfFullReturnTarget",
                "fallback": "ItemTransferReturnStoreUnverified",
                "gate": "ItemTransferReturnGate",
                "anchor": {
                    "ItemTransferDestinationArrivedNext": "ItemTransferReturnReturnToDestinationRepo"
                },
                "switch": "ItemTransferSwitchRepoToDestination",
                "focus": "$task.ItemTransfer.return_store_unverified",
            },
        }

        for direction in expected.values():
            self.assertEqual(
                pipeline[direction["still_in_bag"]]["next"],
                [direction["full_check"], direction["fallback"]],
            )
            fallback = pipeline[direction["fallback"]]
            self.assertEqual(fallback["action"], "Custom")
            self.assertEqual(fallback["custom_action"], "PipelineOverrideAction")
            self.assertEqual(
                fallback["custom_action_param"]["patch"],
                {direction["gate"]: {"enabled": False}},
            )
            self.assertEqual(fallback["anchor"], direction["anchor"])
            self.assertEqual(fallback["next"], [direction["switch"]])
            self.assertEqual(
                fallback["focus"]["Node.Action.Succeeded"], direction["focus"]
            )

        self.assertEqual(pipeline["ItemTransferStoreUnverified"]["action"], "StopTask")

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
            "item_nurturance": make_item("Nurturance", -60, 1),
        }
        zh_cn = {
            "iconRecognition.name.item_nurturance": "培养素材",
        }

        forward = build_transfer_cases(catalog, zh_cn, FORWARD_NODES)[0]
        backward = build_transfer_cases(catalog, zh_cn, RETURN_NODES)[0]

        self.assertEqual(forward["name"], "培养素材")
        self.assertEqual(forward["label"], "$iconRecognition.name.item_nurturance")
        self.assertEqual(
            forward["pipeline_override"],
            {
                "ItemTransferClickForwardItemCategory": {
                    "template": "ItemTransfer/Nurturance.png",
                },
                "ItemTransferFindForwardItemInRepo": self.item_id_override("item_nurturance"),
                "ItemTransferFindForwardItemInBag": self.item_id_override("item_nurturance"),
            },
        )
        self.assertEqual(
            backward["pipeline_override"],
            {
                "ItemTransferClickReturnItemCategory": {
                    "template": "ItemTransfer/Nurturance.png",
                },
                "ItemTransferFindReturnItemInRepo": self.item_id_override("item_nurturance"),
                "ItemTransferFindReturnItemInBag": self.item_id_override("item_nurturance"),
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

    def test_ctrl_click_paths_use_atomic_custom_action(self) -> None:
        pipeline = self.load_item_transfer_pipeline()
        expected_targets = {
            "ItemTransferTransferForwardToBag": "ItemTransferFindForwardItemInRepo",
            "ItemTransferTransferReturnToBag": "ItemTransferFindReturnItemInRepo",
            "ItemTransferTransferForwardToRepo": "ItemTransferFindForwardItemInBag",
            "ItemTransferTransferReturnToRepo": "ItemTransferFindReturnItemInBag",
            "ItemTransferTransferToRepoReturn": "ItemTransferFindItemInBagReturn",
        }

        for node_name, target in expected_targets.items():
            node = pipeline[node_name]
            self.assertEqual(node["action"], "Custom")
            self.assertEqual(node["custom_action"], "ItemTransferCtrlClickAction")
            self.assertEqual(node["target"], target)
            self.assertEqual(node["target_offset"], [26, 25, -52, -50])

        for obsolete_node in (
            "ItemTransferCtrlKeyDownRepo",
            "ItemTransferCtrlKeyUpRepo",
            "ItemTransferCtrlKeyDownBag",
            "ItemTransferCtrlKeyUpBag",
            "ItemTransferCtrlKeyDownBagReturn",
            "ItemTransferCtrlKeyUpBagReturn",
        ):
            self.assertNotIn(obsolete_node, pipeline)

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
