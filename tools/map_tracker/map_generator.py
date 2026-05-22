# /// script
# requires-python = ">=3.12"
# dependencies = [
#     "opencv-python>=4",
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

MAP_FINAL_DIR = "assets/resource/image/MapTracker/map_final"

SCALE_MAP_FACTOR = 0.1625
"""Scale factor to convert *unscaled coordinates* to *converted coordinates*."""

DISCARD_THRESHOLD = 2
"""Pixels with brightness < this value are discarded as non-land."""

LAND_THRESHOLD = 48
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


def load_layouts(layout_dir: str) -> dict[str, RegionLayoutTable]:
    """Load all *_layout.json files from layout_dir."""
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


def ensure_output_dir(path: str) -> None:
    os.makedirs(path, exist_ok=True)
    gitignore_path = os.path.join(path, ".gitignore")
    with open(gitignore_path, "w", encoding="utf-8") as f:
        f.write("*\n")


class DistinMapPage:
    """Distinguishes level maps into separate maps using layout data for positioning."""

    def __init__(self, input_dir: str, output_dir: str, layout_dir: str):
        self.input_dir = input_dir
        self.output_dir = output_dir
        self.layout_dir = layout_dir
        self.window_name = "MapTracker Level Distinguisher"
        self.window_w, self.window_h = 1280, 720

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
    def _content_bbox(mask: np.ndarray) -> Tuple[int, int, int, int] | None:
        """Return (x1, y1, x2, y2) bounding box of True pixels, or None."""
        ys, xs = np.nonzero(mask)
        if len(ys) == 0:
            return None
        return int(xs.min()), int(ys.min()), int(xs.max()) + 1, int(ys.max()) + 1

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

        # --- Remove islands ---
        maps = self._remove_islands(maps)

        # Recomposite canvas after island removal
        canvas = self._composite_canvas(maps, positions, canvas_h, canvas_w)

        # --- Manual split: user draws barrier lines to separate maps ---
        self._manual_split(group_key, maps, positions, names_list, canvas)

    def _remove_islands(self, maps: Dict[str, np.ndarray]) -> Dict[str, np.ndarray]:
        """Remove island pixels from each map.

        For each map, land pixels connected to the center region (within
        5% of width/height from the center) are kept as the "continent".
        All other disconnected land clusters are considered islands —
        typically fragments of neighboring maps captured at the edge —
        and are set to black.
        """
        print(f"\n  {_G}Removing islands...{_0}")
        result: Dict[str, np.ndarray] = {}

        for nm, img in maps.items():
            h, w = img.shape[:2]
            land = self._content_mask(img).astype(np.uint8)

            # Connected components (4-connectivity)
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

    def _manual_split(
        self,
        group_key: str,
        maps: Dict[str, np.ndarray],
        positions: Dict[str, Tuple[int, int]],
        names_list: List[str],
        canvas: np.ndarray,
    ) -> None:
        """Let the user draw barriers to split overlapping regions, then propagate ownership.

        All logic works on binary land masks (gray > 1). Pixel colors are only
        used at the final export step.

        Controls:
          Left drag       draw barrier
          Right drag      erase barrier
          ENTER           confirm and export
          ESC             skip (each map retains its full land, overlap not split)
        """
        print(f"\n  {_G}Manual split mode{_0}")

        canvas_h, canvas_w = canvas.shape[:2]
        n_maps = len(names_list)

        # ------------------------------------------------------------------
        # Step 1: Pre-compute binary land masks on canvas for every map.
        # Each mask is dilated so that thin peninsulas / isolated edge pixels
        # are connected to the main body and do not appear as stray dots.
        # ------------------------------------------------------------------
        _land_dil_kernel = cv2.getStructuringElement(cv2.MORPH_ELLIPSE, (5, 5))
        land_masks: List[np.ndarray] = []  # each: bool (canvas_h, canvas_w)
        for nm in names_list:
            img = maps[nm]
            px, py = positions[nm]
            h, w = img.shape[:2]
            m = np.zeros((canvas_h, canvas_w), dtype=np.uint8)
            ey = min(py + h, canvas_h)
            ex = min(px + w, canvas_w)
            bin_local = self._content_mask(img)[: ey - py, : ex - px].astype(np.uint8)
            m[py:ey, px:ex] = bin_local
            # Dilate to close small gaps & connect isolated edge pixels
            m = cv2.dilate(m, _land_dil_kernel, iterations=2)
            land_masks.append(m.astype(bool))

        # overlap[y,x] = True  ↔  land in 2+ maps
        any_land = np.zeros((canvas_h, canvas_w), dtype=bool)
        multi_hit = np.zeros((canvas_h, canvas_w), dtype=bool)
        for m in land_masks:
            multi_hit |= any_land & m
            any_land |= m
        overlap = multi_hit  # pixels that need splitting

        if not overlap.any():
            print(f"    {_G}No overlaps — exporting maps as-is.{_0}")
            fin = [m.astype(np.uint8) for m in land_masks]
            self._export_split_maps(group_key, maps, positions, names_list, fin, canvas)
            return

        print(f"    Overlap pixels: {np.count_nonzero(overlap)}")

        # owner[y,x]:  -1 = non-land,  -2 = unresolved overlap,  i = map i
        owner = np.full((canvas_h, canvas_w), -1, dtype=np.int16)
        for i, m in enumerate(land_masks):
            exclusive = m & ~overlap
            owner[exclusive] = i
        owner[overlap] = -2

        print("  You're now drawing manual splitting barriers.")
        print("    LDrag=draw  RDrag=erase  ENTER=confirm  ESC=skip")

        # ------------------------------------------------------------------
        # Step 2: Interactive barrier drawing (works on canvas coordinates)
        # ------------------------------------------------------------------
        barrier = np.zeros((canvas_h, canvas_w), dtype=np.uint8)

        # Pre-compute scaled base image (done once, not every frame)
        s = min(self.window_w / canvas_w, self.window_h / canvas_h, 1.0)
        dw, dh = int(canvas_w * s), int(canvas_h * s)
        ox = (self.window_w - dw) // 2
        oy = (self.window_h - dh) // 2

        base_rgb = canvas[:, :, :3].astype(np.float32)
        base_rgb[overlap] = (
            base_rgb[overlap] * 0.35 + np.array([255, 140, 0], np.float32) * 0.65
        )
        base_scaled = cv2.resize(
            np.clip(base_rgb, 0, 255).astype(np.uint8),
            (dw, dh),
            interpolation=cv2.INTER_AREA,
        )

        drawing = [False]
        erasing = [False]
        last_pt: List[Tuple[int, int] | None] = [None]

        def to_canvas_pt(mx: int, my: int) -> Tuple[int, int]:
            return int((mx - ox) / s), int((my - oy) / s)

        def mouse_cb(event, mx, my, flags, _param):
            cx, cy = to_canvas_pt(mx, my)
            if event == cv2.EVENT_LBUTTONDOWN:
                drawing[0] = True
                last_pt[0] = (cx, cy)
                cv2.circle(barrier, (cx, cy), 1, 1, -1)
            elif event == cv2.EVENT_RBUTTONDOWN:
                erasing[0] = True
                last_pt[0] = (cx, cy)
                cv2.circle(barrier, (cx, cy), 1, 0, -1)
            elif event == cv2.EVENT_MOUSEMOVE:
                if drawing[0] and last_pt[0]:
                    cv2.line(barrier, last_pt[0], (cx, cy), 1, 3)
                    last_pt[0] = (cx, cy)
                elif erasing[0] and last_pt[0]:
                    cv2.line(barrier, last_pt[0], (cx, cy), 0, 3)
                    last_pt[0] = (cx, cy)
            elif event in (cv2.EVENT_LBUTTONUP, cv2.EVENT_RBUTTONUP):
                drawing[0] = erasing[0] = False
                last_pt[0] = None

        # Pre-allocated display frame
        frame = np.zeros((self.window_h, self.window_w, 3), dtype=np.uint8)

        def make_display() -> np.ndarray:
            frame[:] = 0
            # Copy pre-computed base into frame
            frame[oy : oy + dh, ox : ox + dw] = base_scaled
            # Overlay barrier (red) on the scaled region
            barrier_scaled = cv2.resize(
                barrier, (dw, dh), interpolation=cv2.INTER_NEAREST
            )
            barrier_mask = barrier_scaled > 0
            region = frame[oy : oy + dh, ox : ox + dw]
            region[barrier_mask] = [255, 0, 0]  # red in RGB
            cv2.putText(
                frame,
                "Operations: LeftDrag=draw  RightDrag=erase  ENTER=confirm  ESC=skip",
                (8, 18),
                cv2.FONT_HERSHEY_SIMPLEX,
                0.45,
                (220, 220, 220),
                1,
                cv2.LINE_AA,
            )
            return cv2.cvtColor(frame, cv2.COLOR_RGB2BGR)

        win = self.window_name
        cv2.namedWindow(win)
        cv2.setMouseCallback(win, mouse_cb)
        while True:
            cv2.imshow(win, make_display())
            key = cv2.waitKey(30) & 0xFF
            if key == 13:  # ENTER
                break
            elif key == 27:  # ESC
                print(
                    f"  {_Y}Splitting skipped — each map retains its full land (overlap not split).{_0}"
                )
                if cv2.getWindowProperty(win, cv2.WND_PROP_VISIBLE) >= 1:
                    cv2.destroyWindow(win)
                fin = [m.astype(np.uint8) for m in land_masks]
                self._export_split_maps(
                    group_key, maps, positions, names_list, fin, canvas
                )
                return
            elif cv2.getWindowProperty(win, cv2.WND_PROP_VISIBLE) < 1:
                break
        if cv2.getWindowProperty(win, cv2.WND_PROP_VISIBLE) >= 1:
            cv2.destroyWindow(win)

        # ------------------------------------------------------------------
        # Step 3: Barrier-aware label-then-assign
        # ------------------------------------------------------------------
        cross_kernel = cv2.getStructuringElement(cv2.MORPH_CROSS, (3, 3))
        wall = cv2.dilate(barrier, cross_kernel, iterations=1).astype(bool)
        print(f"    Barrier pixels (after dilate): {wall.sum()}")

        # Fillable = overlap pixels that are NOT wall
        fillable = (owner == -2) & ~wall
        fillable_u8 = fillable.astype(np.uint8)

        # Connected components of fillable (4-connectivity)
        n_cc, cc_labels = cv2.connectedComponents(fillable_u8, connectivity=4)
        print(f"    Fillable components: {n_cc - 1}")

        exclusive_masks = [(owner == i) for i in range(n_maps)]

        for cc_id in range(1, n_cc):
            cc_mask = (cc_labels == cc_id).astype(np.uint8)
            cc_bool = cc_mask.astype(bool)
            # Dilate to get 4-connected ring around the component
            nbr = cv2.dilate(cc_mask, cross_kernel, iterations=1).astype(bool)
            nbr &= ~cc_bool  # ring only, not inside

            # Count exclusive pixels per map that touch this component
            best_map = -1
            best_cnt = 0
            for i in range(n_maps):
                cnt = int(np.count_nonzero(nbr & exclusive_masks[i]))
                if cnt > best_cnt:
                    best_cnt = cnt
                    best_map = i

            if best_map >= 0:
                owner[cc_bool] = best_map
            else:
                best_map_dt = -1
                best_dist = np.inf
                for i in range(n_maps):
                    if not exclusive_masks[i].any():
                        continue
                    not_excl = (~exclusive_masks[i]).astype(np.uint8)
                    dist_map = cv2.distanceTransform(not_excl, cv2.DIST_L2, 3)
                    min_dist = float(dist_map[cc_bool].min())
                    if min_dist < best_dist:
                        best_dist = min_dist
                        best_map_dt = i
                if best_map_dt >= 0:
                    owner[cc_bool] = best_map_dt
                # If still not found, fallback (wall-pixel pass) handles it

        wall_unresolved = (owner == -2) & any_land
        if wall_unresolved.any():
            alpha_order = sorted(range(n_maps), key=lambda i: names_list[i])
            for i in alpha_order:
                assign = wall_unresolved & land_masks[i]
                owner[assign] = i
                wall_unresolved &= ~assign
        print(
            f"    {_G}Split complete. Still unresolved: {int((owner == -2).sum())}{_0}"
        )

        # ------------------------------------------------------------------
        # Step 4: Build final per-map binary masks from ownership array
        # ------------------------------------------------------------------
        fin = [(owner == i).astype(np.uint8) for i in range(n_maps)]

        self._export_split_maps(group_key, maps, positions, names_list, fin, canvas)

    def _export_split_maps(
        self,
        group_key: str,
        maps: Dict[str, np.ndarray],
        positions: Dict[str, Tuple[int, int]],
        names_list: List[str],
        ownership_masks: List[np.ndarray],
        canvas: np.ndarray,
    ) -> None:
        """Export each map using its ownership mask.
        After saving, shows each map's territory mask one by one.
        """
        canvas_h, canvas_w = canvas.shape[:2]
        canvas_rgb = canvas[:, :, :3]

        dimmed_bg = (canvas_rgb.astype(np.float32) * 0.25).astype(np.uint8)
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

            # Build this map's full-canvas image from its original data
            img = maps[nm]
            px, py = positions[nm]
            h, w = img.shape[:2]
            per_map = np.zeros((canvas_h, canvas_w, 3), dtype=np.uint8)
            ey = min(py + h, canvas_h)
            ex = min(px + w, canvas_w)
            per_map[py:ey, px:ex] = img[: ey - py, : ex - px]

            # Save without cropping: keep original map size, only mask ownership.
            saved = img.copy()
            local_owned = mask[py:ey, px:ex]
            saved[: ey - py, : ex - px][local_owned == 0] = 0
            out_path = os.path.join(self.output_dir, f"{nm}.png")
            cv2.imwrite(out_path, cv2.cvtColor(saved, cv2.COLOR_RGB2BGR))
            print(f"    {_C}{nm}{_0}: bbox=[{x1},{y1}]-[{x2},{y2}]")

            # ---- per-map territory display ----
            # Layer 1: grayscale dimmed canvas as background
            bg = dimmed_bg.copy()
            # Layer 2: this map's actual pixels in its owned region (full brightness)
            owned_bool = mask.astype(bool)
            bg[owned_bool] = per_map[owned_bool]
            # Layer 3: white border around the owned region
            dilated = cv2.dilate(mask, box_kernel, iterations=2)
            border = (dilated > 0) & ~owned_bool
            bg[border] = (255, 255, 255)
            # Layer 4: semi-transparent green tint over owned area
            tint = bg.copy()
            tint[owned_bool] = (
                tint[owned_bool].astype(np.float32) * 0.7
                + np.array([50, 200, 50], np.float32) * 0.3
            ).astype(np.uint8)

            _show(
                tint,
                f"[{i+1}/{len(names_list)}] {nm} | owned {int(owned_bool.sum())} px",
            )

        # ---- final combined overview ----
        overview = (canvas_rgb.astype(np.float32) * 0.35).astype(np.uint8)
        rng = np.random.RandomState(42)
        owner_all = np.full((canvas_h, canvas_w), -1, dtype=np.int16)
        for i, mask in enumerate(ownership_masks):
            owner_all[mask > 0] = i
        colors = [tuple(int(c) for c in rng.randint(80, 220, 3)) for _ in names_list]
        for i, nm in enumerate(names_list):
            owned_bool = ownership_masks[i].astype(bool)
            r, g, b = colors[i]
            overview[owned_bool] = (
                canvas_rgb[owned_bool].astype(np.float32) * 0.7
                + np.array([r, g, b], np.float32) * 0.3
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
        layouts = load_layouts(self.layout_dir)
        if not layouts:
            print(f"{_Y}No layout files found in {self.layout_dir}.{_0}")
            return
        print(f"  {len(layouts)} layout(s) loaded.")

        # Load level images
        all_maps = self._load_level_maps()
        if not all_maps:
            print(f"{_Y}No level maps found in directory.{_0}")
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

            brightness = np.mean(rgb, axis=2)
            ys, xs = np.where(brightness >= LAND_THRESHOLD)
            if len(ys) == 0 or len(xs) == 0:
                continue

            min_x, max_x = int(xs.min()), int(xs.max())
            min_y, max_y = int(ys.min()), int(ys.max())
            results[map_name] = [min_x, min_y, max_x + 1, max_y + 1]

    output_path = os.path.join(output_dir, "map_bbox.json")
    with open(output_path, "w", encoding="utf-8") as f:
        json.dump(results, f, indent=4, ensure_ascii=False)
    print(f"{_G}Saved map rectangles to {output_path}{_0}")
    return output_path


def cmd_distinguish_levels(input_dir: str, output_dir: str, layout_dir: str) -> None:
    """Distinguish level images into separate maps with island removal and manual split."""
    if not os.path.isdir(input_dir):
        print(f"{_R}Input directory not found: {input_dir}{_0}")
        return
    if not os.path.isdir(layout_dir):
        print(f"{_R}Layout directory not found: {layout_dir}{_0}")
        return

    distinguisher = DistinMapPage(input_dir, output_dir, layout_dir)
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

RING_RADIUS = 50
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
        cmd_distinguish_levels(args.input_dir, args.output_dir, args.layout_dir)
    elif args.command == "tidy_tiers":
        cmd_tidy_tiers(args.input_dir, args.output_dir)
    elif args.command == "bbox":
        cmd_bbox(args.input_dir, args.output_dir)


if __name__ == "__main__":
    main()
