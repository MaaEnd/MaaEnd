# /// script
# requires-python = ">=3.12"
# dependencies = [
#     "opencv-python>=4",
#     "PyMaxflow>=1.3",
# ]
# ///

# MapGenerator - Generate map assets from map_fetcher output.
# Subcommands: distinguish_levels, attach_icons, tidy_tiers, bbox.

import os
import re
import json
import numpy as np
from collections import defaultdict
from _internal.core_utils import _R, _G, _Y, _C, _0, Drawer, MapName, cv2
from _internal.zmdmap_schemas import (
    EntitiesTable,
    LevelLayoutMetaData,
    RegionLayoutTable,
)

SCALE_MAP_FACTOR = 0.1625
"""Scale factor to convert *unscaled coordinates* to *converted coordinates*."""

DISCARD_THRESHOLD = 2
"""Pixels with brightness < this value are discarded as non-land."""

MAX_GRAPH_CUT_NODES = 2_000_000
"""Maximum number of pixels used by a single automatic graph cut."""

LAND_THRESHOLD = 64
"""Pixels with brightness < this value are filtered out of bounding boxes."""

CUT_DARK_FLOOR = 0.2
"""Minimum pairwise capacity for gray <= 32 (dark shelf).

>0 reduces free-wandering dark seams; too high over-shrinks shelves.
"""

FEATHER_KSIZE = 17
"""Odd kernel size for Gaussian edge feathering (ownership + tier ring)."""

FEATHER_SIGMA = 4.0
"""Gaussian sigma for edge feathering (ownership + tier ring)."""

_RE_LAYOUT_FILE = re.compile(r"^(\w+\d+)_layout\.json$")


def _feather_mask(mask: np.ndarray, ksize: int, sigma: float) -> np.ndarray:
    """Gaussian-blur a binary mask into soft alpha in [0, 1]."""
    soft = cv2.GaussianBlur(mask.astype(np.float32), (ksize, ksize), sigma)
    return np.clip(soft, 0.0, 1.0)


def scale_layout(layout: RegionLayoutTable, factor: float) -> RegionLayoutTable:
    """Scale layout pixel dimensions by factor."""
    s = lambda v: round(v * factor)
    return RegionLayoutTable(
        base_map=layout.base_map,
        canvas_width=s(layout.canvas_width),
        canvas_height=s(layout.canvas_height),
        tile_w=s(layout.tile_w),
        tile_h=s(layout.tile_h),
        levels={
            k: LevelLayoutMetaData(
                x=s(lv.x),
                y=s(lv.y),
                width=s(lv.width),
                height=s(lv.height),
                tile_w=s(lv.tile_w),
                tile_h=s(lv.tile_h),
            )
            for k, lv in layout.levels.items()
        },
    )


def ensure_output_dir(path: str) -> None:
    os.makedirs(path, exist_ok=True)


class LevelMapDistinguisher:
    """Distinguishes level maps into separate maps using layout data for positioning."""

    def __init__(self, input_dir: str, output_dir: str, data_dir: str):
        self.input_dir = input_dir
        self.output_dir = output_dir
        self.data_dir = data_dir

    def _load_layouts(self) -> dict[str, RegionLayoutTable]:
        """Load all *_layout.json files from data_dir."""
        layouts: dict[str, RegionLayoutTable] = {}
        for fname in os.listdir(self.data_dir):
            m = _RE_LAYOUT_FILE.match(fname)
            if not m:
                continue
            region_name = m.group(1)
            try:
                layouts[region_name] = RegionLayoutTable.load(
                    os.path.join(self.data_dir, fname)
                )
            except Exception as e:
                print(f"  {_Y}Warning: failed to load {fname}: {e}{_0}")
        return layouts

    def _load_level_maps(self) -> dict[str, np.ndarray]:
        """Load level images (files containing '_lv') from input directory.
        Images are immediately converted to 3-channel RGB so all downstream
        code can assume a uniform (H, W, 3) uint8 format.
        """
        maps: dict[str, np.ndarray] = {}
        for fname in sorted(os.listdir(self.input_dir)):
            if not fname.endswith(".png"):
                continue
            if fname.startswith("_"):
                continue
            if "_lv" not in fname:
                continue
            name = fname[:-4]
            path = os.path.join(self.input_dir, fname)
            img = cv2.imread(path, cv2.IMREAD_UNCHANGED)
            if img is None:
                continue
            if img.ndim == 2:
                img = cv2.cvtColor(img, cv2.COLOR_GRAY2RGB)
            elif img.shape[2] == 4:
                # Alpha blend RGBA onto black background
                rgb = img[:, :, :3].astype(np.float32)
                alpha = img[:, :, 3:4].astype(np.float32) / 255.0
                img = (rgb * alpha).astype(np.uint8)
                img = cv2.cvtColor(img, cv2.COLOR_BGR2RGB)
            else:
                img = cv2.cvtColor(img, cv2.COLOR_BGR2RGB)
            maps[name] = img
        return maps

    @staticmethod
    def _content_mask(img: np.ndarray) -> np.ndarray:
        """Binary mask of land pixels (gray >= DISCARD_THRESHOLD)."""
        gray = cv2.cvtColor(img, cv2.COLOR_RGB2GRAY)
        return gray >= DISCARD_THRESHOLD

    def _distinguish_group(
        self,
        group_key: str,
        maps: dict[str, np.ndarray],
        layout: RegionLayoutTable,
    ) -> None:
        """Distinguish a single group of maps using layout positions."""
        print(f"\n{_G}[{group_key}]{_0} Processing {len(maps)} map(s)...")

        if SCALE_MAP_FACTOR != 1.0:
            layout = scale_layout(layout, SCALE_MAP_FACTOR)

        positions: dict[str, tuple[int, int]] = {}
        for level_key, lv in layout.levels.items():
            if level_key in maps:
                positions[level_key] = (lv.x, lv.y)

        names_list = list(positions.keys())
        canvas_w = layout.canvas_width
        canvas_h = layout.canvas_height

        print(f"  Canvas size: {canvas_w} x {canvas_h}")
        for nm in sorted(positions, key=lambda n: positions[n]):
            x, y = positions[nm]
            print(f"    {_C}{nm}{_0} -> ({x}, {y})")

        maps = self._remove_islands(maps)
        self._split_overlaps(maps, positions, names_list, canvas_h, canvas_w)

    @staticmethod
    def _brightness_weight(gray: np.ndarray) -> np.ndarray:
        """n-link capacity: dark shelf floors at CUT_DARK_FLOOR, bright → 1."""
        g = gray.astype(np.float32)
        w = np.ones_like(g, dtype=np.float32)
        w[g <= 32] = CUT_DARK_FLOOR
        mid = (g > 32) & (g < 96)
        w[mid] = CUT_DARK_FLOOR + (g[mid] - 32.0) / 64.0 * (0.5 - CUT_DARK_FLOOR)
        return w

    @staticmethod
    def _largest_cc_mask(mask: np.ndarray) -> np.ndarray:
        """Largest 4-connected component of a binary mask."""
        n_labels, labels = cv2.connectedComponents(
            mask.astype(np.uint8), connectivity=4
        )
        if n_labels <= 1:
            return np.zeros_like(mask, dtype=bool)
        sizes = np.bincount(labels.ravel())
        sizes[0] = 0
        return labels == int(sizes.argmax())

    @staticmethod
    def _cc_labels_touching(labels: np.ndarray, seed: np.ndarray) -> set[int]:
        return set(np.unique(labels[seed])) - {0}

    @staticmethod
    def _assign_nearest_seed(
        target: np.ndarray,
        seed_masks: list[np.ndarray],
        cover_masks: list[np.ndarray] | None = None,
    ) -> np.ndarray:
        """Nearest exclusive seed; prefer maps that cover the pixel."""
        h, w = target.shape
        best_dist = np.full((h, w), np.inf, dtype=np.float32)
        best_owner = np.full((h, w), -1, dtype=np.int16)

        def fill(region: np.ndarray, covers: list[np.ndarray] | None) -> None:
            if not region.any():
                return
            for i, seed in enumerate(seed_masks):
                if not seed.any():
                    continue
                area = region if covers is None else region & covers[i]
                if not area.any():
                    continue
                dist = cv2.distanceTransform((~seed).astype(np.uint8), cv2.DIST_L2, 3)
                better = area & (dist < best_dist)
                best_dist[better] = dist[better]
                best_owner[better] = i

        fill(target, cover_masks)
        if cover_masks is not None:
            fill(target & (best_owner < 0), None)  # any nearest seed
        return best_owner

    @staticmethod
    def _graph_cut_component(
        component: np.ndarray,
        weights: np.ndarray,
        touches_first: np.ndarray,
        touches_second: np.ndarray,
    ) -> tuple[np.ndarray, np.ndarray] | None:
        ys, xs = np.nonzero(component)
        if len(ys) == 0 or len(ys) > MAX_GRAPH_CUT_NODES:
            return None

        y1, y2 = int(ys.min()), int(ys.max()) + 1
        x1, x2 = int(xs.min()), int(xs.max()) + 1
        comp, cost = component[y1:y2, x1:x2], weights[y1:y2, x1:x2]
        h, w = comp.shape
        k = np.ones((3, 3), dtype=np.uint8)

        def seed_from(touches: np.ndarray) -> np.ndarray:
            s = (
                cv2.dilate(touches.astype(np.uint8), k, iterations=1).astype(bool)[
                    y1:y2, x1:x2
                ]
                & comp
            )
            return s

        first_seed, second_seed = seed_from(touches_first), seed_from(touches_second)
        conflict = first_seed & second_seed
        if conflict.any():
            first_seed &= ~conflict
            second_seed &= ~conflict
        if not first_seed.any() or not second_seed.any():
            return None

        import maxflow

        node_ids = np.full((h, w), -1, dtype=np.int32)
        graph = maxflow.GraphFloat()
        nodes = graph.add_nodes(int(comp.sum()))
        node_ids[comp] = nodes
        max_pairwise = float(np.max(cost[comp]))
        inf_cap = max(1_000_000.0, float(len(ys)) * max_pairwise * 20.0)

        for node in node_ids[first_seed]:
            graph.add_tedge(int(node), inf_cap, 0.0)
        for node in node_ids[second_seed]:
            graph.add_tedge(int(node), 0.0, inf_cap)

        for y in range(h):
            for x in range(w):
                if not comp[y, x]:
                    continue
                node = int(node_ids[y, x])
                if x + 1 < w and comp[y, x + 1]:
                    cap = (float(cost[y, x]) + float(cost[y, x + 1])) * 0.5
                    graph.add_edge(node, int(node_ids[y, x + 1]), cap, cap)
                if y + 1 < h and comp[y + 1, x]:
                    cap = (float(cost[y, x]) + float(cost[y + 1, x])) * 0.5
                    graph.add_edge(node, int(node_ids[y + 1, x]), cap, cap)

        graph.maxflow()
        segments = np.zeros((h, w), dtype=bool)
        segments[comp] = [graph.get_segment(int(n)) for n in node_ids[comp]]
        fv, sv = bool(segments[first_seed][0]), bool(segments[second_seed][0])
        if fv == sv:
            return None

        first_side = np.zeros_like(component, dtype=bool)
        second_side = np.zeros_like(component, dtype=bool)
        first_side[y1:y2, x1:x2] = comp & (segments == fv)
        second_side[y1:y2, x1:x2] = comp & (segments == sv)
        return first_side, second_side

    def _split_overlaps(
        self,
        maps: dict[str, np.ndarray],
        positions: dict[str, tuple[int, int]],
        names_list: list[str],
        canvas_h: int,
        canvas_w: int,
    ) -> None:
        print(f"\n  {_G}Splitting overlaps...{_0}")
        n_maps = len(names_list)
        land_masks: list[np.ndarray] = []
        aligned_maps: list[np.ndarray] = []

        for nm in names_list:
            img = maps[nm]
            px, py = positions[nm]
            h, w = img.shape[:2]
            ey, ex = min(py + h, canvas_h), min(px + w, canvas_w)
            aligned_image = np.zeros((canvas_h, canvas_w, 3), dtype=np.uint8)
            aligned_image[py:ey, px:ex] = img[: ey - py, : ex - px]
            aligned_maps.append(aligned_image)
            mask = np.zeros((canvas_h, canvas_w), dtype=bool)
            mask[py:ey, px:ex] = self._content_mask(img)[: ey - py, : ex - px]
            land_masks.append(mask)

        hit_count = sum(m.astype(np.uint8) for m in land_masks)
        owner = np.full((canvas_h, canvas_w), -1, dtype=np.int16)
        for i, mask in enumerate(land_masks):
            owner[mask & (hit_count == 1)] = i
        owner[hit_count >= 2] = -2

        # Only the largest exclusive continent is a real seed.
        for i, nm in enumerate(names_list):
            exclusive = owner == i
            if not exclusive.any():
                continue
            demoted = exclusive & ~self._largest_cc_mask(exclusive)
            n = int(demoted.sum())
            if n == 0:
                continue
            owner[demoted] = -2
            print(
                f"    {_C}{nm}{_0}: demoted {n} exclusive seed fragments to unresolved"
            )

        exclusive_masks = [(owner == i) for i in range(n_maps)]
        if not (owner == -2).any():
            print(f"    {_G}No overlaps, exporting maps as-is.{_0}")
            self._export_split_maps(
                maps,
                positions,
                names_list,
                [m.astype(np.uint8) for m in exclusive_masks],
            )
            return

        # Brightness-weighted pairwise graph-cut + mild center preference.
        combined_gray = np.zeros((canvas_h, canvas_w), dtype=np.uint8)
        c_sum = np.zeros((canvas_h, canvas_w), dtype=np.float32)
        c_cnt = np.zeros((canvas_h, canvas_w), dtype=np.uint8)
        yy, xx = np.indices((canvas_h, canvas_w), dtype=np.float32)
        for nm, aligned_image, mask in zip(names_list, aligned_maps, land_masks):
            gray = cv2.cvtColor(aligned_image, cv2.COLOR_RGB2GRAY)
            combined_gray[mask] = np.maximum(combined_gray[mask], gray[mask])
            px, py = positions[nm]
            mh, mw = maps[nm].shape[:2]
            cx, cy = px + mw * 0.5, py + mh * 0.5
            radius = max((mw * mw + mh * mh) ** 0.5 * 0.5, 1.0)
            factor = 2.0 - np.minimum(np.hypot(xx - cx, yy - cy) / radius, 1.0) * 1.5
            c_sum[mask] += factor[mask]
            c_cnt[mask] += 1
        center = np.ones((canvas_h, canvas_w), dtype=np.float32)
        covered = c_cnt > 0
        center[covered] = c_sum[covered] / c_cnt[covered]
        combined_gray = cv2.GaussianBlur(combined_gray, (5, 5), 0)
        weights = np.minimum(
            (self._brightness_weight(combined_gray) + 1e-3) * center, 1.0
        )

        cross = cv2.getStructuringElement(cv2.MORPH_CROSS, (3, 3))
        for a in range(n_maps):
            for b in range(a + 1, n_maps):
                pair = land_masks[a] & land_masks[b] & (hit_count == 2) & (owner == -2)
                if not pair.any():
                    continue
                n_cc, labs = cv2.connectedComponents(
                    pair.astype(np.uint8), connectivity=4
                )
                for cid in range(1, n_cc):
                    cc = labs == cid
                    ring = cv2.dilate(cc.astype(np.uint8), cross, 1).astype(bool)
                    ring &= ~cc
                    cut = self._graph_cut_component(
                        cc,
                        weights,
                        ring & exclusive_masks[a],
                        ring & exclusive_masks[b],
                    )
                    if cut is not None:
                        sa, sb = cut
                        owner[sa], owner[sb] = a, b

        unresolved = owner == -2
        if unresolved.any():
            assigned = self._assign_nearest_seed(
                unresolved, exclusive_masks, land_masks
            )
            hit = unresolved & (assigned >= 0)
            owner[hit] = assigned[hit]
            unresolved = owner == -2
            if unresolved.any():
                for i in sorted(range(n_maps), key=lambda k: names_list[k]):
                    take = unresolved & land_masks[i]
                    owner[take] = i
                    unresolved &= ~take

        ownership_masks = self._enforce_seed_connectivity(
            owner, exclusive_masks, land_masks, names_list
        )
        print(f"    {_G}Overlap split complete.{_0}")
        self._export_split_maps(maps, positions, names_list, ownership_masks)

    def _enforce_seed_connectivity(
        self,
        owner: np.ndarray,
        exclusive_masks: list[np.ndarray],
        land_masks: list[np.ndarray],
        names_list: list[str],
    ) -> list[np.ndarray]:
        """Keep ownership 4-connected to each exclusive seed; grow-reclaim orphans."""
        n_maps = len(names_list)
        kept = np.full(owner.shape, -1, dtype=np.int16)

        for i, nm in enumerate(names_list):
            seed, owned = exclusive_masks[i], owner == i
            if not seed.any():
                continue
            _, labels = cv2.connectedComponents(
                (owned | seed).astype(np.uint8), connectivity=4
            )
            seed_ids = self._cc_labels_touching(labels, seed)
            if not seed_ids:
                kept[seed] = i
                continue
            connected = np.isin(labels, list(seed_ids))
            kept[connected] = i
            dropped = int((owned & ~connected).sum())
            if dropped > 0:
                print(
                    f"    {_C}{nm}{_0}: dropped {dropped} pixels not connected "
                    f"to exclusive seed"
                )

        orphan = (owner >= 0) & (kept < 0)
        cross = cv2.getStructuringElement(cv2.MORPH_CROSS, (3, 3))
        while orphan.any():
            progressed = False
            for i in range(n_maps):
                continent = kept == i
                if not continent.any():
                    continue
                frontier = cv2.dilate(
                    continent.astype(np.uint8), cross, iterations=1
                ).astype(bool)
                claim = frontier & orphan & land_masks[i]
                if not claim.any():
                    continue
                kept[claim] = i
                orphan[claim] = False
                progressed = True
            if not progressed:
                break

        leftover = int(orphan.sum())
        if leftover > 0:
            print(
                f"    {_Y}Left {leftover} land pixels unowned "
                f"(unreachable from any seed continent){_0}"
            )
        return [(kept == i).astype(np.uint8) for i in range(n_maps)]

    def _remove_islands(self, maps: dict[str, np.ndarray]) -> dict[str, np.ndarray]:
        """Remove land not connected to the center 5% region of each map."""
        print(f"\n  {_G}Removing islands...{_0}")
        result: dict[str, np.ndarray] = {}
        for nm, img in maps.items():
            h, w = img.shape[:2]
            land = self._content_mask(img).astype(np.uint8)
            n_labels, labels = cv2.connectedComponents(land, connectivity=4)
            mx, my = max(1, int(w * 0.05)), max(1, int(h * 0.05))
            cx, cy = w // 2, h // 2
            center = np.zeros_like(land, dtype=bool)
            center[cy - my : cy + my + 1, cx - mx : cx + mx + 1] = True
            seed_ids = self._cc_labels_touching(labels, center)
            if not seed_ids:
                print(f"    {_Y}{nm}: no land at center, keeping all{_0}")
                result[nm] = img.copy()
                continue
            continent = np.isin(labels, list(seed_ids))
            island = (land > 0) & ~continent
            n_island = int(island.sum())
            if n_island == 0:
                result[nm] = img.copy()
                continue
            out = img.copy()
            out[island] = 0
            print(
                f"    {_C}{nm}{_0}: removed {n_island} island pixels "
                f"({n_labels - 1 - len(seed_ids)} component(s))"
            )
            result[nm] = out
        return result

    def _export_split_maps(
        self,
        maps: dict[str, np.ndarray],
        positions: dict[str, tuple[int, int]],
        names_list: list[str],
        ownership_masks: list[np.ndarray],
    ) -> None:
        """Export each map with feathered ownership edges."""
        canvas_h, canvas_w = ownership_masks[0].shape
        for i, nm in enumerate(names_list):
            mask = ownership_masks[i]
            ys, xs = np.nonzero(mask)
            if len(ys) == 0:
                print(f"    {_Y}{nm}: no pixels assigned, skipped{_0}")
                continue
            img = maps[nm]
            px, py = positions[nm]
            h, w = img.shape[:2]
            ey, ex = min(py + h, canvas_h), min(px + w, canvas_w)
            saved = img.copy()
            alpha = _feather_mask(mask[py:ey, px:ex], FEATHER_KSIZE, FEATHER_SIGMA)
            region = saved[: ey - py, : ex - px].astype(np.float32)
            region *= alpha[..., None]
            saved[: ey - py, : ex - px] = np.clip(region, 0, 255).astype(np.uint8)
            cv2.imwrite(
                os.path.join(self.output_dir, f"{nm}.png"),
                cv2.cvtColor(saved, cv2.COLOR_RGB2BGR),
            )
            print(
                f"    {_C}{nm}{_0}: bbox="
                f"[{int(xs.min())},{int(ys.min())}]-"
                f"[{int(xs.max()) + 1},{int(ys.max()) + 1}]"
            )

        print(f"  {_G}Split maps saved to {self.output_dir}{_0}")

    def run(self) -> None:
        """Main flow - groups maps by region and distinguishes each separately."""
        print(f"\n{_G}MapTracker Level Distinguisher{_0}")
        print(f"  Source dir  : {_C}{self.input_dir}{_0}")
        print(f"  Output dir  : {_C}{self.output_dir}{_0}")
        print(f"  Data dir    : {_C}{self.data_dir}{_0}")
        print(f"  Scale       : {_C}{SCALE_MAP_FACTOR}{_0}")

        ensure_output_dir(self.output_dir)

        # Load layouts
        print(f"\nLoading layouts...")
        layouts = self._load_layouts()
        if not layouts:
            print(f"{_Y}No layout files found in {self.data_dir}{_0}")
            return
        print(f"  {len(layouts)} layout(s) loaded.")

        # Load level images
        all_maps = self._load_level_maps()
        if not all_maps:
            print(f"{_Y}No level maps found in {self.input_dir}{_0}")
            return

        groups: dict[str, dict[str, np.ndarray]] = defaultdict(dict)
        for nm, img in all_maps.items():
            for region_name, layout in layouts.items():
                if nm in layout.levels:
                    groups[region_name][nm] = img
                    break

        print(
            f"  Loaded {len(all_maps)} level map(s) "
            f"in {len(groups)} group(s): "
            + ", ".join(f"{_C}{k}{_0}" for k in sorted(groups))
        )

        for group_key in sorted(groups):
            group_maps = groups[group_key]
            layout = layouts[group_key]
            if len(group_maps) < 2:
                print(f"\n{_Y}[{group_key}]{_0} Only 1 map – skipping.")
                continue
            self._distinguish_group(group_key, group_maps, layout)


def cmd_distinguish_levels(input_dir: str, output_dir: str, data_dir: str) -> None:
    """Distinguish level images with island removal and overlap splitting."""
    if not os.path.isdir(input_dir):
        print(f"{_R}Input directory not found: {input_dir}{_0}")
        return
    if not os.path.isdir(data_dir):
        print(f"{_R}Data directory not found: {data_dir}{_0}")
        return

    distinguisher = LevelMapDistinguisher(input_dir, output_dir, data_dir)
    distinguisher.run()


def cmd_bbox(input_dir: str, output_dir: str) -> None:
    """Generate bounding box JSON for map images."""
    if not os.path.isdir(input_dir):
        print(f"{_R}Input directory not found: {input_dir}{_0}")
        return
    ensure_output_dir(output_dir)

    results: dict[str, list[int]] = {}
    for root, _, files in os.walk(input_dir):
        for file in files:
            if not file.endswith(".png") or file.startswith("_"):
                continue

            img = cv2.imread(os.path.join(root, file), cv2.IMREAD_UNCHANGED)
            if img is not None and img.ndim == 3:
                if img.shape[2] == 4:
                    rgb = cv2.cvtColor(img, cv2.COLOR_BGRA2RGB)
                elif img.shape[2] == 3:
                    rgb = cv2.cvtColor(img, cv2.COLOR_BGR2RGB)
                else:
                    continue
            else:
                continue

            brightness = np.mean(rgb, axis=2).astype(np.uint8)
            brightness = cv2.GaussianBlur(brightness, (5, 5), 0)
            ys, xs = np.where(brightness >= LAND_THRESHOLD)
            if len(ys) == 0 or len(xs) == 0:
                continue

            min_x, max_x = int(xs.min()), int(xs.max())
            min_y, max_y = int(ys.min()), int(ys.max())

            map_name = os.path.splitext(file)[0]
            results[map_name] = [min_x, min_y, max_x + 1, max_y + 1]

    output_path = os.path.join(output_dir, "map_bbox_data.json")
    with open(output_path, "w", encoding="utf-8") as f:
        results = dict(sorted(results.items()))
        json.dump(results, f, indent=4, ensure_ascii=False)
    print(f"{_G}Saved map rectangles to {output_path}{_0}")


# Tier image filename format: region_level_gx_gy_tier_id.png
_RE_TIER_FILE = re.compile(r"^(\w+_\w+)_(\d+)_(\d+)_tier_\d+\.png$")

GRID_XY_SIZE = SCALE_MAP_FACTOR * 600
"""Scaled pixel size of one grid cell."""

RING_RADIUS = 42
"""Radius of the ring background around land areas."""


def _load_image_rgb(path: str) -> np.ndarray | None:
    """Load image and convert to RGB."""
    img = cv2.imread(path, cv2.IMREAD_UNCHANGED)
    if img is None:
        return None
    if img.ndim == 2:
        return cv2.cvtColor(img, cv2.COLOR_GRAY2RGB)
    if img.shape[2] == 4:
        alpha = img[:, :, 3:4].astype(np.float32) / 255.0
        bgr = img[:, :, :3].astype(np.float32) * alpha
        return cv2.cvtColor(np.clip(bgr, 0, 255).astype(np.uint8), cv2.COLOR_BGR2RGB)
    return cv2.cvtColor(img, cv2.COLOR_BGR2RGB)


def _load_image_rgba(path: str) -> np.ndarray | None:
    """Load image and convert to RGBA."""
    img = cv2.imread(path, cv2.IMREAD_UNCHANGED)
    if img is None:
        return None
    if img.ndim == 2:
        return cv2.cvtColor(img, cv2.COLOR_GRAY2RGBA)
    if img.shape[2] == 3:
        return cv2.cvtColor(img, cv2.COLOR_BGR2RGBA)
    if img.shape[2] == 4:
        return cv2.cvtColor(img, cv2.COLOR_BGRA2RGBA)
    return None


CAMPFIRE_ICON_PATH = "assets/resource/image/MapTracker/MiniMapIcons/IconCampfire.png"
CAMPFIRE_KEY_NAME = "int_campfire_v2"


def cmd_attach_icons(
    input_dir: str,
    output_dir: str,
    data_dir: str,
) -> None:
    """Attach campfire icons to non-tier map images."""
    if not os.path.isdir(input_dir):
        print(f"{_R}Input directory not found: {input_dir}{_0}")
        return
    if not os.path.isdir(data_dir):
        print(f"{_R}Data directory not found: {data_dir}{_0}")
        return
    entities_file = os.path.join(data_dir, "maaend_entities.json")
    if not os.path.isfile(entities_file):
        print(f"{_R}Entities file not found: {entities_file}{_0}")
        return

    icon = cv2.imread(CAMPFIRE_ICON_PATH, cv2.IMREAD_UNCHANGED)
    if icon is None:
        print(f"{_R}Campfire icon not found: {CAMPFIRE_ICON_PATH}{_0}")
        return
    if icon.ndim == 2 or icon.shape[2] == 3:
        icon = cv2.cvtColor(icon, cv2.COLOR_BGR2BGRA)

    entities = EntitiesTable.load(entities_file)
    ensure_output_dir(output_dir)
    ih, iw = icon.shape[:2]
    processed = 0
    placed = 0

    for fname in sorted(os.listdir(input_dir)):
        if not fname.endswith(".png") or fname.startswith("_"):
            continue
        try:
            map_name = MapName.parse(fname)
        except ValueError:
            continue
        if map_name.map_type == "tier":
            continue

        region = entities.regions.get(map_name.map_id)
        level = region.levels.get(map_name.map_level_id) if region else None
        if level is None:
            print(f"  {_Y}{fname}: entity data not found, skipped{_0}")
            continue

        path = os.path.join(input_dir, fname)
        img = cv2.imread(path, cv2.IMREAD_UNCHANGED)
        if img is None:
            print(f"  {_Y}Failed to load {fname}{_0}")
            continue
        if img.ndim == 2 or img.shape[2] == 3:
            img = cv2.cvtColor(img, cv2.COLOR_BGR2BGRA)

        drawer = Drawer(img)
        map_placed = 0
        for entity in level.categories.get("special", []):
            if entity.key_name != CAMPFIRE_KEY_NAME:
                continue
            px, py = (int(round(value)) for value in entity.map_location)
            drawer.paste(icon, (px - iw // 2, py - ih // 2), with_alpha=True)
            map_placed += 1

        cv2.imwrite(os.path.join(output_dir, fname), img)
        processed += 1
        placed += map_placed
        print(f"  {_C}{fname}{_0}: {map_placed} campfire icon(s) placed")

    print(
        f"\n  {_G}Done. Processed {processed} non-tier map(s), "
        f"placed {placed} campfire icon(s).{_0}"
    )


def cmd_tidy_tiers(input_dir: str, output_dir: str) -> None:
    """Blend tier images with their parent region-level images."""
    if not os.path.isdir(input_dir):
        print(f"{_R}Input directory not found: {input_dir}{_0}")
        return
    os.makedirs(output_dir, exist_ok=True)

    # Discover tier images
    tier_files: list[tuple[str, int, int, str]] = []  # (level_key, gx, gy, fname)
    for fname in os.listdir(input_dir):
        m = _RE_TIER_FILE.match(fname)
        if m:
            tier_files.append((m.group(1), int(m.group(2)), int(m.group(3)), fname))

    if not tier_files:
        print(f"{_Y}No tier images found in {input_dir}{_0}")
        return

    print(f"  Found {len(tier_files)} tier image(s).")

    region_cache: dict[str, np.ndarray] = {}
    dilate_kernel = cv2.getStructuringElement(
        cv2.MORPH_ELLIPSE, (RING_RADIUS * 2 + 1, RING_RADIUS * 2 + 1)
    )

    for level_key, gx, gy, fname in sorted(tier_files):
        tier_rgba = _load_image_rgba(os.path.join(input_dir, fname))
        if tier_rgba is None:
            print(f"  {_Y}Failed to load {fname}{_0}")
            continue

        # Load parent region-level image as RGB (cached)
        if level_key not in region_cache:
            parent_path = os.path.join(input_dir, f"{level_key}.png")
            parent_rgb = _load_image_rgb(parent_path)
            if parent_rgb is None:
                print(f"  {_Y}Parent {level_key}.png not found, skipping {fname}{_0}")
                continue
            region_cache[level_key] = parent_rgb
        parent_rgb = region_cache[level_key]

        # gx, gy are 1-indexed; anchor is bottom-left, gy counts from bottom to top
        th, tw = tier_rgba.shape[:2]
        ph, pw = parent_rgb.shape[:2]
        px = round((gx - 1) * GRID_XY_SIZE)
        py = round(ph - (gy - 1) * GRID_XY_SIZE - th)

        # Clip to canvas bounds
        x1, y1 = max(0, px), max(0, py)
        x2, y2 = min(pw, px + tw), min(ph, py + th)
        if x1 >= x2 or y1 >= y2:
            print(f"  {_Y}{fname}: outside parent bounds{_0}")
            continue
        tx1, ty1 = x1 - px, y1 - py
        tx2, ty2 = tx1 + (x2 - x1), ty1 + (y2 - y1)

        # Land mask: brightness >= threshold and alpha > 0
        tier_rgb = tier_rgba[:, :, :3]
        gray = cv2.cvtColor(tier_rgb, cv2.COLOR_RGB2GRAY)
        gray = cv2.GaussianBlur(gray, (5, 5), 0)
        land_mask = (gray >= LAND_THRESHOLD) & (tier_rgba[:, :, 3] > 0)
        land_crop = land_mask[ty1:ty2, tx1:tx2]

        # Dilate land, then Gaussian-feather the outer ring edge
        land_canvas = np.zeros((ph, pw), dtype=np.uint8)
        land_canvas[y1:y2, x1:x2] = land_crop.astype(np.uint8)
        ring_alpha = _feather_mask(
            cv2.dilate(land_canvas, dilate_kernel),
            FEATHER_KSIZE,
            FEATHER_SIGMA,
        )

        # Draw: soft ring background (parent at 0.25) + alpha-blended tier
        canvas = np.clip(
            parent_rgb.astype(np.float32) * 0.25 * ring_alpha[..., None],
            0,
            255,
        ).astype(np.uint8)

        tier_crop_rgb = tier_rgb[ty1:ty2, tx1:tx2].astype(np.float32)
        tier_crop_alpha = tier_rgba[ty1:ty2, tx1:tx2, 3:4].astype(np.float32) / 255.0
        canvas_region = canvas[y1:y2, x1:x2].astype(np.float32)
        blended = tier_crop_rgb * tier_crop_alpha + canvas_region * (
            1.0 - tier_crop_alpha
        )
        canvas[y1:y2, x1:x2] = np.clip(blended, 0, 255).astype(np.uint8)

        # Save with gx_gy removed: "map01_lv001_3_5_tier_56" -> "map01_lv001_tier_56"
        parts = fname[:-4].split("_")
        save_name = "_".join(parts[:2] + parts[-2:]) + ".png"
        cv2.imwrite(
            os.path.join(output_dir, save_name),
            cv2.cvtColor(canvas, cv2.COLOR_RGB2BGR),
        )
        print(f"    {_C}{save_name}{_0}")

    print(f"\n  {_G}Done.{_0}")


def main():
    import argparse

    parser = argparse.ArgumentParser(
        description=(
            "MapTracker merger - distinguish levels, attach icons, tidy tiers, "
            "generate bounding boxes"
        )
    )
    sub = parser.add_subparsers(dest="command", required=True)

    # distinguish_levels subcommand
    p_distin = sub.add_parser(
        "distinguish_levels", help="Distinguish level images into separate maps"
    )
    p_distin.add_argument(
        "-i", "--input-dir", required=True, help="Directory containing level images"
    )
    p_distin.add_argument(
        "-o",
        "--output-dir",
        required=True,
        help="Output directory for distinguished maps",
    )
    p_distin.add_argument(
        "--data-dir", required=True, help="Directory containing map data files"
    )

    # attach_icons subcommand
    p_icons = sub.add_parser(
        "attach_icons", help="Attach campfire icons to non-tier map images"
    )
    p_icons.add_argument(
        "-i", "--input-dir", required=True, help="Directory containing map images"
    )
    p_icons.add_argument(
        "-o", "--output-dir", required=True, help="Output directory for map images"
    )
    p_icons.add_argument(
        "--data-dir", required=True, help="Directory containing map data files"
    )
    # tidy_tiers subcommand
    p_tiers = sub.add_parser(
        "tidy_tiers", help="Blend tier images with parent region-level images"
    )
    p_tiers.add_argument(
        "-i",
        "--input-dir",
        required=True,
        help="Directory containing tier and level images",
    )
    p_tiers.add_argument(
        "-o",
        "--output-dir",
        required=True,
        help="Output directory for blended tier images",
    )

    # bbox subcommand
    p_bbox = sub.add_parser("bbox", help="Generate bounding box JSON for map images")
    p_bbox.add_argument(
        "-i", "--input-dir", required=True, help="Directory containing map images"
    )
    p_bbox.add_argument(
        "-o", "--output-dir", required=True, help="Output directory for bbox JSON"
    )

    args = parser.parse_args()

    if args.command == "distinguish_levels":
        cmd_distinguish_levels(args.input_dir, args.output_dir, args.data_dir)
    elif args.command == "attach_icons":
        cmd_attach_icons(
            args.input_dir,
            args.output_dir,
            args.data_dir,
        )
    elif args.command == "tidy_tiers":
        cmd_tidy_tiers(args.input_dir, args.output_dir)
    elif args.command == "bbox":
        cmd_bbox(args.input_dir, args.output_dir)


if __name__ == "__main__":
    main()
