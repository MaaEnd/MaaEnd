# /// script
# requires-python = ">=3.12"
# dependencies = [
#     "opencv-python>=4",
#     "PyMaxflow>=1.3",
# ]
# ///

# MapGenerator - Generate map assets from map_fetcher output.
# Subcommands: distinguish_levels, tidy_tiers, bbox.

import os
import re
import json
import numpy as np
from collections import defaultdict
from typing import Dict, List, Tuple
from _internal.core_utils import _R, _G, _Y, _C, _A, _0, Drawer, cv2
from _internal.zmdmap_schemas import RegionLayoutTable, LevelLayoutMetaData

SCALE_MAP_FACTOR = 0.1625
"""Scale factor to convert *unscaled coordinates* to *converted coordinates*."""

DISCARD_THRESHOLD = 2
"""Pixels with brightness < this value are discarded as non-land."""

MAX_GRAPH_CUT_NODES = 2_000_000
"""Maximum number of pixels used by a single automatic graph cut."""

LAND_THRESHOLD = 64
"""Pixels with brightness < this value are filtered out of bounding boxes."""

_RE_LAYOUT_FILE = re.compile(r"^(\w+\d+)_layout\.json$")


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


class DistinMapPage:
    """Distinguishes level maps into separate maps using layout data for positioning."""

    def __init__(
        self, input_dir: str, output_dir: str, layout_dir: str, ui: bool = False
    ):
        self.input_dir = input_dir
        self.output_dir = output_dir
        self.layout_dir = layout_dir
        self.ui = ui
        self.window_name = "MapTracker Level Distinguisher"
        self.window_w, self.window_h = 1280, 720

    def _load_layouts(self) -> dict[str, RegionLayoutTable]:
        """Load all *_layout.json files from layout_dir."""
        layouts: dict[str, RegionLayoutTable] = {}
        for fname in os.listdir(self.layout_dir):
            m = _RE_LAYOUT_FILE.match(fname)
            if not m:
                continue
            region_name = m.group(1)
            try:
                layouts[region_name] = RegionLayoutTable.load(
                    os.path.join(self.layout_dir, fname)
                )
            except Exception as e:
                print(f"  {_Y}Warning: failed to load {fname}: {e}{_0}")
        return layouts

    def _load_level_maps(self) -> Dict[str, np.ndarray]:
        """Load level images (files containing '_lv') from input directory.
        Images are immediately converted to 3-channel RGB so all downstream
        code can assume a uniform (H, W, 3) uint8 format.
        """
        maps: Dict[str, np.ndarray] = {}
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

    @staticmethod
    def _map_group_key(name: str) -> str:
        """Extract the region prefix from a level name.
        E.g. 'map01_lv002' -> 'map01', 'base03_lv001' -> 'base03'.
        """
        idx = name.find("_lv")
        return name[:idx] if idx > 0 else name

    def _make_land_alpha(self, img: np.ndarray) -> np.ndarray:
        """Return a copy of img with non-land pixels set to alpha=0.
        Prevents black backgrounds from erasing other maps during compositing."""
        out = cv2.cvtColor(img, cv2.COLOR_RGB2RGBA)
        out[~self._content_mask(img), 3] = 0
        return out

    def _composite_canvas(
        self,
        maps: Dict[str, np.ndarray],
        positions: Dict[str, tuple],
        canvas_h: int,
        canvas_w: int,
    ) -> np.ndarray:
        """Composite all maps onto a blank RGBA canvas and return it."""
        canvas = np.zeros((canvas_h, canvas_w, 4), dtype=np.uint8)
        canvas[:, :, 3] = 255
        drawer = Drawer(canvas)
        for nm in sorted(positions, key=lambda n: positions[n]):
            x, y = positions[nm]
            drawer.paste(self._make_land_alpha(maps[nm]), (x, y), with_alpha=True)
        return canvas

    def _distinguish_group(
        self,
        group_key: str,
        maps: Dict[str, np.ndarray],
        layout: RegionLayoutTable,
    ) -> None:
        """Distinguish a single group of maps using layout positions."""
        print(f"\n{_G}[{group_key}]{_0} Processing {len(maps)} map(s)...")

        if SCALE_MAP_FACTOR != 1.0:
            layout = scale_layout(layout, SCALE_MAP_FACTOR)

        positions: Dict[str, Tuple[int, int]] = {}
        for level_key, lv in layout.levels.items():
            if level_key in maps:
                positions[level_key] = (lv.x, lv.y)

        names_list = list(positions.keys())
        canvas_w = layout.canvas_width
        canvas_h = layout.canvas_height

        print(f"  Compositing onto {canvas_w} x {canvas_h} canvas...")
        for nm in sorted(positions, key=lambda n: positions[n]):
            x, y = positions[nm]
            print(f"    {_C}{nm}{_0} -> ({x}, {y})")
        canvas = self._composite_canvas(maps, positions, canvas_h, canvas_w)

        # Remove islands
        maps = self._remove_islands(maps)

        # Recomposite canvas after island removal
        canvas = self._composite_canvas(maps, positions, canvas_h, canvas_w)

        # Automatic split: separates overlapping maps with graph cuts
        self._auto_split(maps, positions, names_list, canvas)

    @staticmethod
    def _brightness_weight(gray: np.ndarray) -> np.ndarray:
        gray_f = gray.astype(np.float32)
        weight = np.ones_like(gray_f, dtype=np.float32)

        mask = gray_f <= 32
        weight[mask] = 0.0

        mask = (gray_f > 32) & (gray_f < 96)
        weight[mask] = (gray_f[mask] - 32.0) / 64.0 * 0.5

        return weight

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
        comp = component[y1:y2, x1:x2]
        cost = weights[y1:y2, x1:x2]
        h, w = comp.shape

        seed_kernel = np.ones((3, 3), dtype=np.uint8)
        first_seed_full = cv2.dilate(
            touches_first.astype(np.uint8), seed_kernel, iterations=1
        ).astype(bool)
        second_seed_full = cv2.dilate(
            touches_second.astype(np.uint8), seed_kernel, iterations=1
        ).astype(bool)
        first_seed = first_seed_full[y1:y2, x1:x2] & comp
        second_seed = second_seed_full[y1:y2, x1:x2] & comp

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
        inf_capacity = max(1_000_000.0, float(len(ys)) * max_pairwise * 20.0)

        def edge_capacity(a: float, b: float) -> float:
            return (a + b) * 0.5

        for node in node_ids[first_seed]:
            graph.add_tedge(int(node), inf_capacity, 0.0)
        for node in node_ids[second_seed]:
            graph.add_tedge(int(node), 0.0, inf_capacity)

        for y in range(h):
            for x in range(w):
                if not comp[y, x]:
                    continue
                node = int(node_ids[y, x])
                if x + 1 < w and comp[y, x + 1]:
                    capacity = edge_capacity(float(cost[y, x]), float(cost[y, x + 1]))
                    graph.add_edge(node, int(node_ids[y, x + 1]), capacity, capacity)
                if y + 1 < h and comp[y + 1, x]:
                    capacity = edge_capacity(float(cost[y, x]), float(cost[y + 1, x]))
                    graph.add_edge(node, int(node_ids[y + 1, x]), capacity, capacity)

        graph.maxflow()
        segments = np.zeros((h, w), dtype=bool)
        segments[comp] = [graph.get_segment(int(node)) for node in node_ids[comp]]

        first_value = bool(segments[first_seed][0])
        second_value = bool(segments[second_seed][0])
        if first_value == second_value:
            return None

        first_side = np.zeros_like(component, dtype=bool)
        second_side = np.zeros_like(component, dtype=bool)
        first_side[y1:y2, x1:x2] = comp & (segments == first_value)
        second_side[y1:y2, x1:x2] = comp & (segments == second_value)
        return first_side, second_side

    def _auto_split(
        self,
        maps: Dict[str, np.ndarray],
        positions: Dict[str, Tuple[int, int]],
        names_list: List[str],
        canvas: np.ndarray,
    ) -> None:
        print(f"\n  {_G}Automatic split mode{_0}")

        canvas_h, canvas_w = canvas.shape[:2]
        n_maps = len(names_list)
        land_masks: List[np.ndarray] = []
        canvas_maps: List[np.ndarray] = []

        for nm in names_list:
            img = maps[nm]
            px, py = positions[nm]
            h, w = img.shape[:2]
            ey = min(py + h, canvas_h)
            ex = min(px + w, canvas_w)

            canvas_img = np.zeros((canvas_h, canvas_w, 3), dtype=np.uint8)
            canvas_img[py:ey, px:ex] = img[: ey - py, : ex - px]
            canvas_maps.append(canvas_img)

            mask = np.zeros((canvas_h, canvas_w), dtype=bool)
            mask[py:ey, px:ex] = self._content_mask(img)[: ey - py, : ex - px]
            land_masks.append(mask)

        hit_count = np.zeros((canvas_h, canvas_w), dtype=np.uint8)
        for mask in land_masks:
            hit_count += mask.astype(np.uint8)

        owner = np.full((canvas_h, canvas_w), -1, dtype=np.int16)
        for i, mask in enumerate(land_masks):
            owner[mask & (hit_count == 1)] = i
        overlap = hit_count >= 2
        owner[overlap] = -2

        if not overlap.any():
            print(f"    {_G}No overlaps, exporting maps as-is.{_0}")
            self._export_split_maps(
                maps,
                positions,
                names_list,
                [m.astype(np.uint8) for m in land_masks],
                canvas,
            )
            return

        combined_gray = np.zeros((canvas_h, canvas_w), dtype=np.uint8)
        center_factor_sum = np.zeros((canvas_h, canvas_w), dtype=np.float32)
        center_factor_count = np.zeros((canvas_h, canvas_w), dtype=np.uint8)
        yy, xx = np.indices((canvas_h, canvas_w), dtype=np.float32)
        for nm, canvas_img, mask in zip(names_list, canvas_maps, land_masks):
            gray = cv2.cvtColor(canvas_img, cv2.COLOR_RGB2GRAY)
            combined_gray[mask] = np.maximum(combined_gray[mask], gray[mask])

            px, py = positions[nm]
            h, w = maps[nm].shape[:2]
            cx = px + w * 0.5
            cy = py + h * 0.5
            radius = max((w * w + h * h) ** 0.5 * 0.5, 1.0)
            distance_ratio = np.minimum(np.hypot(xx - cx, yy - cy) / radius, 1.0)
            factor = 2.0 - distance_ratio * 1.5
            center_factor_sum[mask] += factor[mask]
            center_factor_count[mask] += 1

        center_factor = np.ones((canvas_h, canvas_w), dtype=np.float32)
        covered = center_factor_count > 0
        center_factor[covered] = (
            center_factor_sum[covered] / center_factor_count[covered]
        )
        combined_gray = cv2.GaussianBlur(combined_gray, (5, 5), 0)
        weights = np.minimum(
            (self._brightness_weight(combined_gray) + 1e-3) * center_factor, 1.0
        )

        cross_kernel = cv2.getStructuringElement(cv2.MORPH_CROSS, (3, 3))
        exclusive_masks = [(owner == i) for i in range(n_maps)]

        for first in range(n_maps):
            for second in range(first + 1, n_maps):
                pair_overlap = land_masks[first] & land_masks[second] & (hit_count == 2)
                pair_overlap &= owner == -2
                if not pair_overlap.any():
                    continue

                n_pair_cc, pair_cc_labels = cv2.connectedComponents(
                    pair_overlap.astype(np.uint8), connectivity=4
                )
                for cc_id in range(1, n_pair_cc):
                    cc_mask = pair_cc_labels == cc_id
                    ring = cv2.dilate(
                        cc_mask.astype(np.uint8), cross_kernel, iterations=1
                    ).astype(bool)
                    ring &= ~cc_mask
                    touches_first = ring & exclusive_masks[first]
                    touches_second = ring & exclusive_masks[second]

                    cut = self._graph_cut_component(
                        cc_mask,
                        weights,
                        touches_first,
                        touches_second,
                    )
                    if cut is not None:
                        first_side, second_side = cut
                        owner[first_side] = first
                        owner[second_side] = second

        unresolved = (owner == -2) & overlap
        if unresolved.any():
            n_fb_cc, fb_cc_labels = cv2.connectedComponents(
                unresolved.astype(np.uint8), connectivity=4
            )
            for cc_id in range(1, n_fb_cc):
                cc_mask = fb_cc_labels == cc_id
                involved = [
                    i for i, mask in enumerate(land_masks) if (mask & cc_mask).any()
                ]
                best_dist = np.full((canvas_h, canvas_w), np.inf, dtype=np.float32)
                best_owner = np.full((canvas_h, canvas_w), -1, dtype=np.int16)
                for i in involved:
                    if not exclusive_masks[i].any():
                        continue
                    dist_map = cv2.distanceTransform(
                        (~exclusive_masks[i]).astype(np.uint8), cv2.DIST_L2, 3
                    )
                    better = cc_mask & (dist_map < best_dist)
                    best_dist[better] = dist_map[better]
                    best_owner[better] = i
                owner[cc_mask & (best_owner >= 0)] = best_owner[
                    cc_mask & (best_owner >= 0)
                ]

        unresolved = (owner == -2) & overlap
        if unresolved.any():
            for i in sorted(range(n_maps), key=lambda idx: names_list[idx]):
                assign = unresolved & land_masks[i]
                owner[assign] = i
                unresolved &= ~assign

        ownership_masks = [(owner == i).astype(np.uint8) for i in range(n_maps)]
        print(f"    {_G}Auto split complete.{_0}")
        self._export_split_maps(
            maps, positions, names_list, ownership_masks, canvas, weights
        )

    def _remove_islands(self, maps: Dict[str, np.ndarray]) -> Dict[str, np.ndarray]:
        """Remove island pixels from each map.

        For each map, land pixels connected to the center region (within 5%
        of width/height from the center) are kept as the "continent".
        All other disconnected land clusters are considered islands and set to black.
        """
        print(f"\n  {_G}Removing islands...{_0}")
        result: Dict[str, np.ndarray] = {}

        for nm, img in maps.items():
            h, w = img.shape[:2]
            land = self._content_mask(img).astype(np.uint8)

            # Connected components
            n_labels, labels = cv2.connectedComponents(land, connectivity=4)

            # Center region: 5% margin around center
            cx, cy = w // 2, h // 2
            margin_x = max(1, int(w * 0.05))
            margin_y = max(1, int(h * 0.05))
            center_region = labels[
                cy - margin_y : cy + margin_y + 1,
                cx - margin_x : cx + margin_x + 1,
            ]

            # Collect all component labels that touch the center region
            center_labels = set(np.unique(center_region)) - {0}

            if not center_labels:
                # Fallback: keep everything if center has no land
                print(f"    {_Y}{nm}: no land at center, keeping all{_0}")
                result[nm] = img.copy()
                continue

            # Build continent mask: only components connected to center
            continent = np.isin(labels, list(center_labels)).astype(np.uint8)

            # Count removed island pixels
            island_pixels = np.count_nonzero(land) - np.count_nonzero(continent)

            if island_pixels > 0:
                # Zero out island pixels
                out = img.copy()
                island_mask = (land > 0) & (continent == 0)
                out[island_mask] = 0
                print(
                    f"    {_C}{nm}{_0}: removed {island_pixels} island pixels "
                    f"({n_labels - 1 - len(center_labels)} component(s))"
                )
                result[nm] = out
            else:
                result[nm] = img.copy()

        return result

    def _export_split_maps(
        self,
        maps: Dict[str, np.ndarray],
        positions: Dict[str, Tuple[int, int]],
        names_list: List[str],
        ownership_masks: List[np.ndarray],
        canvas: np.ndarray,
        weights: np.ndarray | None = None,
    ) -> None:
        """Export each map using its ownership mask."""
        canvas_h, canvas_w = canvas.shape[:2]
        canvas_rgb = canvas[:, :, :3]
        box_kernel = np.ones((3, 3), dtype=np.uint8)

        def _show(frame_rgb: np.ndarray, title_text: str) -> None:
            """Resize to fit window, add title text, display until keypress.
            frame_rgb is in RGB format; converts to BGR for cv2 display."""
            ch_v, cw_v = frame_rgb.shape[:2]
            s = min(self.window_w / cw_v, self.window_h / ch_v, 1.0)
            disp = cv2.resize(
                frame_rgb,
                (int(cw_v * s), int(ch_v * s)),
                interpolation=cv2.INTER_LINEAR,
            )
            # Embed in black window frame so size is always consistent
            out = np.zeros((self.window_h, self.window_w, 3), dtype=np.uint8)
            ox = (self.window_w - disp.shape[1]) // 2
            oy = (self.window_h - disp.shape[0]) // 2
            out[oy : oy + disp.shape[0], ox : ox + disp.shape[1]] = disp
            cv2.putText(
                out,
                title_text,
                (8, 18),
                cv2.FONT_HERSHEY_SIMPLEX,
                0.5,
                (225, 225, 225),
                1,
                cv2.LINE_AA,
            )
            cv2.putText(
                out,
                "Press any key to continue...",
                (8, self.window_h - 12),
                cv2.FONT_HERSHEY_SIMPLEX,
                0.5,
                (255, 255, 0),
                1,
                cv2.LINE_AA,
            )
            cv2.namedWindow(self.window_name)
            cv2.imshow(self.window_name, cv2.cvtColor(out, cv2.COLOR_RGB2BGR))
            cv2.waitKey(0)

        for i, nm in enumerate(names_list):
            mask = ownership_masks[i]  # uint8, 0/1
            ys, xs = np.nonzero(mask)
            if len(ys) == 0:
                print(f"    {_Y}{nm}: no pixels assigned, skipped{_0}")
                continue

            y1, y2 = int(ys.min()), int(ys.max()) + 1
            x1, x2 = int(xs.min()), int(xs.max()) + 1

            img = maps[nm]
            px, py = positions[nm]
            h, w = img.shape[:2]
            ey = min(py + h, canvas_h)
            ex = min(px + w, canvas_w)

            saved = img.copy()
            local_owned = mask[py:ey, px:ex]
            saved[: ey - py, : ex - px][local_owned == 0] = 0
            out_path = os.path.join(self.output_dir, f"{nm}.png")
            cv2.imwrite(out_path, cv2.cvtColor(saved, cv2.COLOR_RGB2BGR))
            print(f"    {_C}{nm}{_0}: bbox=[{x1},{y1}]-[{x2},{y2}]")

        # ---- final combined overview ----
        overview = (canvas_rgb.astype(np.float32) * 0.25).astype(np.uint8)
        owner_all = np.full((canvas_h, canvas_w), -1, dtype=np.int16)
        for i, mask in enumerate(ownership_masks):
            owner_all[mask > 0] = i
        hsv_colors = np.zeros((len(names_list), 1, 3), dtype=np.uint8)
        hsv_colors[:, 0, 0] = np.linspace(
            0, 180, len(names_list), endpoint=False, dtype=np.uint8
        )
        hsv_colors[:, 0, 1] = 220
        hsv_colors[:, 0, 2] = 255
        colors = cv2.cvtColor(hsv_colors, cv2.COLOR_HSV2RGB)[:, 0, :]
        for i in range(len(names_list)):
            owned_bool = ownership_masks[i].astype(bool)
            overview[owned_bool] = (
                canvas_rgb[owned_bool].astype(np.float32) * 0.35
                + colors[i].astype(np.float32) * 0.65
            ).astype(np.uint8)
        # White boundaries
        for i in range(len(names_list)):
            region_i = (owner_all == i).astype(np.uint8)
            dilated = cv2.dilate(region_i, box_kernel, iterations=1)
            overview[(dilated > 0) & (owner_all != i) & (owner_all >= 0)] = (
                255,
                255,
                255,
            )

        # Label each region with its map name
        for i, nm in enumerate(names_list):
            ys2, xs2 = np.nonzero(ownership_masks[i])
            if len(ys2):
                cy_lbl, cx_lbl = int(ys2.mean()), int(xs2.mean())
                cv2.putText(
                    overview,
                    nm,
                    (cx_lbl, cy_lbl),
                    cv2.FONT_HERSHEY_SIMPLEX,
                    1.0,
                    (255, 255, 255),
                    1,
                    cv2.LINE_AA,
                )

        print(f"  {_G}Split maps saved to {self.output_dir}{_0}")
        if not self.ui:
            return
        if weights is not None:
            weight_value = np.clip(weights, 0.0, 1.0)
            weight_rgb = np.zeros((canvas_h, canvas_w, 3), dtype=np.uint8)
            weight_value[weight_value < 0.01] = 0.0
            mid = (weight_value > 0.0) & (weight_value < 1.0)
            weight_rgb[mid, 0] = ((1.0 - weight_value[mid]) * 255.0).astype(np.uint8)
            weight_rgb[mid, 1] = (weight_value[mid] * 255.0).astype(np.uint8)
            weight_rgb[weight_value >= 1.0] = (255, 255, 255)
            _show(weight_rgb, "Brightness weight")
        _show(overview, f"Overview: {len(names_list)} level maps")
        if cv2.getWindowProperty(self.window_name, cv2.WND_PROP_VISIBLE) >= 1:
            cv2.destroyWindow(self.window_name)

    def run(self) -> None:
        """Main flow - groups maps by region and distinguishes each separately."""
        print(f"\n{_G}MapTracker Level Distinguisher{_0}")
        print(f"  Source dir  : {_C}{self.input_dir}{_0}")
        print(f"  Output dir  : {_C}{self.output_dir}{_0}")
        print(f"  Layout dir  : {_C}{self.layout_dir}{_0}")
        print(f"  Scale       : {_C}{SCALE_MAP_FACTOR}{_0}")

        ensure_output_dir(self.output_dir)

        # Load layouts
        print(f"\nLoading layouts...")
        layouts = self._load_layouts()
        if not layouts:
            print(f"{_Y}No layout files found in {self.layout_dir}{_0}")
            return
        print(f"  {len(layouts)} layout(s) loaded.")

        # Load level images
        all_maps = self._load_level_maps()
        if not all_maps:
            print(f"{_Y}No level maps found in {self.input_dir}{_0}")
            return

        # Group level images by matching layout keys
        groups: Dict[str, Dict[str, np.ndarray]] = defaultdict(dict)
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


def generate_map_bbox_json(input_dir: str, output_dir: str) -> str:
    """Generate map bbox json for all map png files in directory recursively."""
    ensure_output_dir(output_dir)
    results: Dict[str, List[int]] = {}

    for root, _, files in os.walk(input_dir):
        for file in files:
            if not file.endswith(".png"):
                continue
            if file.startswith("_"):
                continue
            map_name = os.path.splitext(file)[0]
            img_path = os.path.join(root, file)
            img = cv2.imread(img_path, cv2.IMREAD_UNCHANGED)
            if img is None:
                continue

            if img.ndim == 2:
                rgb = cv2.cvtColor(img, cv2.COLOR_GRAY2RGB)
            elif img.shape[2] == 3:
                rgb = cv2.cvtColor(img, cv2.COLOR_BGR2RGB)
            else:
                continue

            brightness = np.mean(rgb, axis=2).astype(np.uint8)
            brightness = cv2.GaussianBlur(brightness, (5, 5), 0)
            ys, xs = np.where(brightness >= LAND_THRESHOLD)
            if len(ys) == 0 or len(xs) == 0:
                continue

            min_x, max_x = int(xs.min()), int(xs.max())
            min_y, max_y = int(ys.min()), int(ys.max())
            results[map_name] = [min_x, min_y, max_x + 1, max_y + 1]

    output_path = os.path.join(output_dir, "map_bbox_data.json")
    with open(output_path, "w", encoding="utf-8") as f:
        json.dump(results, f, indent=4, ensure_ascii=False)
    print(f"{_G}Saved map rectangles to {output_path}{_0}")
    return output_path


def cmd_distinguish_levels(
    input_dir: str, output_dir: str, layout_dir: str, ui: bool = False
) -> None:
    """Distinguish level images into separate maps with island removal and automatic split."""
    if not os.path.isdir(input_dir):
        print(f"{_R}Input directory not found: {input_dir}{_0}")
        return
    if not os.path.isdir(layout_dir):
        print(f"{_R}Layout directory not found: {layout_dir}{_0}")
        return

    distinguisher = DistinMapPage(input_dir, output_dir, layout_dir, ui)
    distinguisher.run()


def cmd_bbox(input_dir: str, output_dir: str) -> None:
    """Generate bounding box JSON for map images."""
    if not os.path.isdir(input_dir):
        print(f"{_R}Input directory not found: {input_dir}{_0}")
        return

    generate_map_bbox_json(input_dir, output_dir)


# Tier image filename format: region_level_gx_gy_tier_id.png
_RE_TIER_FILE = re.compile(r"^(\w+_\w+)_(\d+)_(\d+)_tier_\d+\.png$")

GRID_XY_SIZE = SCALE_MAP_FACTOR * 600
"""Scaled pixel size of one grid cell."""

RING_RADIUS = 40
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
        # Load tier image as RGBA
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

        # Calculate tier position on canvas (gx, gy are 1-indexed)
        # Anchor is bottom-left; gy counts from bottom to top
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

        # Build dilated mask on canvas for ring background
        land_canvas = np.zeros((ph, pw), dtype=np.uint8)
        land_canvas[y1:y2, x1:x2] = land_crop.astype(np.uint8)
        ring_mask = cv2.dilate(land_canvas, dilate_kernel).astype(bool)

        # Draw: ring background (parent at 0.25 opacity) + alpha-blended tier
        canvas = np.zeros((ph, pw, 3), dtype=np.uint8)
        canvas[ring_mask] = np.clip(
            parent_rgb[ring_mask].astype(np.float32) * 0.25, 0, 255
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
        description="MapTracker merger - distinguish levels, tidy tiers, generate bounding boxes"
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
        "--layout-dir", required=True, help="Directory containing *_layout.json files"
    )
    p_distin.add_argument(
        "--ui", action="store_true", help="Show visual preview windows while exporting"
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
        cmd_distinguish_levels(
            args.input_dir, args.output_dir, args.layout_dir, args.ui
        )
    elif args.command == "tidy_tiers":
        cmd_tidy_tiers(args.input_dir, args.output_dir)
    elif args.command == "bbox":
        cmd_bbox(args.input_dir, args.output_dir)


if __name__ == "__main__":
    main()
