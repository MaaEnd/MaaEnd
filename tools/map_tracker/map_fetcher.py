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
from _internal.zmdmap_schemas import RegionLayoutTable, GridTiersTable
from _internal.http_utils import download_image, download_json


class APIEndpoint(NamedTuple):
    re_url: str  # Reversed string for anti searching purposes
    file_name: str

    def format(self, **kwargs) -> str:
        return self.re_url[::-1].format(**kwargs)


VERSION_API = APIEndpoint(
    re_url="noisrev/dleifdne/1v/ipa/moc.pamdmz.ipa//:sptth",
    file_name="version.json",
)

GRID_TIERS_API = APIEndpoint(
    re_url="nosj.sreit_dirg/}noisrev{/ytitne/atad/moc.pamdmz.stessa//:sptth",
    file_name="grid_tiers.json",
)

REGION_LAYOUT_API = APIEndpoint(
    re_url="nosj.tuoyal_}eman_pam{/}noisrev{/ytitne/atad/moc.pamdmz.stessa//:sptth",
    file_name="{map_name}_layout.json",
)

REGION_IMAGE_API = APIEndpoint(
    re_url="gnp.}eman_noiger{/war/pam/segami/moc.pamdmz.stessa//:sptth",
    file_name="{region_name}.png",
)

TIER_IMAGE_API = APIEndpoint(
    re_url="gnp.}eman_lluf_egami_reit{/reit/segami/moc.pamdmz.stessa//:sptth",
    file_name="{tier_image_full_name}.png",
)

SCALE_MAP_FACTOR = 0.1625
"""Scale factor to convert *unscaled coordinates* to *converted coordinates*."""

_RE_LAYOUT_FILE = re.compile(r"^(\w+\d+)_layout\.json$")
"""Regex to match remote layout JSON file names.

Groups:
1. region_name
"""


def _save_json(
    data: dict, dest: str, *, no_minify_if_size_less_than: int = 4 * 1024
) -> None:
    with open(dest, "w", encoding="utf-8") as f:
        json_str = json.dumps(
            data, ensure_ascii=False, indent=None, separators=(",", ":")
        )
        if len(json_str) < no_minify_if_size_less_than:
            json_str = json.dumps(data, ensure_ascii=False, indent=4)
        f.write(json_str)


def _scale_image(img: np.ndarray, factor: float) -> np.ndarray:
    if factor == 1.0:
        return img
    return cv2.resize(
        img,
        (round(img.shape[1] * factor), round(img.shape[0] * factor)),
        interpolation=cv2.INTER_AREA if factor < 1.0 else cv2.INTER_LINEAR,
    )


def _download_json_cached(url: str, dest: str, use_cache: bool = False) -> bool:
    """Download JSON from url to dest. Skips download if use_cache and file exists."""
    if use_cache and os.path.exists(dest):
        return True
    data = download_json(url)
    if data is None:
        return False
    _save_json(data, dest)
    return True


# ── json subcommand ───────────────────────────────────────────────────────────


def cmd_json(output_dir: str, use_cache: bool = False) -> None:
    """Download version, layout, and grid_tiers JSON to output_dir."""
    os.makedirs(output_dir, exist_ok=True)

    print(f"  Fetching version...")
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
        _save_json(ver_raw, ver_dest)

    ver_list = ver_raw.get("data", {}).get("list", [])
    if not ver_list:
        print(f"  {_R}No versions in response{_0}")
        raise SystemExit(1)

    version = ver_list[0]["version"]
    print(f"  {_G}Latest Version: {_C}{version}{_0}")

    # Download grid_tiers first to discover region names
    print(f"  Downloading grid_tiers...")
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
    print(f"  Downloading layouts...")
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
    scale: float = 1.0,
) -> dict[str, np.ndarray]:
    """Split region image into individual level images. Returns filename -> image."""
    s = lambda v: round(v * scale)
    result: dict[str, np.ndarray] = {}
    for level_key, lv in layout.levels.items():
        sx, sy = s(lv.x), s(lv.y)
        sw, sh = s(lv.width), s(lv.height)
        result[f"{level_key}.png"] = canvas[sy : sy + sh, sx : sx + sw]
    return result


def download_region(region_name: str) -> tuple[np.ndarray, int] | None:
    """Download complete region image from server."""
    url = REGION_IMAGE_API.format(region_name=region_name)
    return download_image(url)


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

    os.makedirs(output_dir, exist_ok=True)

    # Track which regions were processed for tier downloading
    processed_regions: list[str] = []

    print(f"\n  Downloading regions...")
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
            result = download_region(region_name)
            if result is None:
                print(f"  {_Y}{region_name}: download failed{_0}")
                continue
            canvas, size = result
            print(f"  {_G}Downloaded {size / 1024 / 1024:.2f} MB")
            canvas = _scale_image(canvas, SCALE_MAP_FACTOR)
            cv2.imwrite(region_path, canvas)
            print(
                f"    {_C}{region_name}.png {_A}({canvas.shape[1]}x{canvas.shape[0]}){_0}"
            )

        processed_regions.append(region_name)
        print(f"  Canvas size: {canvas.shape[1]}x{canvas.shape[0]}")

        for fname, level_img in split_levels(canvas, layout, SCALE_MAP_FACTOR).items():
            cv2.imwrite(os.path.join(output_dir, fname), level_img)
            print(
                f"    {_C}{fname} {_A}({level_img.shape[1]}x{level_img.shape[0]}){_0}"
            )

    # Download tier images after all regions are processed
    if not no_tiers and processed_regions:
        print(f"\n  Downloading tier images...")
        grid_tiers_path = os.path.join(input_dir, GRID_TIERS_API.file_name)
        if not os.path.exists(grid_tiers_path):
            print(f"  {_Y}grid_tiers.json not found in {input_dir}, skipping tiers{_0}")
        else:
            grid_table = GridTiersTable.load(grid_tiers_path)
            tier_count = 0
            for position_key, tier_data in grid_table.tiers.items():
                # position_key: "region_level_gx_gy"
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
                    img = _scale_image(img, SCALE_MAP_FACTOR)
                    cv2.imwrite(dest, img)
                    print(
                        f"    {_C}{tier_name}.png {_A}({img.shape[1]}x{img.shape[0]}){_0}"
                    )
                    tier_count += 1

            print(f"  {_G}Downloaded {tier_count} tier image(s){_0}")


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

    args = parser.parse_args()

    print(f"{_G}MapFetcher{_0} [{args.command}]")

    if args.command == "json":
        cmd_json(args.output_dir, args.with_cache)
    elif args.command == "image":
        cmd_image(
            args.input_dir, args.output_dir, args.match, args.with_cache, args.no_tiers
        )

    print(f"\n{_G}Done.{_0}")


if __name__ == "__main__":
    main()
