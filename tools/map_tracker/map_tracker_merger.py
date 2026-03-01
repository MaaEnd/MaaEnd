# /// script
# requires-python = ">=3.12"
# dependencies = [
#     "opencv-python>=4",
# ]
# ///

# MapTracker - Merger Tool
# This tool helps merge map tile image files into complete maps.

import os
import re
import json
import shutil
import numpy as np
from collections import defaultdict, deque
from typing import Dict, List, Tuple, NamedTuple
from utils import _R, _G, _Y, _C, _A, _0, Drawer, cv2, Point, MapName


class TileInfo(NamedTuple):
    file_name: str
    file_x: int
    file_y: int
    raw_img: np.ndarray
    raw_w: int
    raw_h: int
    align_mode: str  # "auto" or "manual"
    align_direction: str  # "lt", "rt", "lb", "rb"


class MergeMapConfig(NamedTuple):
    scale: float
    flip_x: bool
    flip_y: bool
    force_size: Tuple[int, int]  # (width, height)


MAP_MERGED_DIR = "assets/resource/image/MapTracker/map_merged"
MAP_FINAL_DIR = "assets/resource/image/MapTracker/map_final"


def ensure_output_dir(path: str) -> None:
    os.makedirs(path, exist_ok=True)
    gitignore_path = os.path.join(path, ".gitignore")
    with open(gitignore_path, "w", encoding="utf-8") as f:
        f.write("*\n")


default_config = MergeMapConfig(
    scale=0.1625,
    flip_x=False,
    flip_y=True,
    force_size=(600, 600),  # Width, Height
)


class MergeMapPage:
    def __init__(self, map_types: list[str], input_dir: str, output_dir: str):
        self.map_types = map_types
        self.input_dir = input_dir
        self.output_dir = output_dir
        self.window_name = "MapTracker Merger"
        self.window_w, self.window_h = 1280, 720
        self.groups: Dict[str, Dict[Tuple[int, int], str]] = {}

        # Load and prepare data
        self._prepare_data()

    def _get_tile_pos(
        self, tx: int, ty: int, scale: float, x_offset: int, y_offset: int, max_x: int
    ) -> Point:
        """Calculate scaled tile position on the canvas."""
        sw, sh = default_config.force_size
        tile_x = x_offset + int(
            ((max_x - tx) * sw * scale)
            if default_config.flip_x
            else ((tx - 1) * sw * scale)
        )
        tile_y = y_offset + int(
            ((self.max_y - ty) * sh * scale)
            if default_config.flip_y
            else ((ty - 1) * sh * scale)
        )
        return tile_x, tile_y

    def _prepare_data(self) -> None:
        """Prepare and group tiles by base name."""
        if not self.map_types:
            raise ValueError("Invalid map types")
        if any(t not in ("normal", "tier", "base", "dung") for t in self.map_types):
            raise ValueError("Invalid map type")

        ensure_output_dir(self.output_dir)

        # Collect matching files
        groups = defaultdict(dict)
        for root, _, file_names in os.walk(self.input_dir):
            for file_name in file_names:
                parsed = self._parse_tile_file(file_name)
                if parsed is None:
                    continue
                name, x, y, parsed_type = parsed
                if parsed_type not in self.map_types:
                    continue

                file_path = os.path.join(root, file_name)
                key = (x, y)
                if key in groups[name]:
                    print(
                        f"{_Y}Warning: Duplicate tile at ({x}, {y}) for {name}, skipping{_0}"
                    )
                else:
                    groups[name][key] = file_path

        if not groups:
            print(f"{_R}No map tiles found in input directories.{_0}")
        self.groups = groups

        # Compute bounds for normal groups so tier groups can align to them
        self.normal_bounds: Dict[str, Tuple[int, int]] = {}
        for gname, tiles_dict in groups.items():
            if not self._is_tier_map_name(gname):
                gmax_x = max(x for (x, y) in tiles_dict.keys())
                gmax_y = max(y for (x, y) in tiles_dict.keys())
                self.normal_bounds[gname] = (gmax_x, gmax_y)

    @staticmethod
    def _is_tier_map_name(name: str) -> bool:
        try:
            return MapName.parse(name).map_type == "tier"
        except ValueError:
            return False

    @staticmethod
    def _parse_tile_file(file_name: str) -> tuple[str, int, int, str] | None:
        """Parse a tile file name into (group_name, x, y, map_type)."""
        try:
            parsed = MapName.parse(file_name, is_tile=True)
        except ValueError:
            return None
        group_name = os.path.splitext(parsed.map_full_name)[0]
        return group_name, int(parsed.tile_x), int(parsed.tile_y), parsed.map_type

    def _has_opaque_pixels_on_edge(
        self, img: np.ndarray, edge: str, threshold: int = 4
    ) -> bool:
        """Check if image has opaque pixels on the specified edge."""
        if edge == "left":
            return np.any(img[:, 0, 3] >= threshold)
        elif edge == "right":
            return np.any(img[:, -1, 3] >= threshold)
        elif edge == "top":
            return np.any(img[0, :, 3] >= threshold)
        elif edge == "bottom":
            return np.any(img[-1, :, 3] >= threshold)
        return False

    def _render_canvas(
        self,
        canvas: np.ndarray,
        manual_tiles: List[TileInfo],
        max_x: int,
        max_y: int,
        current_group: str,
        progress: float,
    ) -> np.ndarray:
        """Render the canvas and UI elements to the display window."""
        drawer = Drawer.new(self.window_w, self.window_h)

        if canvas is not None:
            temp_canvas = canvas.copy()

            # Apply current manual tile adjustments to display (preview)
            temp_drawer = Drawer(temp_canvas)
            for tile in manual_tiles:
                mode = tile.align_direction
                x_pos = (
                    (max_x - tile.file_x) * default_config.force_size[0]
                    if default_config.flip_x
                    else (tile.file_x - 1) * default_config.force_size[0]
                )
                y_pos = (
                    (max_y - tile.file_y) * default_config.force_size[1]
                    if default_config.flip_y
                    else (tile.file_y - 1) * default_config.force_size[1]
                )
                th, tw = tile.raw_img.shape[:2]
                sw, sh = default_config.force_size
                if mode == "lt":
                    ax, ay = x_pos, y_pos
                elif mode == "rt":
                    ax, ay = x_pos + sw - tw, y_pos
                elif mode == "lb":
                    ax, ay = x_pos, y_pos + sh - th
                elif mode == "rb":
                    ax, ay = x_pos + sw - tw, y_pos + sh - th
                else:
                    ax, ay = x_pos, y_pos
                temp_drawer.paste(tile.raw_img, (ax, ay), with_alpha=True)

            # Scale canvas to fit window, keeping aspect ratio
            ch, cw = temp_canvas.shape[:2]
            scale = min(self.window_w / cw, (self.window_h - 100) / ch)
            new_w = int(cw * scale)
            new_h = int(ch * scale)
            scaled_canvas = cv2.resize(
                temp_canvas, (new_w, new_h), interpolation=cv2.INTER_LINEAR
            )

            # Center the canvas
            x_offset = (self.window_w - new_w) // 2
            y_offset = ((self.window_h - 100) - new_h) // 2
            drawer._img[y_offset : y_offset + new_h, x_offset : x_offset + new_w] = (
                scaled_canvas[:, :, :3]
            )

            # Draw coordinate rulers
            sw, sh = default_config.force_size
            for i in range(1, max_x + 1):
                x_pos = x_offset + (i - 1) * sw * scale + sw * scale / 2
                y_pos = y_offset + new_h + 15
                drawer.text_centered(
                    str(i), (x_pos, y_pos), 0.5, color=0xFFFF00, thickness=1
                )
            for j in range(1, max_y + 1):
                x_pos = x_offset - 20
                y_pos = y_offset + (max_y - j) * sh * scale + sh * scale / 2
                drawer.text_centered(
                    str(j), (x_pos, y_pos), 0.5, color=0xFFFF00, thickness=1
                )

            # Draw yellow overlay and adjustment indicators for manual tiles
            for tile in manual_tiles:
                x, y = tile.file_x, tile.file_y
                tile_x, tile_y = self._get_tile_pos(
                    x, y, scale, x_offset, y_offset, max_x
                )
                tile_w = int(sw * scale)
                tile_h = int(sh * scale)

                # Semi-transparent yellow overlay
                drawer.mask(
                    (tile_x, tile_y),
                    (tile_x + tile_w, tile_y + tile_h),
                    color=0xFFFF00,
                    alpha=0.2,
                )

                # Draw alignment indicator lines
                mode = tile.align_direction
                line_length = 20
                if mode == "lt":
                    args1 = [
                        (tile_x, tile_y),
                        (tile_x + line_length, tile_y),
                    ]
                    args2 = [
                        (tile_x, tile_y),
                        (tile_x, tile_y + line_length),
                    ]
                elif mode == "rt":
                    args1 = [
                        (tile_x + tile_w - line_length, tile_y),
                        (tile_x + tile_w, tile_y),
                    ]
                    args2 = [
                        (tile_x + tile_w, tile_y),
                        (tile_x + tile_w, tile_y + line_length),
                    ]
                elif mode == "lb":
                    args1 = [
                        (tile_x, tile_y + tile_h - line_length),
                        (tile_x, tile_y + tile_h),
                    ]
                    args2 = [
                        (tile_x, tile_y + tile_h),
                        (tile_x + line_length, tile_y + tile_h),
                    ]
                elif mode == "rb":
                    args1 = [
                        (tile_x + tile_w - line_length, tile_y + tile_h),
                        (tile_x + tile_w, tile_y + tile_h),
                    ]
                    args2 = [
                        (tile_x + tile_w, tile_y + tile_h - line_length),
                        (tile_x + tile_w, tile_y + tile_h),
                    ]

                drawer.line(*args1, color=0xFFFF00, thickness=1)
                drawer.line(*args2, color=0xFFFF00, thickness=1)

        # Bottom bar
        drawer.line(
            (0, self.window_h - 100),
            (self.window_w, self.window_h - 100),
            color=0x808080,
            thickness=2,
        )

        # File name
        if current_group:
            drawer.text_centered(
                current_group,
                (self.window_w // 2, self.window_h - 50),
                0.7,
                color=0xFFFFFF,
                thickness=2,
            )

        # Progress bar
        bar_w = 400
        bar_h = 10
        bar_x = (self.window_w - bar_w) // 2
        bar_y = self.window_h - 40
        drawer.rect(
            (bar_x, bar_y),
            (bar_x + bar_w, bar_y + bar_h),
            color=0xFFFFFF,
            thickness=2,
        )
        fill_w = int(bar_w * progress)
        drawer.rect(
            (bar_x, bar_y),
            (bar_x + fill_w, bar_y + bar_h),
            color=0x00FF00,
            thickness=-1,
        )

        # Instruction
        if manual_tiles:
            drawer.text_centered(
                "Click highlighted tiles to adjust alignment, press ENTER to continue",
                (self.window_w // 2, self.window_h - 10),
                0.5,
                color=0xFFFFFF,
                thickness=1,
            )

        return drawer.get_image()

    def _process_single_group(
        self,
        name: str,
        tiles_dict: Dict[Tuple[int, int], str],
        forced_bounds: Tuple[int, int] | None = None,
    ) -> None:
        """Process a single map group including loading, display, adjustment, and saving."""
        file_list = list(tiles_dict.items())

        if not file_list:
            return

        print(f"\nProcessing group: {_C}{name}{_0} with {len(file_list)} tiles.")

        if forced_bounds:
            max_x, max_y = forced_bounds
            own_max_x = max(x for (x, y), _ in file_list)
            own_max_y = max(y for (x, y), _ in file_list)
            print(
                f"  {_Y}Aligning to normal map bounds: "
                f"{own_max_x}x{own_max_y} -> {max_x}x{max_y}{_0}"
            )
        else:
            max_x = max(x for (x, y), _ in file_list)
            max_y = max(y for (x, y), _ in file_list)
        self.max_y = max_y  # Store for use in _get_tile_pos

        canvas_w = max_x * default_config.force_size[0]
        canvas_h = max_y * default_config.force_size[1]
        canvas = np.zeros((canvas_h, canvas_w, 4), dtype=np.uint8)
        canvas[:, :, 3] = 0

        all_tiles = []
        manual_tiles = []

        # Load and process tiles
        total_steps = len(file_list)
        for step, ((x, y), file_path) in enumerate(file_list):
            img = cv2.imread(file_path, cv2.IMREAD_UNCHANGED)
            if img is None:
                continue
            if img.shape[2] == 3:
                img = cv2.cvtColor(img, cv2.COLOR_BGR2BGRA)

            tile = TileInfo(
                file_name=os.path.basename(file_path),
                file_x=x,
                file_y=y,
                raw_img=img,
                raw_w=img.shape[1],
                raw_h=img.shape[0],
                align_mode=None,
                align_direction=None,
            )
            all_tiles.append(tile)

            x_pos = (
                (max_x - x) * default_config.force_size[0]
                if default_config.flip_x
                else (x - 1) * default_config.force_size[0]
            )
            y_pos = (
                (max_y - y) * default_config.force_size[1]
                if default_config.flip_y
                else (y - 1) * default_config.force_size[1]
            )

            if (tile.raw_w, tile.raw_h) == default_config.force_size:
                # Standard size - directly blend
                canvas_drawer = Drawer(canvas)
                canvas_drawer.paste(img, (x_pos, y_pos), with_alpha=True)
            else:
                # Non-standard size - detect alignment
                auto_aligned = False
                align_mode = None
                flag_l = self._has_opaque_pixels_on_edge(img, "left")
                flag_r = self._has_opaque_pixels_on_edge(img, "right")
                flag_t = self._has_opaque_pixels_on_edge(img, "top")
                flag_b = self._has_opaque_pixels_on_edge(img, "bottom")

                sw, sh = default_config.force_size
                if tile.raw_w == sw:
                    true_flags = [
                        ("t" if flag_t else None),
                        ("b" if flag_b else None),
                    ]
                    true_flags = [f for f in true_flags if f]
                    if len(true_flags) == 1:
                        align_mode = true_flags[0]
                        auto_aligned = True
                elif tile.raw_h == sh:
                    true_flags = [
                        ("l" if flag_l else None),
                        ("r" if flag_r else None),
                    ]
                    true_flags = [f for f in true_flags if f]
                    if len(true_flags) == 1:
                        align_mode = true_flags[0]
                        auto_aligned = True
                else:
                    flag_lt = flag_l and flag_t
                    flag_rt = flag_r and flag_t
                    flag_lb = flag_l and flag_b
                    flag_rb = flag_r and flag_b
                    true_corners = [
                        ("lt" if flag_lt else None),
                        ("rt" if flag_rt else None),
                        ("lb" if flag_lb else None),
                        ("rb" if flag_rb else None),
                    ]
                    true_corners = [c for c in true_corners if c]
                    if len(true_corners) == 1:
                        align_mode = true_corners[0]
                        auto_aligned = True

                if auto_aligned and align_mode:
                    direction = align_mode.lower()
                    if len(direction) == 1:
                        if direction == "l":
                            direction = "lt"
                        elif direction == "r":
                            direction = "rt"
                        elif direction == "t":
                            direction = "lt"
                        elif direction == "b":
                            direction = "lb"
                    tile = tile._replace(align_mode="auto", align_direction=direction)
                    all_tiles[-1] = tile

                    sw, sh = default_config.force_size
                    if align_mode == "l":
                        ax, ay = x_pos, y_pos
                    elif align_mode == "r":
                        ax, ay = x_pos + sw - tile.raw_w, y_pos
                    elif align_mode == "t":
                        ax, ay = x_pos, y_pos
                    elif align_mode == "b":
                        ax, ay = x_pos, y_pos + sh - tile.raw_h
                    elif align_mode == "lt":
                        ax, ay = x_pos, y_pos
                    elif align_mode == "rt":
                        ax, ay = x_pos + sw - tile.raw_w, y_pos
                    elif align_mode == "lb":
                        ax, ay = x_pos, y_pos + sh - tile.raw_h
                    elif align_mode == "rb":
                        ax, ay = x_pos + sw - tile.raw_w, y_pos + sh - tile.raw_h
                    canvas_drawer = Drawer(canvas)
                    canvas_drawer.paste(img, (ax, ay), with_alpha=True)

                    print(
                        f"Tile {tile.file_name}: {_G}auto aligned to {direction}{_0} ({tile.raw_w}x{tile.raw_h})"
                    )
                else:
                    tile = tile._replace(align_mode="manual", align_direction="lt")
                    all_tiles[-1] = tile
                    manual_tiles.append(tile)
                    print(
                        f"Tile {tile.file_name}: {_Y}requires manual alignment{_0} ({tile.raw_w}x{tile.raw_h})"
                    )

            progress = (step + 1) / total_steps
            cv2.imshow(
                self.window_name,
                self._render_canvas(canvas, manual_tiles, max_x, max_y, name, progress),
            )
            cv2.waitKey(1)

        # Manual adjustment phase
        if not manual_tiles:
            print(f"{_G}No manual adjustments needed{_0}")
        else:
            print(
                f"{_Y}{len(manual_tiles)} non-standard tiles cannot be auto aligned{_0}"
            )
            print(
                "  Please click on each highlighted tile to adjust their alignment, then press ENTER when done."
            )
            self._manual_adjustment_phase(canvas, manual_tiles, max_x, max_y, name)

        # Save the final merged map
        new_w = int(canvas_w * default_config.scale)
        new_h = int(canvas_h * default_config.scale)
        scaled = cv2.resize(canvas, (new_w, new_h), interpolation=cv2.INTER_LINEAR)

        final_bg = np.zeros((new_h, new_w, 4), dtype=np.uint8)
        final_bg[:, :, 3] = 255
        bg_drawer = Drawer(final_bg)
        bg_drawer.paste(scaled, (0, 0), with_alpha=True)

        output_path = os.path.join(self.output_dir, f"{name}.png")
        cv2.imwrite(output_path, final_bg)
        print(f"{_G}Saved to {output_path}{_0}")

    def _manual_adjustment_phase(
        self,
        canvas: np.ndarray,
        manual_tiles: List[TileInfo],
        max_x: int,
        max_y: int,
        name: str,
    ) -> None:
        """Handle the manual adjustment phase for non-standard tiles."""

        # Create a handler class to encapsulate the state
        class MouseHandler:
            def __init__(self, parent):
                self.parent = parent
                self.tiles_list = manual_tiles

            def handle_click(self, event, x, y, flags, param):
                if event != cv2.EVENT_LBUTTONDOWN:
                    return

                ch, cw = canvas.shape[:2]
                scale = min(
                    self.parent.window_w / cw, (self.parent.window_h - 100) / ch
                )
                new_w = int(cw * scale)
                new_h = int(ch * scale)
                x_offset = (self.parent.window_w - new_w) // 2
                y_offset = ((self.parent.window_h - 100) - new_h) // 2

                for i, tile in enumerate(self.tiles_list):
                    tx, ty = tile.file_x, tile.file_y
                    sw, sh = default_config.force_size
                    tile_x, tile_y = self.parent._get_tile_pos(
                        tx, ty, scale, x_offset, y_offset, max_x
                    )
                    tile_w = int(sw * scale)
                    tile_h = int(sh * scale)

                    if (
                        tile_x <= x <= tile_x + tile_w
                        and tile_y <= y <= tile_y + tile_h
                    ):
                        # Cycle alignment mode
                        current_mode = tile.align_direction
                        modes = ["lt", "rt", "rb", "lb"]
                        idx = modes.index(current_mode)
                        new_mode = modes[(idx + 1) % 4]
                        self.tiles_list[i] = tile._replace(align_direction=new_mode)
                        print(
                            f"Tile {tile.file_name}: {_C}Changed alignment {current_mode} -> {new_mode}{_0}"
                        )
                        break

        handler = MouseHandler(self)
        cv2.setMouseCallback(
            self.window_name,
            lambda event, x, y, flags, param: handler.handle_click(
                event, x, y, flags, param
            ),
        )

        # Display adjustment UI
        cv2.imshow(
            self.window_name,
            self._render_canvas(canvas, manual_tiles, max_x, max_y, name, 1.0),
        )

        # Wait for user input
        while True:
            key = cv2.waitKey(1) & 0xFF
            if key == 13:  # ENTER
                break
            if key == 27:  # ESC
                break
            if cv2.getWindowProperty(self.window_name, cv2.WND_PROP_VISIBLE) < 1:
                break
            cv2.imshow(
                self.window_name,
                self._render_canvas(canvas, manual_tiles, max_x, max_y, name, 1.0),
            )

        # Apply final manual alignments to canvas
        for tile in manual_tiles:
            mode = tile.align_direction
            x_pos = (
                (max_x - tile.file_x) * default_config.force_size[0]
                if default_config.flip_x
                else (tile.file_x - 1) * default_config.force_size[0]
            )
            y_pos = (
                (max_y - tile.file_y) * default_config.force_size[1]
                if default_config.flip_y
                else (tile.file_y - 1) * default_config.force_size[1]
            )
            sw, sh = default_config.force_size
            if mode == "lt":
                ax, ay = x_pos, y_pos
            elif mode == "rt":
                ax, ay = x_pos + sw - tile.raw_w, y_pos
            elif mode == "lb":
                ax, ay = x_pos, y_pos + sh - tile.raw_h
            elif mode == "rb":
                ax, ay = x_pos + sw - tile.raw_w, y_pos + sh - tile.raw_h
            canvas_drawer = Drawer(canvas)
            canvas_drawer.paste(tile.raw_img, (ax, ay), with_alpha=True)

        # Remove the mouse callback for the next group
        try:
            cv2.setMouseCallback(self.window_name, lambda *args: None)
        except cv2.error:
            pass

    def run(self) -> None:
        """Main processing flow for all map groups."""
        cv2.namedWindow(self.window_name)

        for name, tiles_dict in self.groups.items():
            forced_bounds = None
            if self._is_tier_map_name(name):
                base_name = name.split("_tier_")[0]
                forced_bounds = self.normal_bounds.get(base_name)
                if forced_bounds:
                    print(
                        f"\n{_C}Tier group '{name}' aligned to "
                        f"normal group '{base_name}'{_0}"
                    )
                else:
                    print(
                        f"\n{_Y}Warning: No matching normal group for "
                        f"tier group '{name}', using own bounds{_0}"
                    )
            self._process_single_group(name, tiles_dict, forced_bounds=forced_bounds)

        cv2.destroyAllWindows()


class DistinMapPage:
    """Stitches multiple normal maps into a single composite map
    by detecting overlapping regions via grid-aligned template matching."""

    def __init__(self, input_dir: str, output_dir: str):
        self.input_dir = input_dir
        self.output_dir = output_dir
        self.window_name = "MapTracker Map Stitcher"
        self.window_w, self.window_h = 1280, 720
        self.step_fx = default_config.force_size[0] * default_config.scale
        self.step_fy = default_config.force_size[1] * default_config.scale

    def _load_normal_maps(self) -> Dict[str, np.ndarray]:
        """Load all non-tier normal maps from the input directory.
        Images are immediately converted to 3-channel BGR so all downstream
        code can assume a uniform (H, W, 3) uint8 format.
        """
        maps: Dict[str, np.ndarray] = {}
        for fname in sorted(os.listdir(self.input_dir)):
            if not fname.endswith(".png"):
                continue
            if fname.startswith("_"):
                continue
            try:
                parsed = MapName.parse(fname)
            except ValueError:
                continue
            if parsed.map_type != "normal":
                continue
            name = fname[:-4]
            path = os.path.join(self.input_dir, fname)
            img = cv2.imread(path, cv2.IMREAD_UNCHANGED)
            if img is None:
                continue
            # Normalise to 3-channel BGR regardless of source format
            if img.ndim == 2:
                img = cv2.cvtColor(img, cv2.COLOR_GRAY2BGR)
            elif img.shape[2] == 4:
                img = cv2.cvtColor(img, cv2.COLOR_BGRA2BGR)
            maps[name] = img
        return maps

    def _copy_tier_maps(self) -> None:
        """Copy tier maps to output directory unchanged."""
        copied = 0
        for fname in sorted(os.listdir(self.input_dir)):
            if not fname.endswith(".png"):
                continue
            if fname.startswith("_"):
                continue
            try:
                parsed = MapName.parse(fname)
            except ValueError:
                continue
            if parsed.map_type != "tier":
                continue
            src = os.path.join(self.input_dir, fname)
            dst = os.path.join(self.output_dir, fname)
            shutil.copy2(src, dst)
            copied += 1
        if copied > 0:
            print(f"  {_G}Copied {copied} tier map(s) to output dir.{_0}")
        else:
            print(f"  {_Y}No tier maps found to copy.{_0}")

    @staticmethod
    def _content_mask(img: np.ndarray) -> np.ndarray:
        """Binary mask of land pixels (gray > 1)."""
        gray = cv2.cvtColor(img, cv2.COLOR_BGR2GRAY)
        return gray > 1

    @staticmethod
    def _content_bbox(mask: np.ndarray) -> Tuple[int, int, int, int] | None:
        """Return (x1, y1, x2, y2) bounding box of True pixels, or None."""
        ys, xs = np.nonzero(mask)
        if len(ys) == 0:
            return None
        return int(xs.min()), int(ys.min()), int(xs.max()) + 1, int(ys.max()) + 1

    def _match_pair(
        self,
        img_a: np.ndarray,
        mask_a: np.ndarray,
        img_b: np.ndarray,
        mask_b: np.ndarray,
        threshold: float = 0.85,
    ) -> Tuple[int, int, float] | None:
        """Find best grid-aligned offset of B relative to A.
        Returns (dx, dy, score) or None if no match above threshold.

        Optimized with:
        - Content bounding box pruning (skip offsets with no content overlap)
        - Full-resolution grayscale matching
        """
        h_a, w_a = img_a.shape[:2]
        h_b, w_b = img_b.shape[:2]
        sfx, sfy = self.step_fx, self.step_fy

        tiles_bx = round(w_b / sfx)
        tiles_by = round(h_b / sfy)
        tiles_ax = round(w_a / sfx)
        tiles_ay = round(h_a / sfy)

        min_content = round(sfx) * round(sfy) * 0.3

        # Precompute content bounding boxes for fast pruning
        bbox_a = self._content_bbox(mask_a)
        bbox_b = self._content_bbox(mask_b)
        if bbox_a is None or bbox_b is None:
            return None
        ax1, ay1, ax2, ay2 = bbox_a
        bx1, by1, bx2, by2 = bbox_b

        # Precompute grayscale images
        gray_a = cv2.cvtColor(img_a, cv2.COLOR_BGR2GRAY)
        gray_b = cv2.cvtColor(img_b, cv2.COLOR_BGR2GRAY)

        best: Tuple[int, int, float] | None = None

        for nx in range(-(tiles_bx - 1), tiles_ax):
            for ny in range(-(tiles_by - 1), tiles_ay):
                if nx == 0 and ny == 0:
                    continue

                dx = round(nx * sfx)
                dy = round(ny * sfy)

                # --- Pruning: check if content bounding boxes overlap ---
                cb_x1, cb_y1 = bx1 + dx, by1 + dy
                cb_x2, cb_y2 = bx2 + dx, by2 + dy
                if cb_x1 >= ax2 or cb_x2 <= ax1 or cb_y1 >= ay2 or cb_y2 <= ay1:
                    continue

                # --- Grayscale matching at full resolution ---
                ox1, oy1 = max(0, dx), max(0, dy)
                ox2, oy2 = min(w_a, dx + w_b), min(h_a, dy + h_b)
                ow, oh = ox2 - ox1, oy2 - oy1
                if ow <= 0 or oh <= 0:
                    continue

                ra = gray_a[oy1:oy2, ox1:ox2]
                ma = mask_a[oy1:oy2, ox1:ox2]

                fbx1, fby1 = ox1 - dx, oy1 - dy
                rb = gray_b[fby1 : fby1 + oh, fbx1 : fbx1 + ow]
                mb = mask_b[fby1 : fby1 + oh, fbx1 : fbx1 + ow]

                both = ma & mb
                n_both = np.count_nonzero(both)
                if n_both < min_content:
                    continue

                diff = np.abs(ra.astype(np.int16) - rb.astype(np.int16))
                score = 1.0 - np.mean(diff[both]) / 255.0

                if score > threshold and (best is None or score > best[2]):
                    best = (dx, dy, score)

        return best

    def _build_layout(self, maps: Dict[str, np.ndarray]) -> Dict[str, Tuple[int, int]]:
        """Find pairwise overlaps and compute global positions via BFS."""
        names = list(maps.keys())
        n = len(names)
        masks = {nm: self._content_mask(img) for nm, img in maps.items()}

        edges: Dict[str, List[Tuple[str, int, int]]] = {nm: [] for nm in names}
        total = n * (n - 1) // 2
        idx = 0

        print(f"\nSearching for overlaps across {total} pair(s)...")
        for i in range(n):
            for j in range(i + 1, n):
                idx += 1
                na, nb = names[i], names[j]
                print(
                    f"  [{idx}/{total}] {_C}{na}{_0} <-> {_C}{nb}{_0} ",
                    end="",
                    flush=True,
                )
                result = self._match_pair(maps[na], masks[na], maps[nb], masks[nb])
                if result:
                    dx, dy, sc = result
                    print(f"{_G}matched{_0}  offset=({dx},{dy})  score={sc:.4f}")
                    edges[na].append((nb, dx, dy))
                    edges[nb].append((na, -dx, -dy))
                else:
                    print(f"{_A}no overlap{_0}")

        # BFS per connected component
        positions: Dict[str, Tuple[int, int]] = {}
        components: List[List[str]] = []

        for start in names:
            if start in positions:
                continue
            comp: List[str] = []
            positions[start] = (0, 0)
            queue: deque[str] = deque([start])
            while queue:
                cur = queue.popleft()
                comp.append(cur)
                cx, cy = positions[cur]
                for nbr, dx, dy in edges[cur]:
                    if nbr not in positions:
                        positions[nbr] = (cx + dx, cy + dy)
                        queue.append(nbr)
            components.append(comp)

        # Normalize per component; place disconnected components side by side
        x_cursor = 0
        for comp in components:
            min_x = min(positions[nm][0] for nm in comp)
            min_y = min(positions[nm][1] for nm in comp)
            max_right = 0
            for nm in comp:
                px, py = positions[nm]
                positions[nm] = (px - min_x + x_cursor, py - min_y)
                max_right = max(max_right, positions[nm][0] + maps[nm].shape[1])
            x_cursor = max_right + 20

        if len(components) > 1:
            print(
                f"\n{_Y}{len(components)} disconnected component(s) "
                f"placed side by side{_0}"
            )

        return positions

    @staticmethod
    def _map_group_key(name: str) -> str:
        """Extract the map_id prefix from a merged map name.
        E.g. 'map01_lv002' -> 'map01', 'base03_lv001' -> 'base03'.
        Falls back to the full name if no '_lv' separator is found.
        """
        try:
            return MapName.parse(name).map_id
        except ValueError:
            return name

    def _make_land_alpha(self, img: np.ndarray) -> np.ndarray:
        """Return a copy of img with non-land pixels set to alpha=0.
        Prevents black backgrounds from erasing other maps during compositing."""
        out = cv2.cvtColor(img, cv2.COLOR_BGR2BGRA)
        out[~self._content_mask(img), 3] = 0
        return out

    def _composite_canvas(
        self,
        maps: Dict[str, np.ndarray],
        positions: Dict[str, tuple],
        canvas_h: int,
        canvas_w: int,
    ) -> np.ndarray:
        """Composite all maps onto a blank BGRA canvas and return it."""
        canvas = np.zeros((canvas_h, canvas_w, 4), dtype=np.uint8)
        canvas[:, :, 3] = 255
        drawer = Drawer(canvas)
        for nm in sorted(positions, key=lambda n: positions[n]):
            x, y = positions[nm]
            drawer.paste(self._make_land_alpha(maps[nm]), (x, y), with_alpha=True)
        return canvas

    def _stitch_group(self, group_key: str, maps: Dict[str, np.ndarray]) -> None:
        """Stitch a single group of maps and save the result."""
        print(f"\n{_G}[{group_key}]{_0} Stitching {len(maps)} map(s)...")

        positions = self._build_layout(maps)
        names_list = list(positions.keys())

        canvas_w = max(x + maps[nm].shape[1] for nm, (x, y) in positions.items())
        canvas_h = max(y + maps[nm].shape[0] for nm, (x, y) in positions.items())

        print(f"  Compositing onto {canvas_w} x {canvas_h} canvas...")
        for nm in sorted(positions, key=lambda n: positions[n]):
            x, y = positions[nm]
            print(f"    {_C}{nm}{_0} -> ({x}, {y})")
        canvas = self._composite_canvas(maps, positions, canvas_h, canvas_w)

        output_path = os.path.join(self.output_dir, f"_stitched_{group_key}.png")
        cv2.imwrite(output_path, canvas)
        print(f"  {_G}Saved to {output_path}{_0}")

        # --- Remove islands before splitting ---
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

            # Connected components (8-connectivity)
            n_labels, labels = cv2.connectedComponents(land, connectivity=8)

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
        """Let the user draw barriers to split overlapping regions, then BFS.

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

        print(f"  LDrag=draw  RDrag=erase  ENTER=confirm  ESC=skip")

        # ------------------------------------------------------------------
        # Step 2: Interactive barrier drawing (works on canvas coordinates)
        # ------------------------------------------------------------------
        barrier = np.zeros((canvas_h, canvas_w), dtype=np.uint8)
        drawing = [False]
        erasing = [False]
        last_pt: List[Tuple[int, int] | None] = [None]
        scale_ref = [1.0]
        offset_ref: List[Tuple[int, int]] = [(0, 0)]

        def to_canvas_pt(mx: int, my: int) -> Tuple[int, int]:
            s = scale_ref[0]
            ox, oy = offset_ref[0]
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

        def make_display() -> np.ndarray:
            vis = canvas[:, :, :3].astype(np.float32)
            vis[overlap] = (
                vis[overlap] * 0.35 + np.array([0, 140, 255], np.float32) * 0.65
            )
            vis[barrier > 0] = [0, 0, 255]
            vis = np.clip(vis, 0, 255).astype(np.uint8)
            ch_v, cw_v = vis.shape[:2]
            s = min(self.window_w / cw_v, self.window_h / ch_v, 1.0)
            scale_ref[0] = s
            dw, dh = int(cw_v * s), int(ch_v * s)
            scaled = cv2.resize(vis, (dw, dh), interpolation=cv2.INTER_LINEAR)
            frame = np.zeros((self.window_h, self.window_w, 3), dtype=np.uint8)
            ox = (self.window_w - dw) // 2
            oy = (self.window_h - dh) // 2
            offset_ref[0] = (ox, oy)
            frame[oy : oy + dh, ox : ox + dw] = scaled
            cv2.putText(
                frame,
                "LDrag=draw  RDrag=erase  ENTER=confirm  ESC=skip",
                (8, 18),
                cv2.FONT_HERSHEY_SIMPLEX,
                0.45,
                (220, 220, 220),
                1,
                cv2.LINE_AA,
            )
            return frame

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
        # Correct approach:
        #   1. Dilate barrier to close diagonal gaps.
        #   2. Mask out barrier pixels from the overlap zone → "fillable" region.
        #   3. Find connected components of the fillable region.
        #   4. Each component is entirely on ONE side of the barrier, so assign it
        #      in bulk to whichever map has the most exclusive land pixels adjacent
        #      to that component.  No BFS racing, no ordering artifacts.

        # 1 iteration of MORPH_CROSS is the minimum needed to close diagonal
        # gaps so that the barrier forms a 4-connected separator.
        cross_kernel = cv2.getStructuringElement(cv2.MORPH_CROSS, (3, 3))
        wall = cv2.dilate(barrier, cross_kernel, iterations=1).astype(bool)
        print(f"    Barrier pixels (after dilate): {wall.sum()}")

        # Fillable = overlap pixels that are NOT wall
        fillable = (owner == -2) & ~wall
        fillable_u8 = fillable.astype(np.uint8)

        # Connected components of fillable (4-connectivity)
        n_cc, cc_labels = cv2.connectedComponents(fillable_u8, connectivity=4)
        print(f"    Fillable components: {n_cc - 1}")  # component 0 = background

        # Snapshot exclusive regions BEFORE any assignment so loop iterations
        # don't see already-assigned components as "exclusive" land of a map.
        exclusive_masks = [(owner == i) for i in range(n_maps)]

        # For each component, count how many exclusive pixels of each map are
        # 4-adjacent to it → assign to the map with the highest adjacent count.
        # Use cross-kernel dilation so only 4-connected neighbours are checked.
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
                # Component is fully isolated — find nearest exclusive region via
                # distance transform rather than an expensive dilation loop.
                best_map_dt = -1
                best_dist = np.inf
                for i in range(n_maps):
                    if not exclusive_masks[i].any():
                        continue
                    # not_excl: 0 where exclusive (target), 1 elsewhere
                    # distanceTransform gives each non-zero pixel distance to
                    # nearest zero pixel, i.e. distance to nearest exclusive pixel.
                    not_excl = (~exclusive_masks[i]).astype(np.uint8)
                    dist_map = cv2.distanceTransform(not_excl, cv2.DIST_L2, 3)
                    min_dist = float(dist_map[cc_bool].min())
                    if min_dist < best_dist:
                        best_dist = min_dist
                        best_map_dt = i
                if best_map_dt >= 0:
                    owner[cc_bool] = best_map_dt
                # If still not found, fallback (wall-pixel pass) handles it

        # Assign remaining wall pixels (owner == -2) to the alphabetically
        # lowest-named map that has land there (deterministic, no racing).
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
        canvas_bgr = canvas[:, :, :3]

        # Pre-compute shared resources used across the per-map display loop
        dimmed_bg = (canvas_bgr.astype(np.float32) * 0.25).astype(np.uint8)
        box_kernel = np.ones((3, 3), dtype=np.uint8)

        def _show(frame: np.ndarray, title_text: str) -> None:
            """Resize to fit window, add title text, display until keypress."""
            ch_v, cw_v = frame.shape[:2]
            s = min(self.window_w / cw_v, self.window_h / ch_v, 1.0)
            disp = cv2.resize(
                frame, (int(cw_v * s), int(ch_v * s)), interpolation=cv2.INTER_LINEAR
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
                0.55,
                (220, 220, 220),
                1,
                cv2.LINE_AA,
            )
            cv2.putText(
                out,
                "Press any key to continue...",
                (8, self.window_h - 8),
                cv2.FONT_HERSHEY_SIMPLEX,
                0.45,
                (160, 160, 160),
                1,
                cv2.LINE_AA,
            )
            cv2.namedWindow(self.window_name)
            cv2.imshow(self.window_name, out)
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
            cv2.imwrite(out_path, saved)
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
                f"[{i+1}/{len(names_list)}] {nm}  |  owned {int(owned_bool.sum())} px",
            )

        # ---- final combined overview ----
        overview = (canvas_bgr.astype(np.float32) * 0.35).astype(np.uint8)
        rng = np.random.RandomState(42)
        owner_all = np.full((canvas_h, canvas_w), -1, dtype=np.int16)
        for i, mask in enumerate(ownership_masks):
            owner_all[mask > 0] = i
        colors = [tuple(int(c) for c in rng.randint(80, 220, 3)) for _ in names_list]
        for i, nm in enumerate(names_list):
            owned_bool = ownership_masks[i].astype(bool)
            b, g, r = colors[i]
            overview[owned_bool] = (
                canvas_bgr[owned_bool].astype(np.float32) * 0.7
                + np.array([b, g, r], np.float32) * 0.3
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
                    0.4,
                    (255, 255, 255),
                    1,
                    cv2.LINE_AA,
                )

        print(f"  {_G}Split maps saved to {self.output_dir}{_0}")
        _show(overview, f"Overview — {len(names_list)} maps  (any key to close)")
        if cv2.getWindowProperty(self.window_name, cv2.WND_PROP_VISIBLE) >= 1:
            cv2.destroyWindow(self.window_name)

    def run(self) -> None:
        """Main stitching flow - groups maps by map_id and stitches each separately."""
        print(f"\n{_G}MapTracker Map Stitcher{_0}")
        print(f"  Source dir : {_C}{self.input_dir}{_0}")
        print(f"  Output dir : {_C}{self.output_dir}{_0}")
        print(f"  Tile step  : {self.step_fx:.1f} x {self.step_fy:.1f} px")

        ensure_output_dir(self.output_dir)
        self._copy_tier_maps()

        all_maps = self._load_normal_maps()
        if not all_maps:
            print(f"{_Y}No normal maps found in directory.{_0}")
            return

        # Group by map_id prefix
        groups: Dict[str, Dict[str, np.ndarray]] = defaultdict(dict)
        for nm, img in all_maps.items():
            groups[self._map_group_key(nm)][nm] = img

        print(
            f"  Loaded {len(all_maps)} normal map(s) "
            f"in {len(groups)} group(s): "
            + ", ".join(f"{_C}{k}{_0}" for k in sorted(groups))
        )

        for group_key in sorted(groups):
            group_maps = groups[group_key]
            if len(group_maps) < 2:
                print(f"\n{_Y}[{group_key}]{_0} Only 1 map – skipping stitch.")
                continue
            self._stitch_group(group_key, group_maps)


def generate_map_bbox_json(input_dir: str) -> str:
    """Generate map bbox json for all map png files in directory recursively."""
    ensure_output_dir(input_dir)
    results: Dict[str, List[int]] = {}
    threshold = 0.05 * 255.0

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
                rgb = cv2.cvtColor(img, cv2.COLOR_GRAY2BGR)
            elif img.shape[2] >= 3:
                rgb = img[:, :, :3]
            else:
                continue

            brightness = np.mean(rgb, axis=2)
            ys, xs = np.where(brightness > threshold)
            if len(ys) == 0 or len(xs) == 0:
                continue

            min_x, max_x = int(xs.min()), int(xs.max())
            min_y, max_y = int(ys.min()), int(ys.max())
            results[map_name] = [min_x, min_y, max_x + 1, max_y + 1]

    output_path = os.path.join(input_dir, "map_bbox.json")
    with open(output_path, "w", encoding="utf-8") as f:
        json.dump(results, f, indent=4, ensure_ascii=False)
    print(f"{_G}Saved map rectangles to {output_path}{_0}")
    return output_path


def main():
    print(f"{_G}Welcome to MapTracker map merging tool.{_0}")
    ensure_output_dir(MAP_MERGED_DIR)
    ensure_output_dir(MAP_FINAL_DIR)

    print(f"\n{_Y}Select a mode:{_0}")
    print(f"  {_C}[1]{_0} Merge normal and tier maps")
    print(f"  {_C}[2]{_0} Merge base maps")
    print(f"  {_C}[3]{_0} Merge dungeon maps")
    print(f"  {_C}[4]{_0} Distin maps (stitch + split)")
    print(f"  {_C}[5]{_0} Generate map bbox JSON")
    mode = input("> ").strip().upper()

    if mode == "4":
        if not os.path.isdir(MAP_MERGED_DIR):
            print(f"{_R}Source directory not found: {MAP_MERGED_DIR}{_0}")
            return
        stitch = DistinMapPage(MAP_MERGED_DIR, MAP_FINAL_DIR)
        stitch.run()
        return

    if mode == "5":
        generate_map_bbox_json(MAP_FINAL_DIR)
        return

    map_types = {
        "1": ["normal", "tier"],
        "2": ["base"],
        "3": ["dung"],
    }.get(mode)

    if not map_types:
        print(f"{_R}Invalid selection. Exiting.{_0}")
        return

    print(f"\n{_Y}Input a directory to load map tiles from:{_0}")
    input_dir = input("> ").strip()

    if not os.path.isdir(input_dir):
        print(f"{_R}Given path not found or not a dir. Exiting.{_0}")
        return

    page = MergeMapPage(map_types, input_dir, MAP_MERGED_DIR)
    page.run()


if __name__ == "__main__":
    main()
