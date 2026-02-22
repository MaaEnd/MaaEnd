# /// script
# requires-python = ">=3.12"
# dependencies = [
#     "opencv-python>=4",
# ]
# ///

# MapTracker - Editor Tool
# This tool provides a GUI to view and edit paths for MapTracker.

import os
import math
import re
import json
import time
from typing import NamedTuple
import numpy as np
from utils import _R, _G, _Y, _C, _A, _0, Color, Drawer, cv2


MAP_DIR = "assets/resource/image/MapTracker/map"
SERVICE_LOG_FILE = "install/debug/go-service.log"


class SelectMapPage:
    """Map selection page"""

    def __init__(self, map_dir=MAP_DIR):
        self.map_dir = map_dir
        self.map_files = self._load_and_sort_maps()
        self.rows, self.cols = 2, 5
        self.nav_height = 90
        self.window_w, self.window_h = 1280, 720
        self.cell_size = min(
            self.window_w // self.cols, (self.window_h - self.nav_height) // self.rows
        )
        self.page_size = self.rows * self.cols
        self.window_name = "MapTracker Tool - Map Selector"

        self.current_page = 0
        self.cached_page = -1
        self.cached_img = None
        self.selected_index = -1
        self.total_pages = math.ceil(len(self.map_files) / self.page_size)

    def _load_and_sort_maps(self):
        map_files = [f for f in os.listdir(self.map_dir) if f.endswith(".png")]
        if not map_files:
            return []

        def natural_sort_key(s):
            return [
                int(text) if text.isdigit() else text.lower()
                for text in re.split("([0-9]+)", s)
            ]

        map_files.sort(key=lambda x: (len(x), natural_sort_key(x)))
        return map_files

    def _render_page(self):
        if self.cached_page == self.current_page:
            return self.cached_img
        drawer: Drawer = Drawer.new(self.window_w, self.window_h)
        start_idx = self.current_page * self.page_size
        end_idx = min(start_idx + self.page_size, len(self.map_files))

        # Content area height (excluding bottom navigation)
        content_h = self.window_h - self.nav_height
        content_w = self.window_w

        # Calculate horizontal and vertical spacing (space-between)
        if self.cols > 1:
            gap_x = int((content_w - self.cols * self.cell_size) / (self.cols - 1))
        else:
            gap_x = 0
        if self.rows > 1:
            gap_y = int((content_h - self.rows * self.cell_size) / (self.rows - 1))
        else:
            gap_y = 0

        # Draw map previews in space-between layout
        for i in range(start_idx, end_idx):
            idx_in_page = i - start_idx
            r = idx_in_page // self.cols
            c = idx_in_page % self.cols

            cell_x = int(c * (self.cell_size + gap_x))
            cell_y = int(r * (self.cell_size + gap_y))

            path = os.path.join(self.map_dir, self.map_files[i])
            img = cv2.imread(path)
            if img is not None:
                h, w = img.shape[:2]
                # Calculate scaling to maintain aspect ratio, fit image completely into cell
                scale = min(self.cell_size / w, self.cell_size / h)
                new_w = max(1, int(w * scale))
                new_h = max(1, int(h * scale))
                resized = cv2.resize(img, (new_w, new_h))
                # Center the image within the cell
                x1 = cell_x
                y1 = cell_y
                x2 = x1 + self.cell_size
                y2 = y1 + self.cell_size
                # Calculate placement offset
                dx = (self.cell_size - new_w) // 2
                dy = (self.cell_size - new_h) // 2
                dest_x1 = x1 + dx
                dest_y1 = y1 + dy
                dest_x2 = dest_x1 + new_w
                dest_y2 = dest_y1 + new_h
                # Boundary clipping (to prevent exceeding content area)
                dest_x2 = min(self.window_w, dest_x2)
                dest_y2 = min(content_h, dest_y2)
                src_x2 = dest_x2 - dest_x1
                src_y2 = dest_y2 - dest_y1
                if src_x2 > 0 and src_y2 > 0:
                    drawer._img[
                        dest_y1 : dest_y1 + src_y2, dest_x1 : dest_x1 + src_x2
                    ] = resized[0:src_y2, 0:src_x2]

                # Label (bottom)
                label = self.map_files[i]
                drawer.rect(
                    (x1, y1 + self.cell_size - 30),
                    (x1 + self.cell_size, y1 + self.cell_size),
                    color=0x000000,
                    thickness=-1,
                )
                drawer.text_centered(
                    label,
                    (x1 + self.cell_size // 2, y1 + self.cell_size - 10),
                    0.4,
                    color=0xFFFFFF,
                    thickness=1,
                )

        # Bottom navigation bar
        drawer.line(
            (0, content_h),
            (self.window_w, content_h),
            color=0x808080,
            thickness=2,
        )

        # Top navigation prompt text
        drawer.text_centered(
            "Please click a map to continue",
            (drawer.w // 2, content_h + 30),
            0.7,
            color=0xFFFFFF,
            thickness=1,
        )

        # Left arrow
        drawer.text(
            "< PREV",
            (150, self.window_h - 20),
            0.6,
            color=0x00FF00 if self.current_page > 0 else 0x808080,
            thickness=2,
        )

        # Middle page info
        page_text = f"Page {self.current_page + 1} / {self.total_pages}"
        drawer.text_centered(
            page_text,
            (drawer.w // 2, self.window_h - 20),
            0.5,
            color=0xFFFFFF,
            thickness=1,
        )

        # Right arrow
        drawer.text(
            "NEXT >",
            (self.window_w - 200, self.window_h - 20),
            0.6,
            color=0x00FF00 if self.current_page < self.total_pages - 1 else 0x808080,
            thickness=2,
        )

        self.cached_img = drawer.get_image()
        self.cached_page = self.current_page
        return self.cached_img

    def _handle_mouse(self, event, x, y, flags, param):
        if event == cv2.EVENT_LBUTTONDOWN:
            # Content area height (excluding bottom navigation)
            content_h = self.window_h - self.nav_height
            if y < content_h:
                # Use layout calculation to determine which cell the click falls into
                if self.cols > 1:
                    gap_x = int(
                        (self.window_w - self.cols * self.cell_size) / (self.cols - 1)
                    )
                else:
                    gap_x = 0
                if self.rows > 1:
                    gap_y = int(
                        (content_h - self.rows * self.cell_size) / (self.rows - 1)
                    )
                else:
                    gap_y = 0

                found = False
                for r in range(self.rows):
                    for c in range(self.cols):
                        cell_x = int(c * (self.cell_size + gap_x))
                        cell_y = int(r * (self.cell_size + gap_y))
                        if (
                            x >= cell_x
                            and x < cell_x + self.cell_size
                            and y >= cell_y
                            and y < cell_y + self.cell_size
                        ):
                            idx = self.current_page * self.page_size + r * self.cols + c
                            if idx < len(self.map_files):
                                self.selected_index = idx
                                found = True
                                break
                    if found:
                        break
            else:
                # Bottom navigation
                if x < self.window_w // 3:
                    if self.current_page > 0:
                        self.current_page -= 1
                elif x > 2 * self.window_w // 3:
                    if self.current_page < self.total_pages - 1:
                        self.current_page += 1

    def run(self):
        if not self.map_files:
            print(f"Error: No maps found in {self.map_dir}")
            return None

        cv2.namedWindow(self.window_name)
        cv2.setMouseCallback(self.window_name, self._handle_mouse)

        while True:
            cv2.imshow(self.window_name, self._render_page())

            if self.selected_index != -1:
                break
            key = cv2.waitKey(30) & 0xFF
            if key == 27:  # ESC
                break
            if cv2.getWindowProperty(self.window_name, cv2.WND_PROP_VISIBLE) < 1:
                break

        cv2.destroyAllWindows()
        if self.selected_index != -1:
            return self.map_files[self.selected_index]
        return None


class PathEditPage:
    """Path editing page"""

    SIDEBAR_W = 240
    STATUS_BAR_H = 32
    LINE_WIDTH = 1.75
    POINT_RADIUS = 4.5
    POINT_SELECTION_THRESHOLD = 10

    class StatusRecord(NamedTuple):
        timestamp: float
        color: Color
        message: str

    def __init__(
        self,
        map_name,
        initial_points=None,
        map_dir=MAP_DIR,
        *,
        pipeline_context: dict | None = None,
    ):
        """
        Args:
            pipeline_context: Optional dict with keys:
                ``handler``    – PipelineHandler instance
                ``node_name``  – str, node to save back
                ``file_path``  – str, for display
            If None the editor runs in "N mode" (no save button).
        """
        self.map_name = map_name
        self.map_path = os.path.join(map_dir, map_name)
        if not os.path.exists(self.map_path):
            print(f"Error: Map file not found: {self.map_path}")

        self.img = cv2.imread(self.map_path)
        if self.img is None:
            raise ValueError(f"Cannot load map: {self.map_path}")

        self.points = [list(p) for p in initial_points] if initial_points else []
        self._point_snapshot: list[list] = [list(p) for p in self.points]

        self.pipeline_context = pipeline_context  # None → N mode
        self.location_service = LocationService()

        self.scale = 1.0
        self.offset_x, self.offset_y = 0, 0
        self.window_w, self.window_h = 1280, 720
        self.window_name = "MapTracker Tool - Path Editor"

        self.drag_idx = -1
        self.selected_idx = -1
        self.panning = False
        self.pan_start = (0, 0)
        self.mouse_pos: tuple[int, int] = (-1, -1)  # For crosshair display
        self._scaled_img: np.ndarray | None = None
        self._scaled_scale: float | None = None

        # Action state for point interactions (left button)
        self.action_down_idx = -1
        self.action_mouse_down = False
        self.action_down_pos = (0, 0)
        self.action_moved = False
        self.action_dragging = False
        self.done = False

        # Status feedback shown in map area status bar
        self._status: PathEditPage.StatusRecord = self.StatusRecord(
            0, 0xFFFFFF, "Welcome to MapTracker Editor!"
        )
        self._modal_active: bool = False
        self._modal_text: str = ""

        # Button hit-rects: (x1, y1, x2, y2) – populated by _render_sidebar
        self._btn_save_rect: tuple | None = None
        self._btn_loc_rect: tuple | None = None
        self._btn_finish_rect: tuple | None = None

    # ------------------------------------------------------------------
    # Helpers
    # ------------------------------------------------------------------

    @property
    def is_dirty(self) -> bool:
        """True when current points differ from the initial snapshot."""
        return self.points != self._point_snapshot

    def _update_status(self, color: Color, message: str) -> None:
        self._status = self.StatusRecord(time.time(), color, message)

    def _do_save(self):
        """Save the current path to the pipeline file (I mode only)."""
        if self.pipeline_context is None:
            return
        handler: PipelineHandler = self.pipeline_context["handler"]
        node_name: str = self.pipeline_context["node_name"]
        if handler.replace_path(node_name, self.points):
            self._point_snapshot = [list(p) for p in self.points]
            self._update_status(0x50DC50, "Saved changes!")
            print(f"  {_G}Path saved to file.{_0}")
        else:
            self._update_status(0xFC4040, "Failed to save changes!")
            print(f"  {_Y}Failed to save path to file.{_0}")

    def _apply_realtime_location(self):
        """Append the latest realtime location from service log."""
        self._modal_active = True
        self._modal_text = "Connecting to service"
        self._render()

        result = self.location_service.wait_for_new_location(
            norm_map_name(self.map_name), timeout_seconds=5.0
        )

        self._modal_active = False
        self._modal_text = ""
        if result.status == "ok":
            x = result.payload["x"]
            y = result.payload["y"]
            self.points.append([x, y])
            self.selected_idx = len(self.points) - 1
            self._update_status(0x78DCFF, f"Realtime location added: ({x}, {y})")
        elif result.status == "mismatch":
            self._update_status(
                0xFC4040, f"Error located map mismatch ({result.payload})"
            )
        else:
            self._update_status(0xD2D200, str(result.payload))

    def _get_map_coords(self, screen_x, screen_y):
        """Convert screen (viewport) coordinates to original map coordinates.

        The usable map area starts at x = SIDEBAR_W.
        """
        mx = round(screen_x / self.scale + self.offset_x)
        my = round(screen_y / self.scale + self.offset_y)
        return mx, my

    def _get_screen_coords(self, map_x, map_y):
        """Convert original map coordinates to screen (viewport) coordinates."""
        sx = round((map_x - self.offset_x) * self.scale)
        sy = round((map_y - self.offset_y) * self.scale)
        return sx, sy

    def _is_on_line(self, mx, my, p1, p2, threshold=10):
        """Check if a point is on the line between two points"""
        x1, y1 = p1
        x2, y2 = p2
        px, py = mx, my
        dx = x2 - x1
        dy = y2 - y1
        if dx == 0 and dy == 0:
            return math.hypot(px - x1, py - y1) < threshold
        t = max(0, min(1, ((px - x1) * dx + (py - y1) * dy) / (dx * dx + dy * dy)))
        closest_x = x1 + t * dx
        closest_y = y1 + t * dy
        dist = math.hypot(px - closest_x, py - closest_y)
        return dist < threshold

    # ------------------------------------------------------------------
    # Rendering
    # ------------------------------------------------------------------

    def _render(self):
        drawer = Drawer.new(self.window_w, self.window_h)

        if self._scaled_img is None or self._scaled_scale != self.scale:
            scaled_w = max(1, int(self.img.shape[1] * self.scale))
            scaled_h = max(1, int(self.img.shape[0] * self.scale))
            self._scaled_img = cv2.resize(
                self.img, (scaled_w, scaled_h), interpolation=cv2.INTER_AREA
            )
            self._scaled_scale = self.scale

        scaled_img = self._scaled_img
        if scaled_img is not None:
            scaled_h, scaled_w = scaled_img.shape[:2]
            src_x1 = int(round(self.offset_x * self.scale))
            src_y1 = int(round(self.offset_y * self.scale))
            dst_x = max(0, -src_x1)
            dst_y = max(0, -src_y1)
            src_x1 = max(0, src_x1)
            src_y1 = max(0, src_y1)
            src_x2 = min(scaled_w, src_x1 + self.window_w - dst_x)
            src_y2 = min(scaled_h, src_y1 + self.window_h - dst_y)

            copy_w = src_x2 - src_x1
            copy_h = src_y2 - src_y1
            if copy_w > 0 and copy_h > 0:
                drawer.get_image()[dst_y : dst_y + copy_h, dst_x : dst_x + copy_w] = (
                    scaled_img[src_y1:src_y2, src_x1:src_x2]
                )

        # Draw path lines
        for i in range(len(self.points)):
            sx, sy = self._get_screen_coords(self.points[i][0], self.points[i][1])
            if i > 0:
                psx, psy = self._get_screen_coords(
                    self.points[i - 1][0], self.points[i - 1][1]
                )
                drawer.line(
                    (psx, psy),
                    (sx, sy),
                    color=0xFF0000,
                    thickness=max(1, int(self.LINE_WIDTH * self.scale**0.5)),
                )

        # Draw point circles
        for i in range(len(self.points)):
            sx, sy = self._get_screen_coords(self.points[i][0], self.points[i][1])
            drawer.circle(
                (sx, sy),
                int(self.POINT_RADIUS * max(0.5, self.scale**0.5)),
                color=0xFFA500 if i == self.drag_idx else 0xFF0000,
                thickness=-1,
            )

        # Draw point index labels
        for i in range(len(self.points)):
            sx, sy = self._get_screen_coords(self.points[i][0], self.points[i][1])
            drawer.text(str(i), (sx + 5, sy - 5), 0.5, color=0xFFFFFF, thickness=1)

        # Draw crosshair at current mouse position
        drawer.line(
            (self.mouse_pos[0], 0),
            (self.mouse_pos[0], self.window_h),
            color=0xFFFF00,
            thickness=1,
        )
        drawer.line(
            (0, self.mouse_pos[1]),
            (self.window_w, self.mouse_pos[1]),
            color=0xFFFF00,
            thickness=1,
        )

        if self._modal_active:
            x1 = self.SIDEBAR_W
            x2 = self.window_w
            y1 = 0
            y2 = self.window_h
            drawer.mask((x1, y1), (x2, y2), color=0x000000, alpha=0.7)
            drawer.text_centered(
                self._modal_text or "Connecting to service",
                (x1 + (x2 - x1) // 2, y1 + (y2 - y1) // 2),
                0.7,
                color=0xFFFFFF,
                thickness=2,
            )

        self._render_status_bar(drawer)
        self._render_sidebar(drawer)
        cv2.imshow(self.window_name, drawer.get_image())

    def _render_status_bar(self, drawer: "Drawer"):
        x1 = self.SIDEBAR_W
        x2 = self.window_w
        y2 = self.window_h
        y1 = max(0, y2 - self.STATUS_BAR_H)
        drawer.mask((x1, y1), (x2, y2), color=0x000000, alpha=0.8)
        if not self._status:
            return

        drawer.text(
            self._status.message,
            (x1 + 10, y2 - 10),
            0.45,
            color=self._status.color,
            thickness=1,
        )

    def _render_sidebar(self, drawer: "Drawer"):
        """Draw the left sidebar with a 90%-opaque black background.

        Strategy: Extract the existing sidebar pixels, blend them with
        semi-transparent black, then render UI directly on top.
        """
        sw = self.SIDEBAR_W
        h = self.window_h
        pad = 15

        # ── Extract and blend sidebar background ──────────────────────────
        # Blend with semi-transparent black
        drawer.mask((0, 0), (sw, h), color=0x000000, alpha=0.9)

        # ── Right border ─────────────────────────────────────────────────
        drawer.line((sw - 1, 0), (sw - 1, h), color=0xFFFFFF, thickness=1)

        # ── Tips section ─────────────────────────────────────────────────
        cy = pad + 15
        drawer.text(
            "[ Mouse Tips ]",
            (pad, cy),
            0.5,
            color=0x40FFFF,
            thickness=1,
        )
        cy += 10
        tips = [
            "Left Click: Add/Delete Point",
            "Left Drag: Move Point",
            "Right Drag: Move Map",
            "Scroll: Zoom",
        ]
        for line in tips:
            cy += 20
            drawer.text(line, (pad, cy), 0.4, color=0xC8C8C8, thickness=1)
        cy += 15  # small gap after tips

        # ── Buttons ──────────────────────────────────────────────────────
        btn_h = 30
        btn_w = sw - pad * 2
        btn_x0 = pad
        has_pipeline = self.pipeline_context is not None
        dirty = self.is_dirty

        if has_pipeline:
            # Save button
            save_y0 = cy
            save_y1 = cy + btn_h
            self._btn_save_rect = (btn_x0, save_y0, btn_x0 + btn_w, save_y1)

            save_color = 0x64C800 if dirty else 0x3C643C
            save_text_color = 0xFFFFFF if dirty else 0x648264
            drawer.rect(
                (btn_x0, save_y0),
                (btn_x0 + btn_w, save_y1),
                color=save_color,
                thickness=-1,
            )
            drawer.rect(
                (btn_x0, save_y0),
                (btn_x0 + btn_w, save_y1),
                color=0xB4B4B4,
                thickness=1,
            )
            drawer.text_centered(
                "[S] Save",
                (btn_x0 + btn_w // 2, save_y0 + btn_h - 8),
                0.45,
                color=save_text_color,
                thickness=1,
            )
            cy = save_y1 + 8

        # Realtime location button
        loc_y0 = cy
        loc_y1 = cy + btn_h
        self._btn_loc_rect = (btn_x0, loc_y0, btn_x0 + btn_w, loc_y1)
        drawer.rect(
            (btn_x0, loc_y0),
            (btn_x0 + btn_w, loc_y1),
            color=0x1A40B8,
            thickness=-1,
        )
        drawer.rect(
            (btn_x0, loc_y0),
            (btn_x0 + btn_w, loc_y1),
            color=0xB4B4B4,
            thickness=1,
        )
        drawer.text_centered(
            "[G] Get Realtime Location",
            (btn_x0 + btn_w // 2, loc_y0 + btn_h - 8),
            0.42,
            color=0xFFFFFF,
            thickness=1,
        )
        cy = loc_y1 + 8

        # Finish button – always present
        finish_y0 = cy
        finish_y1 = cy + btn_h
        self._btn_finish_rect = (btn_x0, finish_y0, btn_x0 + btn_w, finish_y1)
        drawer.rect(
            (btn_x0, finish_y0),
            (btn_x0 + btn_w, finish_y1),
            color=0xB44022,
            thickness=-1,
        )
        drawer.rect(
            (btn_x0, finish_y0),
            (btn_x0 + btn_w, finish_y1),
            color=0xB4B4B4,
            thickness=1,
        )
        drawer.text_centered(
            "[F] Finish",
            (btn_x0 + btn_w // 2, finish_y0 + btn_h - 8),
            0.45,
            color=0xFFFFFF,
            thickness=1,
        )

        # Status messages moved to map area status bar

        # ── Status section (bottom) ──────────────────────────────────────
        drawer.text(
            f"Zoom: {self.scale:.2f}x",
            (pad, h - 75),
            0.45,
            color=0xD2D200,
            thickness=1,
        )

        if 0 <= self.selected_idx < len(self.points):
            p = self.points[self.selected_idx]
            line = f"Point #{self.selected_idx} ({int(p[0])}, {int(p[1])})"
        else:
            line = f"Points: {len(self.points)}"
        drawer.text(line, (pad, h - 50), 0.45, color=0xFFFFFF, thickness=1)

    # ------------------------------------------------------------------
    # Mouse / keyboard handling
    # ------------------------------------------------------------------

    def _hit_button(self, x, y, rect) -> bool:
        if rect is None:
            return False
        x1, y1, x2, y2 = rect
        return x1 <= x <= x2 and y1 <= y <= y2

    def _get_point_at(self, x, y) -> int:
        for i, p in enumerate(self.points):
            sx, sy = self._get_screen_coords(p[0], p[1])
            dist = math.hypot(x - sx, y - sy)
            if dist < self.POINT_SELECTION_THRESHOLD:
                return i
        return -1

    def _handle_mouse(self, event, x, y, flags, param):
        # Track mouse position for crosshair
        self.mouse_pos = (x, y)
        if self._modal_active:
            if event == cv2.EVENT_MOUSEMOVE:
                self._render()
            return  # Prevent all interactions when modal is active

        # ── Map area events ──────────────────────────────────────────────
        mx, my = self._get_map_coords(x, y)
        if event == cv2.EVENT_MOUSEWHEEL:
            self.scale *= 1.14514 if flags > 0 else 1 / 1.14514
            self.scale = max(0.5, min(self.scale, 10.0))

            self.offset_x = mx - x / self.scale
            self.offset_y = my - y / self.scale
            self._scaled_img = None
            self._scaled_scale = None
            self._render()

        elif event == cv2.EVENT_MOUSEMOVE:
            # Pan
            if self.panning:
                dx = (x - self.pan_start[0]) / self.scale
                dy = (y - self.pan_start[1]) / self.scale
                self.offset_x -= dx
                self.offset_y -= dy
                self.pan_start = (x, y)
                self._render()
                return

            # Action (left button) dragging
            if self.action_mouse_down:
                if self.action_dragging and self.drag_idx != -1:
                    self.points[self.drag_idx] = [mx, my]
                    self.action_moved = True
                    self._render()
                    return

                dx = x - self.action_down_pos[0]
                dy = y - self.action_down_pos[1]
                if dx * dx + dy * dy > 25:
                    self.action_moved = True
                    if self.action_down_idx != -1:
                        self.action_dragging = True
                        self.drag_idx = self.action_down_idx
                        self.points[self.drag_idx] = [mx, my]
                        self._render()
                        return

            if (flags & cv2.EVENT_FLAG_LBUTTON) and self.drag_idx != -1:
                self.points[self.drag_idx] = [mx, my]
                self.action_dragging = True
            self._render()

        elif event == cv2.EVENT_RBUTTONDOWN:
            if x < self.SIDEBAR_W:
                return  # Ignore right-clicks on sidebar
            self.panning = True
            self.pan_start = (x, y)

        elif event == cv2.EVENT_RBUTTONUP:
            self.panning = False

        elif event == cv2.EVENT_LBUTTONDOWN:
            # ── Sidebar clicks ────────────────────────────────────────
            if x < self.SIDEBAR_W:
                if self._hit_button(x, y, self._btn_save_rect) and self.is_dirty:
                    self._do_save()
                    self._render()
                elif self._hit_button(x, y, self._btn_loc_rect):
                    self._apply_realtime_location()
                    self._render()
                elif self._hit_button(x, y, self._btn_finish_rect):
                    self.done = True
                return  # Prevent event propagation

            # ── Map area clicks ─────────────────────────────────

            self.action_down_idx = self._get_point_at(x, y)
            self.action_mouse_down = True
            self.action_down_pos = (x, y)
            self.action_moved = False
            self.action_dragging = False
            if self.action_down_idx != -1:
                self.drag_idx = self.action_down_idx
                self.selected_idx = self.action_down_idx

        elif event == cv2.EVENT_LBUTTONUP:
            if self.action_dragging and self.drag_idx != -1:
                self.drag_idx = -1
            else:
                if self.action_moved and self.action_down_idx == -1:
                    pass
                else:
                    if self.action_down_idx != -1:
                        del_idx = self.action_down_idx
                        if 0 <= del_idx < len(self.points):
                            deleted_point = self.points[del_idx]
                            self.points.pop(del_idx)
                            if self.drag_idx == del_idx:
                                self.drag_idx = -1
                            elif self.drag_idx > del_idx:
                                self.drag_idx -= 1
                            if self.selected_idx == del_idx:
                                self.selected_idx = -1
                            elif self.selected_idx > del_idx:
                                self.selected_idx -= 1
                            self._update_status(
                                0x78DCFF,
                                f"Deleted Point #{del_idx} ({int(deleted_point[0])}, {int(deleted_point[1])})",
                            )
                    elif self.action_down_pos == (x, y):
                        inserted = False
                        for i in range(1, len(self.points)):
                            map_threshold = self.POINT_SELECTION_THRESHOLD / max(
                                0.01, self.scale
                            )
                            if self._is_on_line(
                                mx,
                                my,
                                self.points[i - 1],
                                self.points[i],
                                threshold=map_threshold,
                            ):
                                self.points.insert(i, [mx, my])
                                self.selected_idx = i
                                self._update_status(
                                    0x78DCFF,
                                    f"Added Point #{i} ({int(mx)}, {int(my)})",
                                )
                                inserted = True
                                break
                        if not inserted:
                            self.points.append([mx, my])
                            self.selected_idx = len(self.points) - 1
                            self._update_status(
                                0x78DCFF,
                                f"Added Point #{self.selected_idx} ({int(mx)}, {int(my)})",
                            )

            self.action_down_idx = -1
            self.action_mouse_down = False
            self.action_down_pos = (0, 0)
            self.action_moved = False
            self.action_dragging = False
            self._render()

    # ------------------------------------------------------------------
    # Main loop
    # ------------------------------------------------------------------

    def run(self):
        cv2.namedWindow(self.window_name)
        cv2.setMouseCallback(self.window_name, self._handle_mouse)

        self._render()
        while not self.done:
            if cv2.getWindowProperty(self.window_name, cv2.WND_PROP_VISIBLE) < 1:
                break
            key = cv2.waitKey(30) & 0xFF
            if key == 27 or key == ord("f") or key == ord("F"):  # ESC / F → Finish
                break
            if (
                (key == ord("s") or key == ord("S"))
                and self.pipeline_context
                and self.is_dirty
            ):
                self._do_save()
                self._render()
            if key == ord("g") or key == ord("G"):
                self._apply_realtime_location()
                self._render()

        cv2.destroyAllWindows()
        return [list(p) for p in self.points]


def find_map_file(name: str, map_dir: str = MAP_DIR) -> str | None:
    """Find the filename corresponding to the given name on disk (keeping the suffix), return the filename or None."""
    if not os.path.isdir(map_dir):
        return None
    files = os.listdir(map_dir)
    if name in files:
        return name
    for suffix in [".png", "_merged.png"]:
        if name + suffix in files:
            return name + suffix
    return None


def norm_map_name(name: str) -> str:
    """Normalize a map name by stripping suffixes and extensions."""
    return re.sub(r"(_merged)?\.png$", "", name)


class LocationService:
    """Read realtime location from a jsonl service log."""

    class LocationServiceResult(NamedTuple):
        status: str
        payload: dict | str

    MAX_LOOKBACK_LINES = 600
    READ_CHUNK_SIZE = 8192
    MESSAGE_KEYWORDS = ("Map tracking inference completed",)

    def __init__(self, log_file: str = SERVICE_LOG_FILE):
        self.log_file = log_file

    def _read_last_lines(self) -> list[str]:
        if not os.path.exists(self.log_file):
            return []
        try:
            with open(self.log_file, "rb") as f:
                f.seek(0, os.SEEK_END)
                end_pos = f.tell()
                if end_pos == 0:
                    return []

                buffer = b""
                pos = end_pos
                lines: list[bytes] = []
                while pos > 0 and len(lines) <= self.MAX_LOOKBACK_LINES:
                    read_size = min(self.READ_CHUNK_SIZE, pos)
                    pos -= read_size
                    f.seek(pos, os.SEEK_SET)
                    chunk = f.read(read_size)
                    if not chunk:
                        break
                    buffer = chunk + buffer
                    while b"\n" in buffer:
                        head, buffer = buffer.rsplit(b"\n", 1)
                        lines.append(buffer)
                        buffer = head
                        if len(lines) > self.MAX_LOOKBACK_LINES:
                            break
                if buffer and len(lines) <= self.MAX_LOOKBACK_LINES:
                    lines.append(buffer)
                lines = list(reversed(lines))
                return [
                    line.decode("utf-8", errors="ignore")
                    for line in lines[-self.MAX_LOOKBACK_LINES :]
                    if line
                ]
        except Exception:
            return []

    def _is_target_message(self, message: str | None) -> bool:
        if not message:
            return False
        return any(key in message for key in self.MESSAGE_KEYWORDS)

    def wait_for_new_location(
        self, expected_map_name: str, timeout_seconds: float = 5.0
    ) -> "LocationService.LocationServiceResult":
        if not os.path.exists(self.log_file):
            return self.LocationServiceResult("not_found", "Log file not found.")

        try:
            with open(self.log_file, "rb") as f:
                f.seek(0, os.SEEK_END)
                pos = f.tell()
                buffer = b""
                start_time = time.time()

                while time.time() - start_time <= timeout_seconds:
                    try:
                        f.seek(0, os.SEEK_END)
                        end_pos = f.tell()
                        if end_pos < pos:
                            pos = end_pos
                            buffer = b""
                        if end_pos > pos:
                            f.seek(pos, os.SEEK_SET)
                            data = f.read(end_pos - pos)
                            pos = end_pos
                            if data:
                                buffer += data
                                while b"\n" in buffer:
                                    line, buffer = buffer.split(b"\n", 1)
                                    if not line:
                                        continue
                                    try:
                                        text = line.decode("utf-8", errors="ignore")
                                        data_obj = json.loads(text)
                                    except Exception:
                                        continue

                                    if not isinstance(data_obj, dict):
                                        continue
                                    if not self._is_target_message(
                                        data_obj.get("message")
                                    ):
                                        continue
                                    if data_obj.get("hit") is False:
                                        continue

                                    log_map_name = data_obj.get(
                                        "mapName"
                                    ) or data_obj.get("map_name")
                                    if not log_map_name:
                                        continue
                                    if norm_map_name(log_map_name) != norm_map_name(
                                        expected_map_name
                                    ):
                                        return self.LocationServiceResult(
                                            "mismatch", str(log_map_name)
                                        )

                                    x = data_obj.get("x")
                                    y = data_obj.get("y")
                                    if x is None or y is None:
                                        continue

                                    print(f"  {_G}Realtime location fetched.{_0}")
                                    return self.LocationServiceResult(
                                        "ok",
                                        {
                                            "x": int(round(x)),
                                            "y": int(round(y)),
                                            "raw": data_obj,
                                        },
                                    )
                    except Exception:
                        pass

                    cv2.waitKey(1)
                    time.sleep(0.05)

        except Exception:
            print(f"  {_R}Location service unavailable because error reading log file.")
            print(
                f"    {_Y}Please ensure the node 'MapTrackerTestLoop' is running.{_0}"
            )
            print("  Check the documentation for more details.")
            return self.LocationServiceResult("not_found", "Log file unavailable.")

        print(f"  {_R}Timeout connecting to location service. Please ensure:{_0}")
        print(f"    {_Y}- The node 'MapTrackerTestLoop' is running.{_0}")
        print(f"    {_Y}- The game window can be fully captured.{_0}")
        print("    Check the documentation for more details.")
        return self.LocationServiceResult(
            "not_found",
            "Timeout connecting to location service. Check the console for details.",
        )

    def get_latest_location(
        self, expected_map_name: str
    ) -> "LocationService.LocationServiceResult":
        lines = self._read_last_lines()
        if not lines:
            return self.LocationServiceResult(
                "not_found", "Log file not found or empty."
            )

        for line in reversed(lines):
            try:
                data = json.loads(line)
            except Exception:
                continue

            if not isinstance(data, dict):
                continue

            if not self._is_target_message(data.get("message")):
                continue

            if data.get("hit") is False:
                continue

            log_map_name = data.get("mapName") or data.get("map_name")
            if not log_map_name:
                continue
            if norm_map_name(log_map_name) != norm_map_name(expected_map_name):
                return self.LocationServiceResult("mismatch", str(log_map_name))

            x = data.get("x")
            y = data.get("y")
            if x is None or y is None:
                continue

            return self.LocationServiceResult(
                "ok", {"x": int(round(x)), "y": int(round(y)), "raw": data}
            )

        return self.LocationServiceResult("not_found", "No matching log entry.")


class PipelineHandler:
    """Handle reading and writing of Pipeline JSON, using regex to preserve comments and formatting.

    All node data parsed from the file is stored in ``self.nodes`` (a dict keyed by node
    name).  Each entry is a dict with at minimum the raw ``content`` text and, for
    MapTrackerMove nodes, the structured fields (``map_name``, ``path``, …).
    """

    def __init__(self, file_path):
        self.file_path = file_path
        self._content = ""
        # Full node registry: node_name -> {content, map_name?, path?, is_new_structure?}
        self.nodes: dict[str, dict] = {}

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    def _load(self):
        """Load file content into ``self._content``.  Returns True on success."""
        try:
            with open(self.file_path, "r", encoding="utf-8") as f:
                self._content = f.read()
            return True
        except Exception as e:
            print(f"{_R}Error reading file:{_0} {e}")
            return False

    @staticmethod
    def _parse_tracker_fields(node_content: str) -> dict | None:
        """Extract MapTrackerMove fields from a node body.  Returns None if not a tracker node."""
        if '"custom_action": "MapTrackerMove"' not in node_content:
            return None

        is_new_structure = re.search(r'"action"\s*:\s*\{', node_content) is not None

        m_match = re.search(r'"map_name"\s*:\s*"([^"]+)"', node_content)
        map_name = m_match.group(1) if m_match else "Unknown"

        t_match = re.search(r'"path"\s*:\s*(\[[\s\S]*?\]\s*\]|\[\s*\])', node_content)
        if not t_match:
            return None
        try:
            path = json.loads(t_match.group(1))
        except Exception:
            return None

        return {
            "map_name": map_name,
            "path": path,
            "is_new_structure": is_new_structure,
        }

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    def read_all_nodes(self) -> bool:
        """Parse **all** top-level nodes from the file into ``self.nodes``.

        Returns True on success.  MapTrackerMove nodes get the extra tracker fields.
        """
        if not self._load():
            return False

        self.nodes.clear()
        node_pattern = re.compile(
            r'^\s*"([^"]+)"\s*:\s*(\{[\s\S]*?\n\s*\})', re.MULTILINE
        )
        for match in node_pattern.finditer(self._content):
            node_name = match.group(1)
            node_content = match.group(2)
            entry: dict = {"content": node_content}
            tracker = self._parse_tracker_fields(node_content)
            if tracker is not None:
                entry.update(tracker)
                entry["is_tracker"] = True
            else:
                entry["is_tracker"] = False
            self.nodes[node_name] = entry
        return True

    def read_nodes(self) -> list[dict]:
        """Read all MapTrackerMove nodes.  Populates ``self.nodes`` as a side-effect.

        Returns a list of dicts compatible with the original interface.
        """
        self.read_all_nodes()
        results = []
        for node_name, entry in self.nodes.items():
            if entry.get("is_tracker"):
                results.append(
                    {
                        "node_name": node_name,
                        "map_name": entry["map_name"],
                        "path": entry["path"],
                        "is_new_structure": entry["is_new_structure"],
                    }
                )
        return results

    def get_tracker_nodes(self) -> list[dict]:
        """Return a list of all MapTrackerMove node summaries (same shape as read_nodes)."""
        return [
            {
                "node_name": name,
                "map_name": entry["map_name"],
                "path": entry["path"],
                "is_new_structure": entry["is_new_structure"],
            }
            for name, entry in self.nodes.items()
            if entry.get("is_tracker")
        ]

    def replace_path(self, node_name: str, new_path: list) -> bool:
        """Regex-replace the path list for *node_name* in the pipeline file.

        Updates ``self.nodes`` on success so the in-memory state stays current.
        """
        if not self._load():
            return False

        node_pattern = re.compile(
            r'^(\s*"' + re.escape(node_name) + r'"\s*:\s*\{)([\s\S]*?\n\s*\})',
            re.MULTILINE,
        )
        node_match = node_pattern.search(self._content)
        if not node_match:
            print(f"{_R}Error: Node {node_name} not found in file when saving.{_0}")
            return False

        body = node_match.group(2)

        path_pattern = re.compile(
            r'("path"\s*:\s*)(\[[\s\S]*?\]\s*\]|\[\s*\])',
            re.MULTILINE,
        )
        path_match = path_pattern.search(body)
        if not path_match:
            print(
                f"{_R}Error: 'path' field not found in node {node_name} when saving.{_0}"
            )
            return False

        # Format new path following multi-line array convention
        if self.nodes.get(node_name, {}).get("is_new_structure", False):
            indent_sm = " " * 20
            indent_lg = " " * 24
        else:
            indent_sm = " " * 12
            indent_lg = " " * 16

        if not new_path:
            formatted_path = "[]"
        else:
            formatted_path = "[\n"
            for i, p in enumerate(new_path):
                comma = "," if i < len(new_path) - 1 else ""
                formatted_path += f"{indent_lg}[{p[0]}, {p[1]}]{comma}\n"
            formatted_path += f"{indent_sm}]"

        new_body = (
            body[: path_match.start(2)] + formatted_path + body[path_match.end(2) :]
        )
        new_content = (
            self._content[: node_match.start(2)]
            + new_body
            + self._content[node_match.end(2) :]
        )

        try:
            with open(self.file_path, "w", encoding="utf-8") as f:
                f.write(new_content)
        except Exception as e:
            print(f"{_R}Error writing file:{_0} {e}")
            return False

        # Keep in-memory state consistent
        if node_name in self.nodes:
            self.nodes[node_name]["path"] = [[int(p[0]), int(p[1])] for p in new_path]
        return True


def main():
    print(f"{_G}Welcome to MapTracker tool.{_0}")
    print(f"\n{_Y}Select a mode:{_0}")
    print(f"  {_C}[N]{_0} Create a new path")
    print(f"  {_C}[I]{_0} Import an existing path from pipeline file")

    mode = input("> ").strip().upper()

    map_name = None
    points = []

    # Store context for "Replace" functionality
    import_context = None

    if mode == "N":
        print("\n----------\n")
        print(f"{_Y}Please choose a map in the window.{_0}")
        # Step 1: Select Map
        map_selector = SelectMapPage()
        map_name = map_selector.run()
        if not map_name:
            print(f"\n{_Y}No map selected. Exiting.{_0}")
            return

        # Step 2: Edit Path (Empty initially)
        print(f"  Selected map: {map_name}")
        print(f"\n{_Y}Please edit the path in the window.{_0}")
        print("  Close the window when done.")
        try:
            editor = PathEditPage(map_name, [])
            points = editor.run()
        except ValueError as e:
            print(f"{_R}Error initializing editor:{_0} {e}")
            return

    elif mode == "I":
        print("\n----------\n")
        print(f"{_Y}Where's your pipeline JSON file path?{_0}")
        file_path = input("> ").strip()
        file_path = file_path.strip('"').strip("'")

        handler = PipelineHandler(file_path)
        candidates = handler.read_nodes()

        if not candidates:
            print(f"{_R}No 'MapTrackerMove' nodes found in the file.{_0}")
            print(
                "Please make sure your JSON file contains nodes with 'custom_action' set to 'MapTrackerMove'."
            )
            return

        print(f"\n{_Y}Which node do you want to import?{_0}")
        for i, c in enumerate(candidates):
            print(
                f"  {_C}[{i+1}]{_0} {c['node_name']} {_A}(Map: {c['map_name']}, Points: {len(c['path'])}){_0}"
            )

        try:
            sel = int(input("> ")) - 1
            if not (0 <= sel < len(candidates)):
                print(f"{_R}Invalid selection.{_0}")
                return
            selected_node = candidates[sel]

            original_map_name = selected_node["map_name"]
            initial_points = selected_node["path"]

            # Try to resolve the actual map filename on disk (keeping suffix) for editing
            resolved = find_map_file(original_map_name)
            editor_map_name = resolved if resolved is not None else original_map_name

            print(
                f"  Editing node: {selected_node['node_name']} on map {original_map_name}"
            )
            print(f"\n{_Y}Please edit the path in the window.{_0}")
            print("  Close the window when done.")

            try:
                editor = PathEditPage(
                    editor_map_name,
                    initial_points,
                    pipeline_context={
                        "handler": handler,
                        "node_name": selected_node["node_name"],
                        "file_path": file_path,
                    },
                )
                points = editor.run()

                if not editor.is_dirty:
                    print("\n----------\n")
                    print(f"{_G}Finished editing.{_0}")
                    print("  All done! No unsaved changes.")
                    return

                # Setup context for Replace; keep original name from node for export normalization
                import_context = {
                    "file_path": file_path,
                    "handler": handler,
                    "node_name": selected_node["node_name"],
                    "original_map_name": original_map_name,
                    "is_new_structure": selected_node.get("is_new_structure", False),
                }

            except ValueError as e:
                print(f"{_R}Error initializing editor{_0}: {e}")
                return

        except ValueError:
            print(f"{_R}Invalid input.{_0}")
            return

    else:
        print(f"{_R}Invalid mode.{_0}")
        return

    # Export Logic
    print("\n----------\n")
    print(f"{_G}Finished editing.{_0}")
    print(f"  Total {len(points)} points")
    print(f"\n{_Y}Select an export mode:{_0}")
    if import_context:
        print(f"  {_C}[S]{_0} Save the changes back to file")
        print(f"      {_A}which will replace the path in the pipeline node.{_0}")
    print(f"  {_C}[J]{_0} Print the node JSON string")
    print(f"      {_A}which represents a new pipeline node.{_0}")
    print(f"  {_C}[D]{_0} Print the parameters dict")
    print(f"      {_A}which can be used as 'custom_action_param' field.{_0}")
    print(f"  {_C}[L]{_0} Print the point list")
    print(f"      {_A}which can be used as 'path'{_A} field.{_0}")

    export_mode = input("> ").strip().upper()

    raw_map_name = (
        import_context.get("original_map_name", map_name)
        if import_context
        else map_name
    )
    param_data = {
        "map_name": norm_map_name(raw_map_name),
        "path": [[int(p[0]), int(p[1])] for p in points],
    }

    if export_mode == "S" and import_context:
        handler = import_context["handler"]
        node_name = import_context["node_name"]
        if handler.replace_path(node_name, points):
            print(f"\n{_G}Successfully updated node {_0}'{node_name}'")
        else:
            print(f"\n{_R}Failed to update node.{_0}")

    elif export_mode == "J":
        is_new = (
            import_context.get("is_new_structure", False) if import_context else False
        )
        if is_new:
            node_data = {
                "action": {
                    "custom_action": "MapTrackerMove",
                    "custom_action_param": param_data,
                }
            }
        else:
            node_data = {
                "action": "Custom",
                "custom_action": "MapTrackerMove",
                "custom_action_param": param_data,
            }

        snippet = {"NodeName": node_data}
        print(f"\n{_C}--- JSON Snippet ---{_0}\n")
        print(json.dumps(snippet, indent=4, ensure_ascii=False))

    elif export_mode == "D":
        print(f"\n{_C}--- Parameters Dict ---{_0}\n")
        print(json.dumps(param_data, indent=None, ensure_ascii=False))

    else:
        SIMPact_str = "[" + ", ".join([str(p) for p in points]) + "]"
        if export_mode == "L":
            print(f"\n{_C}--- Point List ---{_0}\n")
            print(SIMPact_str)
        else:
            print(f"{_Y}Invalid export mode.{_0}")
            print(f"  To prevent data loss, the point list is printed below.{_0}")
            print(f"\n{_C}--- Point List ---{_0}\n")
            print(SIMPact_str)


if __name__ == "__main__":
    main()
