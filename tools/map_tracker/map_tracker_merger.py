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
import numpy as np
from collections import defaultdict
from typing import Dict, List, Tuple, NamedTuple
from utils import _R, _G, _Y, _C, _0, Drawer, cv2, Point


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
    default_output_dir: str
    scale: float
    flip_x: bool
    flip_y: bool
    force_size: Tuple[int, int]  # (width, height)


default_config = MergeMapConfig(
    default_output_dir="mapper/merged_gen",
    scale=0.1625,
    flip_x=False,
    flip_y=True,
    force_size=(600, 600),  # Width, Height
)


class MergeMapPage:
    def __init__(self, map_type: str, input_dir: str):
        self.map_type = map_type
        self.input_dir = input_dir
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
        if self.map_type == "normal":
            pattern = r"(map\d+_lv\d+)_(\d+)_(\d+)\.png"
        elif self.map_type == "base":
            pattern = r"(base\d+_lv\d+)_(\d+)_(\d+)\.png"
        elif self.map_type == "dungeon":
            pattern = r"(dung\d+_lv\d+)_(\d+)_(\d+)\.png"
        elif self.map_type == "tier":
            pattern = r"(\w+)_(\d+)_(\d+)_tier_(\w+)\.png"
        else:
            raise ValueError("Invalid map type")

        if not os.path.exists(default_config.default_output_dir):
            os.makedirs(default_config.default_output_dir)

        # Collect matching files
        groups = defaultdict(dict)
        for root, _, file_names in os.walk(self.input_dir):
            for file_name in file_names:
                file_path = os.path.join(root, file_name)
                m = re.match(pattern, file_name)
                if m:
                    if self.map_type in {"normal", "base", "dungeon"}:
                        name = m.group(1)
                        x, y = int(m.group(2)), int(m.group(3))
                    elif self.map_type == "tier":
                        name = f"{m.group(1)}_tier_{m.group(4)}"
                        x, y = int(m.group(2)), int(m.group(3))
                    key = (x, y)
                    if key in groups[name]:
                        print(
                            f"{_Y}Warning: Duplicate tile at ({x}, {y}) for {name}, skipping{_0}"
                        )
                        continue
                    groups[name][key] = file_path

        if not groups:
            print(f"{_R}No map tiles found in input directories.{_0}")
        self.groups = groups

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
        self, name: str, tiles_dict: Dict[Tuple[int, int], str]
    ) -> None:
        """Process a single map group including loading, display, adjustment, and saving."""
        file_list = list(tiles_dict.items())

        if not file_list:
            return

        print(f"\nProcessing group: {_C}{name}{_0} with {len(file_list)} tiles.")

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

        output_path = os.path.join(
            default_config.default_output_dir, f"{name}_merged.png"
        )
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
            self._process_single_group(name, tiles_dict)

        cv2.destroyAllWindows()


def main():
    print(f"{_G}Welcome to MapTracker map merging tool.{_0}")
    print(f"\n{_Y}Select a mode:{_0}")
    print(f"  {_C}[1]{_0} Merge normal maps")
    print(f"  {_C}[2]{_0} Merge tier maps")
    print(f"  {_C}[3]{_0} Merge base maps")
    print(f"  {_C}[4]{_0} Merge dungeon maps")
    mode = input("> ").strip().upper()
    map_type = {
        "1": "normal",
        "2": "tier",
        "3": "base",
        "4": "dungeon",
    }.get(mode)

    if not map_type:
        print(f"{_R}Invalid selection. Exiting.{_0}")
        return

    print(f"\n{_Y}Input a directory to load map tiles from:{_0}")
    input_dir = input("> ").strip()

    if not os.path.isdir(input_dir):
        print(f"{_R}Given path not found or not a dir. Exiting.{_0}")
        return

    page = MergeMapPage(map_type, input_dir)
    page.run()


if __name__ == "__main__":
    main()
