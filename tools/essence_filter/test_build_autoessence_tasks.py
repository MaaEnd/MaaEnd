"""Tests for tools/essence_filter/build_autoessence_tasks.py."""

import io
import json
import os
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))

from build_autoessence_tasks import (
    LOCATION_KEYS,
    OCR_ROI,
    load_location_keys,
    ocr_expected,
    resolve_locations,
    select_weapons,
    strip_choose_location,
    sync_anchor_list,
    weapon_icon,
)

ROOT = Path(__file__).resolve().parents[2]


class LocationKeyTest(unittest.TestCase):
    def load_text(self, text: str) -> dict[str, str]:
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "location_keys.json"
            path.write_text(text, encoding="utf-8")
            return load_location_keys(path)

    def test_loads_shared_json(self):
        keys = load_location_keys(LOCATION_KEYS)
        self.assertEqual(keys["map01_lv001"], "VFTheHub")
        self.assertEqual(keys["map02_lv009"], "WLSnowyForest")

    def test_accepts_jsonc_like_go(self):
        keys = self.load_text('\ufeff{\n    // 祀的画路线未完成\n    "map01_lv001": "VFTheHub",\n}\n')
        self.assertEqual(keys, {"map01_lv001": "VFTheHub"})

    def test_rejects_duplicate_level_id(self):
        with self.assertRaisesRegex(ValueError, "location_keys.json"):
            self.load_text('{"map02_lv009": "WLSnowyForest", "map02_lv009": "WLSisPainting"}')

    def test_rejects_key_unusable_in_names(self):
        for key in ("WLSi_Painting", "TheHub", "VF", ""):
            with self.subTest(key=key), self.assertRaisesRegex(ValueError, "map02_lv010"):
                self.load_text(json.dumps({"map02_lv010": key}))

    def resolve_with_unregistered(self, env: dict[str, str]) -> tuple[list[str], str, str]:
        stdout, stderr = io.StringIO(), io.StringIO()
        with mock.patch.dict(os.environ, env):
            for name in ("GITHUB_ACTIONS", "GITHUB_STEP_SUMMARY"):
                if name not in env:
                    os.environ.pop(name, None)
            with redirect_stdout(stdout), redirect_stderr(stderr):
                resolved = resolve_locations(
                    [
                        {"name": "重度能量淤积点·祀的画", "level_id": "map02_lv010"},
                        {"name": "重度能量淤积点·枢纽区", "level_id": "map01_lv001"},
                    ],
                    {"map01_lv001": "VFTheHub"},
                    {"global.region.TheHub"},
                )
        return [key for key, _ in resolved], stdout.getvalue(), stderr.getvalue()

    def test_unregistered_location_is_skipped(self):
        keys, stdout, stderr = self.resolve_with_unregistered({})
        self.assertEqual(keys, ["VFTheHub"])
        self.assertIn("map02_lv010", stderr)
        self.assertNotIn("::warning", stdout)

    def test_skip_is_reported_in_github_actions(self):
        with tempfile.TemporaryDirectory() as tmp:
            summary = Path(tmp) / "summary.md"
            keys, stdout, _ = self.resolve_with_unregistered(
                {"GITHUB_ACTIONS": "true", "GITHUB_STEP_SUMMARY": str(summary)}
            )
            report = summary.read_text(encoding="utf-8")
        self.assertEqual(keys, ["VFTheHub"])
        self.assertRegex(stdout, r"(?m)^::warning title=[^:]+::.*map02_lv010")
        self.assertIn("| 重度能量淤积点·祀的画 | `map02_lv010` |", report)

    def test_registered_level_missing_from_data_fails(self):
        # 上游把枢纽区改成别的 level_id：要报错，不能当成新地区跳过（跳过会删掉 VFTheHub 的选项）。
        with self.assertRaisesRegex(ValueError, "map01_lv001"):
            resolve_locations(
                [{"name": "重度能量淤积点·枢纽区", "level_id": "map01_lv011"}],
                {"map01_lv001": "VFTheHub"},
                {"global.region.TheHub"},
            )


class OcrExpectedTest(unittest.TestCase):
    def test_dedupes_pool_languages(self):
        expected = ocr_expected(
            "slot1",
            {
                "id": 3,
                "cn": "意志",
                "tc": "意志",
                "en": "Will",
                "jp": "意志",
                "kr": "의지",
            },
        )
        self.assertEqual(expected, ["意志", "Will", "의지"])

    def test_ultimate_uses_interface_text(self):
        pools = json.loads(
            (ROOT / "assets/data/EssenceFilter/skill_pools.json").read_text(encoding="utf-8")
        )
        entry = next(item for item in pools["slot2"] if item["id"] == 11)
        expected = ocr_expected("slot2", entry)
        self.assertIn("Ultimate Gain", expected)
        self.assertNotIn("ULT", expected)

    def test_arts_intensity_keeps_ui_aliases(self):
        pools = json.loads(
            (ROOT / "assets/data/EssenceFilter/skill_pools.json").read_text(encoding="utf-8")
        )
        entry = next(item for item in pools["slot2"] if item["id"] == 6)
        expected = ocr_expected("slot2", entry)
        self.assertEqual(expected[0], "源石技艺提升")
        self.assertIn("源石技艺强度", expected)
        self.assertIn("アーツ強度UP", expected)


class WeaponSelectionTest(unittest.TestCase):
    def test_keeps_five_and_six_star_sorted(self):
        grouped = select_weapons(
            {
                "wpn_sword_0009": {"weapon_type": "Sword", "rarity": 4},
                "wpn_sword_0005": {"weapon_type": "Sword", "rarity": 5},
                "wpn_sword_0010": {"weapon_type": "Sword", "rarity": 6},
                "wpn_sword_0006": {"weapon_type": "Sword", "rarity": 6},
            }
        )
        self.assertEqual(
            [item["id"] for item in grouped["Sword"]],
            ["wpn_sword_0006", "wpn_sword_0010", "wpn_sword_0005"],
        )

    def test_icon_path_uses_item_id(self):
        self.assertEqual(weapon_icon("wpn_sword_0010"), "resource/image/UI/Item/wpn_sword_0010.png")
        self.assertTrue((ROOT / "assets/resource/image/UI/Item/wpn_sword_0010.png").is_file())


class TextRewriteTest(unittest.TestCase):
    def test_strip_choose_location(self):
        text = """{
    "option": {
        "Keep": {
            "type": "select"
        },
        "AutoEssenceChooseLocation": {
            "cases": [
                {"name": "VFTheHub"}
            ]
        },
        "Next": {
            "type": "select"
        }
    }
}
"""
        updated = strip_choose_location(text)
        self.assertNotIn("AutoEssenceChooseLocation", updated)
        self.assertIn('"Keep"', updated)
        self.assertIn('"Next"', updated)

    def test_sync_anchor_list(self):
        text = """{
    "sub": [
        "GotoTriggerPointSetAnchor_VFTheHub",
        "GotoTriggerPointSetAnchor_WLSnowyForest"
    ]
}
"""
        updated = sync_anchor_list(text, ["VFTheHub", "WLWulingCity"])
        self.assertIn('"GotoTriggerPointSetAnchor_WLWulingCity"', updated)
        self.assertNotIn("WLSnowyForest", updated)
        self.assertEqual(updated.count("GotoTriggerPointSetAnchor"), 2)


class SecondaryShapeTest(unittest.TestCase):
    def test_roi_is_720p(self):
        self.assertEqual(OCR_ROI, [43, 125, 904, 540])


if __name__ == "__main__":
    unittest.main()
