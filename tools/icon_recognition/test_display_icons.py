from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from PIL import Image

from display_icons import (
    DISPLAY_SIZE,
    RARITY_MASK_FILES,
    build_icon_index,
    check,
    collect_referenced_icon_ids,
    generate,
    mask_path,
    plan,
    render_display_icon,
)


def _write_png(path: Path, size: tuple[int, int], color: tuple[int, int, int, int]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    Image.new("RGBA", size, color).save(path, format="PNG")


def _write_mask(path: Path, size: tuple[int, int], color: tuple[int, int, int, int]) -> None:
    """下半不透明、上半透明的遮罩，用于验证拉伸后确实覆盖整张图。"""
    path.parent.mkdir(parents=True, exist_ok=True)
    image = Image.new("RGBA", size, (color[0], color[1], color[2], 0))
    for y in range(size[1] // 2, size[1]):
        for x in range(size[0]):
            image.putpixel((x, y), color)
    image.save(path, format="PNG")


class DisplayIconsTest(unittest.TestCase):
    def test_rarity_mask_mapping_covers_all_rarities(self) -> None:
        self.assertEqual(sorted(RARITY_MASK_FILES), [
            1,
            2,
            3,
            4,
            5,
            6,
        ])
        self.assertEqual(RARITY_MASK_FILES[1], "Gray.png")
        self.assertEqual(RARITY_MASK_FILES[6], "Orange.png")

    def test_build_icon_index_rejects_shared_icon_across_rarities(self) -> None:
        catalog = {
            "item_a": {"iconId": "item_shared", "rarity": 3},
            "item_b": {"iconId": "item_shared", "rarity": 3},
            "item_c": {"iconId": "item_other", "rarity": 5},
        }
        index = build_icon_index(catalog)
        self.assertEqual(index["item_shared"]["rarity"], 3)
        self.assertEqual(index["item_shared"]["item_id"], "item_a")
        self.assertEqual(index["item_other"]["rarity"], 5)

        with self.assertRaisesRegex(ValueError, "无法确定遮罩"):
            build_icon_index({
                "item_a": {"iconId": "item_shared", "rarity": 3},
                "item_b": {"iconId": "item_shared", "rarity": 4},
            })

    def test_build_icon_index_rejects_invalid_rarity(self) -> None:
        with self.assertRaisesRegex(ValueError, "rarity"):
            build_icon_index({"item_a": {"iconId": "item_a", "rarity": 7}})
        with self.assertRaisesRegex(ValueError, "rarity"):
            build_icon_index({"item_a": {"iconId": "item_a", "rarity": True}})

    def test_collect_referenced_icon_ids_reads_fields_and_rich_labels(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            root = Path(raw)
            (root / "Task.json").write_text(
                json.dumps(
                    {
                        "option": {
                            "Items": {
                                "cases": [
                                    {"name": "A", "icon": "resource/image/icon/item_a.png"},
                                ],
                            },
                            "Limits": {
                                "inputs": [
                                    {
                                        "name": "B",
                                        "label": "$option.Limits.inputs.B.label",
                                    },
                                ],
                            },
                        },
                    },
                ),
                encoding="utf-8",
            )
            (root / "locales.json").write_text(
                json.dumps({"option.Limits.inputs.B.label": "![](resource/image/icon/item_b.png) 乙 ★4"}),
                encoding="utf-8",
            )
            references = collect_referenced_icon_ids([root])
            self.assertEqual(sorted(references), ["item_a", "item_b"])
            self.assertEqual(len(references["item_a"]), 1)
            self.assertTrue(next(iter(references["item_a"])).endswith("Task.json"))

    def test_render_display_icon_stretches_mask_over_whole_icon(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            root = Path(raw)
            source = root / "5/item_wide.png"
            mask = root / "masks/Gold.png"
            _write_png(source, (64, 64), (0, 0, 0, 255))
            _write_mask(mask, (192, 64), (255, 204, 0, 255))

            image = render_display_icon(source, mask, size=DISPLAY_SIZE)

            self.assertEqual(image.size, (DISPLAY_SIZE, DISPLAY_SIZE))
            self.assertEqual(image.mode, "RGBA")
            # 遮罩被垂直拉伸后覆盖整张图：下半实色、上半不染色
            self.assertEqual(image.getpixel((8, 14)), (255, 204, 0, 255))
            self.assertEqual(image.getpixel((8, 2)), (0, 0, 0, 255))

    def test_generate_and_check_round_trip(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            root = Path(raw)
            image_root = root / "images"
            masks_root = root / "masks"
            output_root = root / "icon"
            _write_png(image_root / "3/item_a.png", (32, 32), (10, 20, 30, 255))
            _write_mask(masks_root / "Blue.png", (96, 32), (0, 128, 255, 255))

            catalog = {"item_a": {"iconId": "item_a", "rarity": 3}}
            jobs, problems = plan(
                catalog,
                {"item_a": {"Task.json"}},
                image_root=image_root,
                masks_root=masks_root,
            )
            self.assertEqual(problems, [])
            self.assertEqual(list(jobs), ["item_a"])

            missing = check(jobs, output_root=output_root)
            self.assertEqual(len(missing), 1)
            self.assertIn("缺少展示图标", missing[0])

            written = generate(jobs, output_root=output_root)
            self.assertEqual(len(written), 1)
            self.assertTrue(written[0].endswith("item_a.png"))
            self.assertEqual(check(jobs, output_root=output_root), [])

            # 重复运行不再写入（幂等）
            self.assertEqual(generate(jobs, output_root=output_root), [])

            # 素材变化后 check 能发现过期
            _write_png(image_root / "3/item_a.png", (32, 32), (200, 10, 10, 255))
            self.assertEqual(len(check(jobs, output_root=output_root)), 1)

    def test_plan_reports_missing_catalog_source_and_mask(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            root = Path(raw)
            image_root = root / "images"
            masks_root = root / "masks"
            _write_png(image_root / "2/item_b.png", (16, 16), (0, 0, 0, 255))
            _write_mask(masks_root / "Green.png", (48, 16), (0, 255, 0, 255))

            catalog = {
                "item_b": {"iconId": "item_b", "rarity": 2},
                "item_c": {"iconId": "item_c", "rarity": 5},
            }
            jobs, problems = plan(
                catalog,
                {"item_b": {"Task.json"}, "item_c": {"Task.json"}, "item_unknown": {"Task.json"}},
                image_root=image_root,
                masks_root=masks_root,
            )
            self.assertEqual(list(jobs), ["item_b"])
            joined = "\n".join(problems)
            self.assertIn("item_c 缺少识别素材", joined)
            self.assertIn("item_unknown 不在 IconRecognition 目录中", joined)

    def test_mask_path_uses_vendored_masks(self) -> None:
        self.assertTrue(mask_path(5).is_file(), "缺少 Gold 遮罩素材")
        self.assertTrue(mask_path(1).is_file(), "缺少 Gray 遮罩素材")


if __name__ == "__main__":
    unittest.main()
