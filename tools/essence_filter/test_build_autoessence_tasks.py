"""Tests for tools/essence_filter/build_autoessence_tasks.py."""

from __future__ import annotations

import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from build_autoessence_tasks import (
    OCR_ROI,
    ocr_expected,
    parse_location_keys,
    select_weapons,
    strip_choose_location,
    sync_anchor_list,
    weapon_icon,
)

ROOT = Path(__file__).resolve().parents[2]


class LocationKeyTest(unittest.TestCase):
    def test_parse_go_map(self):
        source = '''
var locationNameToKey = map[string]string{
	"重度能量淤积点·枢纽区":   "VFTheHub",
	"重度能量淤积点·雪松林":   "WLSnowyForest",
}
'''
        self.assertEqual(
            parse_location_keys(source),
            {
                "重度能量淤积点·枢纽区": "VFTheHub",
                "重度能量淤积点·雪松林": "WLSnowyForest",
            },
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
