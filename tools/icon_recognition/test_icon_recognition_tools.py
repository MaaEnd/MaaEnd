from __future__ import annotations

import json
import struct
import tempfile
import unittest
from pathlib import Path

from catalog import build_catalog, write_catalog
from download import (
    DEFAULT_CACHE_ROOT,
    IMAGE_BASE_URL,
    ITEM_TABLE_URL,
    LANG_URL,
    WEAPON_TABLE_URL,
    build_download_jobs,
    download_sources,
    validate_icon_png_bytes,
    merge_item_sources,
    prepare_item_map,
    prepare_weapon_map,
)
from localization import (
    FIXED_NAME_KEYS,
    LOCALE_MAP,
    build_locale_values,
    update_interface_locale,
)
from publish import default_publish_paths, publish, publish_fixed_items
from expected import merge_expected_results
from text import clean_text, validate_identifier


class IconRecognitionToolsTest(unittest.TestCase):
    @staticmethod
    def _mini_item(**overrides: object) -> dict[str, object]:
        item: dict[str, object] = {
            "name": "测试物品",
            "category": "产物",
            "storageKind": "Normal",
            "categoryType": "Product",
            "iconId": "item_test",
            "rarity": 3,
            "sortId1": -100,
            "sortId2": 12,
            "fluidType": None,
            "fluid": None,
            "fullContainers": [],
            "emptyContainers": [],
        }
        item.update(overrides)
        return item

    def test_publish_defaults_use_tool_cache_and_public_assets(self) -> None:
        paths = default_publish_paths(Path("repo"))
        self.assertEqual(paths.item_source, Path("repo/tools/icon_recognition/.cache/downloads/item.json"))
        self.assertEqual(paths.image_root, Path("repo/tools/icon_recognition/.cache/downloads/images"))
        self.assertEqual(paths.catalog_output, Path("repo/assets/data/IconRecognition/recognition_items.json"))
        self.assertEqual(paths.asset_image_root, Path("repo/assets/resource/image/IconRecognition"))
        self.assertEqual(paths.locale_root, Path("repo/assets/locales/interface"))

    def test_fixed_publish_updates_only_fixed_catalog_and_locale_entries(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            paths = default_publish_paths(root)
            paths.catalog_output.parent.mkdir(parents=True)
            paths.catalog_output.write_text(
                json.dumps(
                    {
                        "item_kept": {
                            "name": "保留",
                            "category": "产物",
                            "storageKind": "Normal",
                            "categoryType": "Product",
                            "rarity": 3,
                            "iconId": "item_kept",
                            "fluidIconId": "",
                        },
                        "item_diamond": {
                            "name": "旧嵌晶玉",
                            "category": "独立资源",
                            "storageKind": "Isolate",
                            "categoryType": "Diamond",
                            "rarity": 5,
                            "iconId": "item_diamond",
                            "fluidIconId": "",
                        },
                    },
                    ensure_ascii=False,
                ),
                encoding="utf-8",
            )
            for locale, (_, language) in LOCALE_MAP.items():
                locale_path = paths.locale_root / f"{locale}.json"
                locale_path.parent.mkdir(parents=True, exist_ok=True)
                locale_path.write_text(
                    json.dumps(
                        {
                            "unrelated": "keep",
                            "iconRecognition.name.item_kept": "Keep",
                            "iconRecognition.name.item_diamond": "Old",
                        }
                    ),
                    encoding="utf-8",
                )
                translations = {
                    key: f"{language}:{item_id}"
                    for item_id, key in FIXED_NAME_KEYS.items()
                }
                if locale == "en_us":
                    translations.pop(FIXED_NAME_KEYS["item_diamond"])
                language_path = paths.language_root / f"lang_{language}.json"
                language_path.parent.mkdir(parents=True, exist_ok=True)
                language_path.write_text(json.dumps(translations), encoding="utf-8")
            for item_id, item in merge_item_sources({}, {}).items():
                source = paths.image_root / str(item["rarity"]) / f"{item_id}.png"
                source.parent.mkdir(parents=True, exist_ok=True)
                source.write_bytes(self._png_header(128, 128))
            stale = paths.asset_image_root / "5" / "item_diamond.png"
            stale.parent.mkdir(parents=True, exist_ok=True)
            stale.write_bytes(b"old")

            count = publish_fixed_items(paths)

            catalog = json.loads(paths.catalog_output.read_text(encoding="utf-8"))
            self.assertEqual(count, 9)
            self.assertIn("item_kept", catalog)
            self.assertEqual(catalog["item_diamond"]["rarity"], 6)
            self.assertEqual(catalog["item_diamond"]["name"], "zh-CN:item_diamond")
            self.assertFalse(stale.exists())
            self.assertTrue((paths.asset_image_root / "6" / "item_diamond.png").is_file())
            locale = json.loads((paths.locale_root / "en_us.json").read_text(encoding="utf-8"))
            self.assertEqual(locale["unrelated"], "keep")
            self.assertEqual(locale["iconRecognition.name.item_kept"], "Keep")
            self.assertEqual(
                locale["iconRecognition.name.item_diamond"],
                FIXED_NAME_KEYS["item_diamond"],
            )

    def test_expected_merge_replaces_old_image_cases_and_keeps_all_reported_rois(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            tracked = root / "tracked.csv"
            tracked.write_text(
                'image,roi,item_id,count\n'
                'transfer/1.png,"[1,2,3,4]",item_old,1\n'
                'rewards/130.png,"[9,9,9,9]",item_stale,1\n',
                encoding="utf-8",
            )
            detail = root / "detail" / "rewards-full-130.local1.json"
            detail.parent.mkdir()
            detail.write_text(
                json.dumps(
                    {
                        "matches": [
                            {"item_id": "item_new"},
                            {"item_id": "item_new"},
                            {"item_id": "item_other"},
                        ]
                    }
                ),
                encoding="utf-8",
            )
            second_detail = root / "detail" / "rewards-right-130.local1.json"
            second_detail.write_text(
                json.dumps({"matches": [{"item_id": "item_second_roi"}]}),
                encoding="utf-8",
            )
            report = root / "report.json"
            report.write_text(
                json.dumps(
                    {
                        "cases": [
                            {
                                "image": "rewards/130.local1.png",
                                "roi": [150, 180, 980, 360],
                                "detail": str(detail),
                            },
                            {
                                "image": "rewards/130.local1.png",
                                "roi": [1130, 180, 100, 100],
                                "detail": str(second_detail),
                            },
                        ]
                    }
                ),
                encoding="utf-8",
            )
            output = root / "expected.csv"

            merge_expected_results(tracked, report, output)

            self.assertEqual(
                output.read_text(encoding="utf-8"),
                'image,roi,item_id,count\n'
                'rewards/130.png,"[1130,180,100,100]",item_second_roi,1\n'
                'rewards/130.png,"[150,180,980,360]",item_new,2\n'
                'rewards/130.png,"[150,180,980,360]",item_other,1\n'
                'transfer/1.png,"[1,2,3,4]",item_old,1\n',
            )

    def test_expected_merge_rejects_object_roi(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            tracked = root / "tracked.csv"
            tracked.write_text("image,roi,item_id,count\n", encoding="utf-8")
            detail = root / "detail.json"
            detail.write_text('{"matches": []}', encoding="utf-8")
            report = root / "report.json"
            report.write_text(
                json.dumps(
                    {
                        "cases": [
                            {
                                "image": "shipment/1.png",
                                "roi": {"x": 1, "y": 2, "width": 3, "height": 4},
                                "detail": str(detail),
                            }
                        ]
                    }
                ),
                encoding="utf-8",
            )

            with self.assertRaisesRegex(
                ValueError,
                r"report ROI must be \[x,y,width,height\]",
            ):
                merge_expected_results(tracked, report, root / "expected.csv")

    def test_expected_merge_naturally_sorts_numbered_images(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            tracked = root / "tracked.csv"
            tracked.write_text(
                'image,roi,item_id,count\n'
                'shipment/10.png,"[1,2,3,4]",item_10,1\n'
                'shipment/1.png,"[1,2,3,4]",item_1,1\n'
                'shipment/9.png,"[1,2,3,4]",item_9,1\n',
                encoding="utf-8",
            )
            report = root / "report.json"
            report.write_text('{"cases": []}', encoding="utf-8")
            output = root / "expected.csv"

            merge_expected_results(tracked, report, output)

            self.assertEqual(
                output.read_text(encoding="utf-8"),
                'image,roi,item_id,count\n'
                'shipment/1.png,"[1,2,3,4]",item_1,1\n'
                'shipment/9.png,"[1,2,3,4]",item_9,1\n'
                'shipment/10.png,"[1,2,3,4]",item_10,1\n',
            )

    @staticmethod
    def _png_header(width: int, height: int) -> bytes:
        return b"\x89PNG\r\n\x1a\n" + struct.pack(
            ">I4sIIBBBBB",
            13,
            b"IHDR",
            width,
            height,
            8,
            6,
            0,
            0,
            0,
        )

    def test_icon_png_requires_square_power_of_two_dimensions(self) -> None:
        self.assertEqual(validate_icon_png_bytes(self._png_header(128, 128)), 128)
        self.assertEqual(validate_icon_png_bytes(self._png_header(256, 256)), 256)
        with self.assertRaisesRegex(ValueError, "正方形"):
            validate_icon_png_bytes(self._png_header(128, 256))
        with self.assertRaisesRegex(ValueError, "2 的整数次幂"):
            validate_icon_png_bytes(self._png_header(127, 127))

    def test_clean_text_replaces_controls_and_compresses_space(self) -> None:
        self.assertEqual(
            clean_text("  A\x00\x08\n\tB\r\x1f  ", field="name"),
            "A B",
        )

    def test_identifier_rejects_path_and_control_characters(self) -> None:
        with self.assertRaises(ValueError):
            validate_identifier("a/b", field="iconId")
        with self.assertRaises(ValueError):
            validate_identifier("a\x00b", field="iconId")

    def test_remote_urls_match_verified_sources(self) -> None:
        self.assertEqual(DEFAULT_CACHE_ROOT, Path("tools/icon_recognition/.cache/downloads"))
        self.assertEqual(
            ITEM_TABLE_URL,
            "https://assets.fz.wiki/output_beyondmap/item_mini_table.json",
        )
        self.assertEqual(
            WEAPON_TABLE_URL,
            "https://assets.fz.wiki/output_maaend/weapons.json",
        )
        self.assertEqual(
            LANG_URL.format(locale="en-US"),
            "https://assets.fz.wiki/output_beyondmap/i18n/en-US/lang.json",
        )
        self.assertEqual(
            IMAGE_BASE_URL,
            "https://assets.fz.wiki/output_image/itemicon",
        )

    def test_download_sources_timestamp_all_remote_json_urls(self) -> None:
        sources = download_sources(Path("cache"), dry_run=True)
        self.assertTrue(all("timestamp=" in url for url in sources.values()))

    def test_icon_url_has_no_rarity_segment_and_uses_raw_suffix(self) -> None:
        items = {
            "item_test": {
                "name": "测试",
                "iconId": "icon id+plus",
                "rarity": 6,
            }
        }

        jobs, missing = build_download_jobs(items, Path("images"))

        self.assertEqual([], missing)
        self.assertEqual(
            jobs[0].url,
            "https://assets.fz.wiki/output_image/itemicon/icon%20id%2Bplus.png@raw",
        )
        self.assertEqual(jobs[0].destination, Path("images/6/icon id+plus.png"))

    def test_catalog_applies_currency_types_to_actual_item_ids(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            image_root = Path(directory) / "images"
            for rarity, icon_id in (
                (4, "item_gold"),
                (6, "item_diamond"),
                (6, "item_gachabyproducts_weapongold"),
            ):
                path = image_root / str(rarity) / f"{icon_id}.png"
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(b"png")
            source = {
                "item_gold": {"name": "1b858553aff0ccfee876ed0367300534", "category": "独立资源", "storageKind": "Isolate", "categoryType": "Gold", "rarity": 4, "iconId": "item_gold", "fullContainers": []},
                "item_diamond": {"name": "aa5edf17dd5cbb87661ec9f3d86d353e", "category": "独立资源", "storageKind": "Isolate", "categoryType": "Diamond", "rarity": 6, "iconId": "item_diamond", "fullContainers": []},
                "item_gachabyproducts_weapongold": {"name": "011c0bffc4a5c215eb651edaf3e5b929", "category": "独立资源", "storageKind": "Isolate", "categoryType": "WeaponGold", "rarity": 6, "iconId": "item_gachabyproducts_weapongold", "fullContainers": []},
            }
            result = build_catalog(source, image_root)
            self.assertEqual(result["item_gold"]["categoryType"], "Gold")
            self.assertEqual(result["item_diamond"]["categoryType"], "Diamond")
            self.assertEqual(
                result["item_gachabyproducts_weapongold"]["categoryType"],
                "WeaponGold",
            )

    def test_catalog_excludes_empty_icon_id_before_identifier_validation(self) -> None:
        source = {
            "item_missing": {
                "name": "缺图物品",
                "category": "产物",
                "storageKind": "Normal",
                "categoryType": "Product",
                "rarity": 3,
                "iconId": "",
                "fullContainers": [],
            }
        }
        with tempfile.TemporaryDirectory() as directory:
            result = build_catalog(source, Path(directory))
        self.assertEqual(result, {})

    def test_catalog_includes_new_source_items_without_frozen_allowlist(self) -> None:
        source = {
            "remote_new_item": {
                "name": "remote_name_key",
                "category": "产物",
                "storageKind": "Normal",
                "categoryType": "Product",
                "rarity": 3,
                "iconId": "remote_new_icon",
                "fullContainers": [],
            }
        }
        with tempfile.TemporaryDirectory() as directory:
            image_root = Path(directory)
            icon = image_root / "3" / "remote_new_icon.png"
            icon.parent.mkdir(parents=True, exist_ok=True)
            icon.write_bytes(b"png")
            result = build_catalog(source, image_root)
        self.assertEqual(list(result), ["remote_new_item"])

    def test_catalog_preserves_item_sort_ids(self) -> None:
        items, removals = prepare_item_map(
            {"item_test": self._mini_item()}
        )
        with tempfile.TemporaryDirectory() as directory:
            image_root = Path(directory)
            icon = image_root / "3" / "item_test.png"
            icon.parent.mkdir(parents=True, exist_ok=True)
            icon.write_bytes(b"png")
            result = build_catalog(items, image_root)

        self.assertEqual(removals, [])
        self.assertEqual(result["item_test"]["sortId1"], -100)
        self.assertEqual(result["item_test"]["sortId2"], 12)

    def test_catalog_order_does_not_depend_on_source_order(self) -> None:
        source = {
            "item_b": self._mini_item(iconId="item_b"),
            "item_a": self._mini_item(iconId="item_a"),
        }
        with tempfile.TemporaryDirectory() as directory:
            image_root = Path(directory)
            for item_id in source:
                icon = image_root / "3" / f"{item_id}.png"
                icon.parent.mkdir(parents=True, exist_ok=True)
                icon.write_bytes(b"png")

            catalog = build_catalog(source, image_root)

        self.assertEqual(list(catalog), ["item_a", "item_b"])

    def test_write_catalog_order_does_not_depend_on_mapping_order(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "recognition_items.json"

            write_catalog({"item_b": {"name": "乙"}, "item_a": {"name": "甲"}}, output)

            catalog = json.loads(output.read_text(encoding="utf-8"))
        self.assertEqual(list(catalog), ["item_a", "item_b"])

    def test_region_restricted_is_optional_and_only_true_is_published(self) -> None:
        source = {
            "item_missing": self._mini_item(iconId="item_missing"),
            "item_false": self._mini_item(
                iconId="item_false",
                regionRestricted=False,
            ),
            "item_true": self._mini_item(
                iconId="item_true",
                regionRestricted=True,
            ),
        }

        items, removals = prepare_item_map(source, blacklist=())

        self.assertEqual(removals, [])
        self.assertNotIn("regionRestricted", items["item_missing"])
        self.assertNotIn("regionRestricted", items["item_false"])
        self.assertIs(items["item_true"].get("regionRestricted"), True)

        with tempfile.TemporaryDirectory() as directory:
            image_root = Path(directory)
            for item_id in source:
                icon = image_root / "3" / f"{item_id}.png"
                icon.parent.mkdir(parents=True, exist_ok=True)
                icon.write_bytes(b"png")
            catalog = build_catalog(items, image_root)

        self.assertNotIn("regionRestricted", catalog["item_missing"])
        self.assertNotIn("regionRestricted", catalog["item_false"])
        self.assertIs(catalog["item_true"].get("regionRestricted"), True)

    def test_region_restricted_rejects_non_boolean_values(self) -> None:
        with self.assertRaisesRegex(
            ValueError,
            r"item_test\.regionRestricted 必须是布尔值",
        ):
            prepare_item_map(
                {
                    "item_test": self._mini_item(
                        regionRestricted="true",
                    )
                },
                blacklist=(),
            )

    def test_publish_replaces_catalog_name_hash_with_zh_cn_locale(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            paths = default_publish_paths(root)
            item = self._mini_item(name="item_name_hash")
            paths.item_source.parent.mkdir(parents=True, exist_ok=True)
            paths.item_source.write_text(
                json.dumps({"item_test": item}),
                encoding="utf-8",
            )
            paths.localization_item_source.write_text(
                json.dumps({"item_test": item}),
                encoding="utf-8",
            )
            paths.weapon_source.write_text("{}", encoding="utf-8")
            icon = paths.image_root / "3" / "item_test.png"
            icon.parent.mkdir(parents=True, exist_ok=True)
            icon.write_bytes(b"png")
            for locale, (_, language) in LOCALE_MAP.items():
                translations = (
                    {}
                    if locale == "en_us"
                    else {
                        "item_name_hash": (
                            "测试物品"
                            if locale == "zh_cn"
                            else f"{language}:item_test"
                        )
                    }
                )
                (paths.language_root / f"lang_{language}.json").write_text(
                    json.dumps(translations, ensure_ascii=False),
                    encoding="utf-8",
                )
                locale_path = paths.locale_root / f"{locale}.json"
                locale_path.parent.mkdir(parents=True, exist_ok=True)
                locale_path.write_text("{}", encoding="utf-8")

            publish(paths)

            catalog = json.loads(paths.catalog_output.read_text(encoding="utf-8"))
            en_us = json.loads(
                (paths.locale_root / "en_us.json").read_text(encoding="utf-8")
            )
        self.assertEqual(catalog["item_test"]["name"], "测试物品")
        self.assertEqual(en_us["iconRecognition.name.item_test"], "item_name_hash")

    def test_prepare_item_map_accepts_isolate_category_types(self) -> None:
        items, removals = prepare_item_map(
            {
                "item_remote_isolate": self._mini_item(
                    category="上游分类名称",
                    storageKind="Isolate",
                    categoryType="FutureResource",
                    iconId="item_remote_isolate",
                )
            },
            blacklist=(),
        )

        self.assertEqual(removals, [])
        self.assertEqual(items["item_remote_isolate"]["category"], "独立资源")
        self.assertEqual(
            items["item_remote_isolate"]["categoryType"],
            "FutureResource",
        )

    def test_prepare_item_map_filters_explicit_normal_product_blacklist(self) -> None:
        blacklisted_ids = {
            "item_activity_xiranite_enr_hulu",
            "item_activity_xiranite_hulu",
            "item_activity_xiranite_enr_tool",
            "item_activity_xiranite_enr_cmpt",
            "item_activity_xiranite_enr_bottle",
            "item_fbottle_xiranenr_grass_2",
            "item_activity_xiranite_cmpt",
            "item_activity_xiranite_bottle",
        }
        source = {
            item_id: self._mini_item(iconId=item_id)
            for item_id in blacklisted_ids
        }
        source["item_kept"] = self._mini_item(iconId="item_kept")
        source["item_liquid_grass"] = self._mini_item(
            iconId="item_liquid_grass",
            fullContainers=["item_fbottle_xiranenr_grass_2"],
        )

        items, removals = prepare_item_map(source)

        self.assertEqual(set(items), {"item_kept"})
        removed_by_id = {row["removedId"]: row for row in removals}
        self.assertTrue(blacklisted_ids <= set(removed_by_id))
        self.assertEqual(removed_by_id["item_liquid_grass"]["reason"], "blacklist-orphan")

        mismatched, mismatched_removals = prepare_item_map(
            {
                "item_activity_xiranite_enr_hulu": self._mini_item(
                    category="可用道具",
                    categoryType="Usable",
                )
            }
        )
        self.assertIn("item_activity_xiranite_enr_hulu", mismatched)
        self.assertEqual(mismatched_removals, [])

    def test_fixed_translation_hashes_are_not_catalog_item_ids(self) -> None:
        self.assertEqual(
            FIXED_NAME_KEYS,
            {
                "item_cbp_exp": "b8cff4d169274d30f90de3fe0bf854d9",
                "item_adventureexp": "9ec5628af29c72843a11c5621ec0e32a",
                "item_gold": "1b858553aff0ccfee876ed0367300534",
                "item_diamond": "aa5edf17dd5cbb87661ec9f3d86d353e",
                "item_gachabyproducts_weapongold": "011c0bffc4a5c215eb651edaf3e5b929",
                "item_domain_jinlong_coupon": "af0a45fb328a9cdd07d2fe0e7ddacbb2",
                "item_domain_tundra_coupon": "40f1e255b753623044d91b8c492ab277",
                "item_spaceship_credit": "1bd1aaac9e3e0db9677cd16b2118ff8f",
                "item_originium_recharge": "ff990914a2dc3f11473e5d6739e873b7",
            },
        )

        merged = merge_item_sources({}, {})

        self.assertEqual(
            set(merged),
            {
                "item_cbp_exp",
                "item_adventureexp",
                "item_gold",
                "item_diamond",
                "item_gachabyproducts_weapongold",
                "item_domain_jinlong_coupon",
                "item_domain_tundra_coupon",
                "item_spaceship_credit",
                "item_originium_recharge",
            },
        )
        self.assertTrue(set(FIXED_NAME_KEYS.values()).isdisjoint(merged))
        self.assertEqual(merged["item_gold"]["rarity"], 4)
        self.assertEqual(merged["item_diamond"]["rarity"], 6)
        self.assertEqual(merged["item_gachabyproducts_weapongold"]["rarity"], 6)
        self.assertTrue(all(item["category"] == "独立资源" for item in merged.values()))
        self.assertTrue(all(item["storageKind"] == "Isolate" for item in merged.values()))

        with tempfile.TemporaryDirectory() as directory:
            item_id = FIXED_NAME_KEYS["item_gold"]
            image = Path(directory) / "images" / "2" / "ordinary.png"
            image.parent.mkdir(parents=True)
            image.write_bytes(b"png")
            catalog = build_catalog(
                {
                    item_id: {
                        "name": "普通物品",
                        "category": "产物",
                        "storageKind": "Normal",
                        "categoryType": "Product",
                        "rarity": 2,
                        "iconId": "ordinary",
                        "fullContainers": [],
                    }
                },
                Path(directory) / "images",
            )
        self.assertEqual(catalog[item_id]["categoryType"], "Product")

    def test_localization_uses_uppercase_weapon_name_keys(self) -> None:
        catalog = {"weapon": {"name": "中文"}}
        source = {"weapon": {"names": {"CN": "中", "TC": "繁", "EN": "English", "JP": "日本", "KR": "한국"}}}
        self.assertEqual(build_locale_values(catalog, source, "EN")["iconRecognition.name.weapon"], "English")

        weapons = prepare_weapon_map(
            {
                "weapon": {
                    "internal_id": "weapon",
                    "rarity": 4,
                    "names": source["weapon"]["names"],
                }
            }
        )
        self.assertEqual(weapons["weapon"]["name"], "中")

    def test_locale_map_uses_verified_path_and_uppercase_weapon_keys(self) -> None:
        self.assertEqual(
            LOCALE_MAP,
            {
                "zh_cn": ("CN", "zh-CN"),
                "zh_tw": ("TC", "zh-TW"),
                "en_us": ("EN", "en-US"),
                "ja_jp": ("JP", "ja-JP"),
                "ko_kr": ("KR", "ko-KR"),
            },
        )

    def test_localization_resolves_mini_table_name_hash(self) -> None:
        catalog = {"item": {"name": "中文", "fluidIconId": ""}}
        source = {"item": {"name": "hash"}}
        values = build_locale_values(catalog, source, "EN", {"hash": "English"})
        self.assertEqual(values["iconRecognition.name.item"], "English")

    def test_localization_resolves_composite_container_and_content_hashes(self) -> None:
        catalog = {
            "container": {
                "name": "容器(内容)",
                "iconId": "container_icon",
                "fluidIconId": "fluid_icon",
            }
        }
        source = {
            "container": {"name": "container_hash", "iconId": "container_icon"},
            "fluid": {"name": "fluid_hash", "iconId": "fluid_icon"},
        }
        values = build_locale_values(
            catalog,
            source,
            "EN",
            {"container_hash": "Container", "fluid_hash": "Content"},
        )
        self.assertEqual(
            values["iconRecognition.name.container"],
            "Container(Content)",
        )

    def test_locale_update_removes_stale_icon_recognition_names(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "locale.json"
            path.write_text(
                json.dumps(
                    {
                        "unrelated": "keep",
                        "iconRecognition.name.stale": "remove",
                    }
                ),
                encoding="utf-8",
            )
            update_interface_locale(
                path,
                {"iconRecognition.name.current": "Current"},
            )
            result = json.loads(path.read_text(encoding="utf-8"))
        self.assertEqual(
            result,
            {
                "unrelated": "keep",
                "iconRecognition.name.current": "Current",
            },
        )

    def test_locale_update_keeps_item_keys_in_one_stable_group(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "locale.json"
            path.write_text(
                json.dumps(
                    {
                        "before": "Before",
                        "iconRecognition.name.item_b": "Old B",
                        "middle": "Middle",
                        "iconRecognition.name.item_a": "Old A",
                        "after": "After",
                    }
                ),
                encoding="utf-8",
            )

            update_interface_locale(
                path,
                {
                    "iconRecognition.name.item_c": "Item C",
                    "iconRecognition.name.item_a": "Item A",
                    "iconRecognition.name.item_b": "Item B",
                },
            )

            result = json.loads(path.read_text(encoding="utf-8"))
        self.assertEqual(
            list(result),
            [
                "before",
                "iconRecognition.name.item_b",
                "iconRecognition.name.item_a",
                "iconRecognition.name.item_c",
                "middle",
                "after",
            ],
        )
        self.assertEqual(
            [result[key] for key in result if key.startswith("iconRecognition.name.")],
            ["Item B", "Item A", "Item C"],
        )


if __name__ == "__main__":
    unittest.main()
