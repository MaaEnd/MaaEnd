# /// script
# requires-python = ">=3.12"
# dependencies = [
#     "opencv-python>=4",
# ]
# ///
#
# MapFetcher - Download map data and images.
#
# Usage:
#   python map_fetcher.py json -o/--output-dir <dir>
#   python map_fetcher.py image -i/--input-dir <dir> -o/--output-dir <dir> [--match <kw>] [--no-tiers]

import os
import re
import json
import argparse
import numpy as np
from typing import NamedTuple

from _internal.core_utils import _R, _G, _Y, _C, _A, _0, cv2
from _internal.zmdmap_schemas import RegionLayoutTable, GridTiersTable, EntitiesTable
from _internal.http_utils import download_image, download_json


class APIEndpoint(NamedTuple):
    re_url: str  # Reversed string for anti searching purposes
    file_name: str

    def format(self, *, append_query: bool = True, **kwargs) -> str:
        result = self.re_url[::-1].format(**kwargs)
        if append_query:
            from datetime import datetime, timezone

            assert "?" not in result, "URL already has query parameters"
            result += (
                f"?source=MaaEnd"
                f"&t={datetime.now(timezone.utc).strftime('%Y%m%d%H%M00')}"
            )
        return result


VERSION_API = APIEndpoint(
    re_url="noisrev/dleifdne/1v/ipa/moc.pamdmz.ipa//:sptth",
    file_name="version.json",
)

ENTITIES_API = APIEndpoint(
    # re_url="nosj.seititne_dneaam/}noisrev{/ytitne/atad/moc.pamdmz.stessa//:sptth",
    re_url="nosj.seititne_dneaam/dneaam_tuptuo/ikiw.zf.stessa//:sptth",
    file_name="maaend_entities.json",
)

GRID_TIERS_API = APIEndpoint(
    # re_url="nosj.sreit_dirg/}noisrev{/ytitne/atad/moc.pamdmz.stessa//:sptth",
    re_url="nosj.sreit_dirg/pamdnoyeb_tuptuo/ikiw.zf.stessa//:sptth",
    file_name="grid_tiers.json",
)

REGION_LAYOUT_API = APIEndpoint(
    # re_url="nosj.tuoyal_}eman_pam{/}noisrev{/ytitne/atad/moc.pamdmz.stessa//:sptth",
    re_url="nosj.tuoyal_}eman_pam{/dneaam_tuptuo/ikiw.zf.stessa//:sptth",
    file_name="{map_name}_layout.json",
)

REGION_IMAGE_API = APIEndpoint(
    # re_url="gnp.}eman_noiger{/war/pam/segami/moc.pamdmz.stessa//:sptth",
    re_url="gnp.}eman_noiger{/pam/egami_tuptuo/ikiw.zf.stessa//:sptth",
    file_name="{region_name}.png",
)

TIER_IMAGE_API = APIEndpoint(
    # re_url="gnp.}eman_lluf_egami_reit{/reit/segami/moc.pamdmz.stessa//:sptth",
    re_url="gnp.}eman_lluf_egami_reit{/reit/egami_tuptuo/ikiw.zf.stessa//:sptth",
    file_name="{tier_image_full_name}.png",
)

SCALE_MAP_FACTOR = 0.1625
"""Scale factor to convert *unscaled coordinates* to *converted coordinates*."""

_RE_LAYOUT_FILE = re.compile(r"^(\w+\d+)_layout\.json$")
"""Regex to match remote layout JSON file names.

Groups:
1. region_name
"""


def _save_json(data: dict | list, dest: str, *, sort_dict: bool) -> None:
    if isinstance(data, dict) and sort_dict:
        data = dict(sorted(data.items()))
    elif not isinstance(data, list):
        raise ValueError(f"Data must be a dict or list, got {type(data)}")
    with open(dest, "w", encoding="utf-8") as f:
        json_str = json.dumps(data, ensure_ascii=False, indent=2)
        f.write(json_str)


def _download_json_cached(
    url: str,
    dest: str,
    use_cache: bool = False,
    sort_dict: bool = True,
) -> bool:
    """Download JSON from a remote URL to local destination.

    Skips download if use_cache and file exists.
    If sort_dict, the JSON will be saved with sorted top layer keys.
    """
    if use_cache and os.path.exists(dest):
        return True
    data = download_json(url)
    if data is None:
        return False
    _save_json(data, dest, sort_dict=sort_dict)
    return True


# ── json subcommand ───────────────────────────────────────────────────────────


def test_entities_data(entities_table: EntitiesTable) -> bool:
    """Run regression tests on entities data."""
    # 1. Test total count of entities
    EXPECTED_COUNT_GEQ = 5000

    total_count = sum(
        len(e)
        for r in entities_table.regions.values()
        for l in r.levels.values()
        for e in l.categories.values()
    )
    if total_count < EXPECTED_COUNT_GEQ:
        print(f"  {_Y}Entities: total entities quantity {total_count} is too small{_0}")
        return False

    # 2. Test some specific entities
    TEST_CASES = [
        (22800030005, "campfire", 423.64512, 575.81365),  # 武陵城东门传送锚点
        (23400083018, "campfire", 498.95625, 199.86563),  # 首墩蓄水站传送锚点
        (25000000462, "campfire", 601.68164, 479.32676),  # 藏剑谷演武传送锚点
    ]

    for case in TEST_CASES:
        entity_id, expected_key_contains, expected_x, expected_y = case
        entity = entities_table.find_entity_by_id(entity_id)
        if entity is None:
            print(f"  {_Y}Entities: test entity {entity_id} not found{_0}")
            return False
        if expected_key_contains not in entity.key_name:
            print(
                f"  {_Y}Entities: test entity {entity_id} key name mismatch: "
                f"expected to contain '{expected_key_contains}', got '{entity.key_name}'{_0}"
            )
            return False
        got_x, got_y = entity.map_location
        if abs(got_x - expected_x) > 0.1 or abs(got_y - expected_y) > 0.1:
            print(
                f"  {_Y}Entities: test entity {entity_id} position mismatch: "
                f"expected ({expected_x}, {expected_y}), got ({got_x}, {got_y}){_0}"
            )
            return False
    return True


def cmd_json(output_dir: str, use_cache: bool = False) -> None:
    """Download version, layout, and grid_tiers JSON to output_dir."""
    os.makedirs(output_dir, exist_ok=True)

    print(f"Fetching version...")
    ver_dest = os.path.join(output_dir, VERSION_API.file_name)
    if use_cache and os.path.exists(ver_dest):
        with open(ver_dest, encoding="utf-8") as f:
            ver_raw = json.load(f)
        print(f"  {_G}Version (cached){_0}")
    else:
        ver_raw = download_json(VERSION_API.format())
        if ver_raw is None:
            print(f"  {_R}Failed to fetch version{_0}")
            raise SystemExit(1)
        _save_json(ver_raw, ver_dest, sort_dict=True)

    ver_list = ver_raw.get("data", {}).get("list", [])
    if not ver_list:
        print(f"  {_R}No versions in response{_0}")
        raise SystemExit(1)

    version = ver_list[0]["version"]
    print(f"  {_G}Latest Version: {_C}{version}{_0}")

    # Download entities data
    print(f"Downloading entities...")
    entities_url = ENTITIES_API.format(version=version)
    entities_dest = os.path.join(output_dir, ENTITIES_API.file_name)
    if not _download_json_cached(entities_url, entities_dest, use_cache):
        print(f"  {_R}Failed to fetch entities data{_0}")
        raise SystemExit(1)

    entities_table = EntitiesTable.load(entities_dest)
    print(f"  {_G}Entities: totally {_C}{len(entities_table.regions)}{_G} regions{_0}")
    if test_entities_data(entities_table):
        print(f"  {_G}Entities: data passed regression tests{_0}")
    else:
        print(f"  {_R}Entities: data failed regression tests{_0}")
        print(f"    {_Y}Source URL:{_0} {entities_url}")
        print(f"    {_Y}Please report this issue to the upstream data provider{_0}")
        raise SystemExit(1)

    # Download grid_tiers first to discover region names
    print(f"Downloading grid_tiers...")
    grid_url = GRID_TIERS_API.format(version=version)
    grid_dest = os.path.join(output_dir, GRID_TIERS_API.file_name)
    if not _download_json_cached(grid_url, grid_dest, use_cache):
        print(f"  {_R}Failed to fetch grid_tiers{_0}")
        raise SystemExit(1)

    # Extract region names from grid_tiers + some defaults
    grid_table = GridTiersTable.load(grid_dest)
    region_names = {"base01"} | set(grid_table.region_names)
    print(f"  {_G}Regions with Tiers: {_C}{', '.join(sorted(region_names))}{_0}")

    # Download layouts
    print(f"Downloading layouts...")
    for region_name in sorted(region_names):
        fname = f"{region_name}_layout.json"
        dest = os.path.join(output_dir, fname)
        url = REGION_LAYOUT_API.format(version=version, map_name=region_name)
        ok = _download_json_cached(url, dest, use_cache)
        print(f"    {_C}{fname}: {f'{_G}success{_0}' if ok else f'{_Y}failed{_0}'}")


# ── image subcommand ──────────────────────────────────────────────────────────


def load_layouts(layout_dir: str) -> dict[str, RegionLayoutTable]:
    """Load all *_layout.json files from layout_dir, returns region_name -> layout."""
    layouts: dict[str, RegionLayoutTable] = {}
    for fname in os.listdir(layout_dir):
        m = _RE_LAYOUT_FILE.match(fname)
        if not m:
            continue
        region_name = m.group(1)
        try:
            layouts[region_name] = RegionLayoutTable.load(
                os.path.join(layout_dir, fname)
            )
        except Exception as e:
            print(f"  {_Y}Warning: failed to load {fname}: {e}{_0}")
    return layouts


def split_levels(
    canvas: np.ndarray,
    layout: RegionLayoutTable,
) -> dict[str, np.ndarray]:
    """Crop level sub-images from a full-resolution region canvas.

    Layout coordinates match the unscaled region image; do not pre-scale them.
    Caller should scale crops on save if a downscaled output is desired.
    """
    result: dict[str, np.ndarray] = {}
    for level_key, lv in layout.levels.items():
        result[f"{level_key}.png"] = canvas[
            lv.y : lv.y + lv.height, lv.x : lv.x + lv.width
        ]
    return result


def _scale_image(img: np.ndarray, factor: float) -> np.ndarray:
    if factor == 1.0:
        return img
    return cv2.resize(
        img,
        (round(img.shape[1] * factor), round(img.shape[0] * factor)),
        interpolation=cv2.INTER_AREA if factor < 1.0 else cv2.INTER_LINEAR,
    )


def _save_image(img: np.ndarray, dest: str, *, scale: float | None = None) -> None:
    img_scaled = _scale_image(img, scale) if scale is not None else img
    os.makedirs(os.path.dirname(dest), exist_ok=True)
    cv2.imwrite(
        dest,
        img_scaled,
        [cv2.IMWRITE_PNG_COMPRESSION, 6] if dest.lower().endswith(".png") else None,
    )


def _format_image_repr(
    path_or_name: str, img: np.ndarray, size_bytes: int | None = None
) -> str:
    name = os.path.basename(path_or_name)
    result = f"{_C}{name} {_A}({img.shape[1]}x{img.shape[0]})"
    if size_bytes is not None:
        result += (
            f" {size_bytes / 1024 / 1024:.1f} MB"
            if size_bytes >= 1024**2
            else f" {size_bytes / 1024:.1f} KB"
        )
    return f"{result}{_0}"


def cmd_image(
    input_dir: str,
    output_dir: str,
    match: str | None = None,
    use_cache: bool = False,
    no_tiers: bool = False,
) -> None:
    """Download region images, split into levels, save to output_dir."""
    print(f"  Loading layouts from {_C}{input_dir}{_0}...")
    layouts = load_layouts(input_dir)
    print(f"  {len(layouts)} layout(s) loaded.")

    # Track which regions were processed for tier downloading
    processed_regions: list[str] = []

    print(f"\nDownloading regions...")
    for region_name, layout in layouts.items():
        if match and match not in region_name:
            print(f"  {_A}{region_name}: filtered out{_0}")
            continue

        region_path = os.path.join(output_dir, f"{region_name}.png")
        print(f"\n  [{region_name}]")

        if use_cache and os.path.exists(region_path):
            print(f"  {_G}Loading region image from cache...{_0}")
            canvas = cv2.imread(region_path, cv2.IMREAD_UNCHANGED)
            if canvas is None:
                print(f"  {_Y}Failed to read cached image{_0}")
                continue
        else:
            result = download_image(REGION_IMAGE_API.format(region_name=region_name))
            if result is None:
                print(f"  {_Y}{region_name}: download failed{_0}")
                continue
            canvas, size = result
            _save_image(canvas, region_path)
            print(f"    {_format_image_repr(region_path, canvas, size)}")

        processed_regions.append(region_name)

        for fname, level_img in split_levels(canvas, layout).items():
            dest = os.path.join(output_dir, fname)
            scaled = _scale_image(level_img, SCALE_MAP_FACTOR)
            _save_image(scaled, dest)
            print(f"    Cropped {_format_image_repr(fname, scaled)}")

    # Download tier images after all regions are processed
    if not no_tiers and processed_regions:
        print(f"\nDownloading tier images...")
        grid_tiers_path = os.path.join(input_dir, GRID_TIERS_API.file_name)
        if not os.path.exists(grid_tiers_path):
            print(f"  {_Y}grid_tiers.json not found in {input_dir}, skipping tiers{_0}")
        else:
            grid_table = GridTiersTable.load(grid_tiers_path)
            tier_count = 0
            for position_key, tier_data in grid_table.tiers.items():
                # position_key: "region_level_gx_gy"
                print(f"\n  [{position_key}]")
                region_name = position_key.split("_")[0]
                if region_name not in processed_regions:
                    continue

                for tier_name in tier_data.items.values():
                    dest = os.path.join(output_dir, f"{tier_name}.png")

                    if use_cache and os.path.exists(dest):
                        continue

                    url = TIER_IMAGE_API.format(tier_image_full_name=tier_name)
                    result = download_image(url)
                    if result is None:
                        continue
                    img, size = result
                    _save_image(img, dest, scale=SCALE_MAP_FACTOR)
                    print(f"    {_format_image_repr(dest, img, size)}")
                    tier_count += 1

            print(f"\n  {_G}Downloaded {tier_count} tier image(s){_0}")


# ── version subcommand ────────────────────────────────────────────────────────


def cmd_version(input_file: str) -> None:
    """Parse the first version entry from a version JSON file and print version info."""
    try:
        with open(input_file, encoding="utf-8") as f:
            raw = json.load(f)
    except (json.JSONDecodeError, OSError) as e:
        print(f"  {_R}Failed to read file: {e}{_0}")
        raise SystemExit(1)

    try:
        ver_list = raw["data"]["list"]
        entry = ver_list[0]
        version = entry["version"]
        game_version = entry["game_version"]
        res_version = entry["resource_bundles"][0]["res_version"]
    except (TypeError, KeyError, IndexError):
        print(f"  {_R}Not a valid version file{_0}")
        raise SystemExit(1)

    print(version)
    print(game_version)
    print(res_version)


# ── main ──────────────────────────────────────────────────────────────────────


def main():
    parser = argparse.ArgumentParser(
        description="MapFetcher - download map data and images"
    )
    parser.add_argument(
        "--with-cache", action="store_true", help="Reuse existing local JSON files"
    )
    sub = parser.add_subparsers(dest="command", required=True)

    # json
    p_json = sub.add_parser(
        "json", help="Download JSON data (version, layouts, grid_tiers)"
    )
    p_json.add_argument(
        "-o", "--output-dir", required=True, help="Output directory for JSON files"
    )

    # image
    p_img = sub.add_parser("image", help="Download region images and split into levels")
    p_img.add_argument(
        "-i", "--input-dir", required=True, help="Directory with layout JSON files"
    )
    p_img.add_argument(
        "-o", "--output-dir", required=True, help="Output directory for images"
    )
    p_img.add_argument(
        "--match", type=str, default=None, help="Only process matching regions"
    )
    p_img.add_argument(
        "--no-tiers",
        action="store_true",
        help="Only download region images, skip tier images",
    )

    # version
    p_ver = sub.add_parser(
        "version", help="Parse version info from a version JSON file"
    )
    p_ver.add_argument(
        "-i", "--input-file", required=True, help="Path to version JSON file"
    )

    args = parser.parse_args()

    if args.command == "version":
        cmd_version(args.input_file)
    else:
        print(f"{_G}MapFetcher{_0} [{args.command}]")
        if args.command == "json":
            cmd_json(args.output_dir, args.with_cache)
        elif args.command == "image":
            cmd_image(
                args.input_dir,
                args.output_dir,
                args.match,
                args.with_cache,
                args.no_tiers,
            )
        print(f"\n{_G}Done.{_0}")


if __name__ == "__main__":
    main()
