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
from datetime import datetime, timezone
from typing import NamedTuple
import numpy as np
from utils import (
    _R,
    _G,
    _Y,
    _C,
    _A,
    _0,
    Color,
    Drawer,
    cv2,
    MapName,
    SelectMapPage,
    ViewportManager,
    Layer,
)


MAP_DIR = "assets/resource/image/MapTracker/map"
SERVICE_LOG_FILE = "install/debug/go-service.log"


class _MapLayer(Layer):
    def __init__(self, view: ViewportManager, img: np.ndarray):
        super().__init__(view)
        self._img = img
        self._scaled_img: np.ndarray | None = None
        self._scaled_zoom: float | None = None

    def render(self, drawer: Drawer) -> None:
        zoom = self.view.zoom
        if self._scaled_img is None or self._scaled_zoom != zoom:
            scaled_w = max(1, int(self._img.shape[1] * zoom))
            scaled_h = max(1, int(self._img.shape[0] * zoom))
            self._scaled_img = cv2.resize(
                self._img, (scaled_w, scaled_h), interpolation=cv2.INTER_AREA
            )
            self._scaled_zoom = zoom

        scaled_img = self._scaled_img
        if scaled_img is None:
            return

        scaled_h, scaled_w = scaled_img.shape[:2]
        src_x1 = int(round(self.view._vx * zoom))
        src_y1 = int(round(self.view._vy * zoom))
        dst_x = max(0, -src_x1)
        dst_y = max(0, -src_y1)
        src_x1 = max(0, src_x1)
        src_y1 = max(0, src_y1)
        src_x2 = min(scaled_w, src_x1 + drawer.w - dst_x)
        src_y2 = min(scaled_h, src_y1 + drawer.h - dst_y)

        copy_w = src_x2 - src_x1
        copy_h = src_y2 - src_y1
        if copy_w > 0 and copy_h > 0:
            drawer.get_image()[dst_y : dst_y + copy_h, dst_x : dst_x + copy_w] = (
                scaled_img[src_y1:src_y2, src_x1:src_x2]
            )


class _RealtimePathLayer(Layer):
    def __init__(self, view: ViewportManager, page: "PathEditPage"):
        super().__init__(view)
        self._page = page

    def render(self, drawer: Drawer) -> None:
        points = self._page._recorded_path
        if len(points) < 2:
            return
        for i in range(1, len(points)):
            psx, psy = self.view.get_view_coords(points[i - 1][0], points[i - 1][1])
            sx, sy = self.view.get_view_coords(points[i][0], points[i][1])
            drawer.line(
                (psx, psy),
                (sx, sy),
                color=0x22AAFF,
                thickness=max(1, int(self._page.LINE_WIDTH * self.view.zoom**0.5)),
            )


class _PathLayer(Layer):
    def __init__(self, view: ViewportManager, page: "PathEditPage"):
        super().__init__(view)
        self._page = page

    def render(self, drawer: Drawer) -> None:
        points = self._page.points
        # Draw path lines
        for i in range(len(points)):
            sx, sy = self.view.get_view_coords(points[i][0], points[i][1])
            if i > 0:
                psx, psy = self.view.get_view_coords(points[i - 1][0], points[i - 1][1])
                drawer.line(
                    (psx, psy),
                    (sx, sy),
                    color=0xFF0000,
                    thickness=max(1, int(self._page.LINE_WIDTH * self.view.zoom**0.5)),
                )

        # Draw point circles
        for i in range(len(points)):
            sx, sy = self.view.get_view_coords(points[i][0], points[i][1])
            drawer.circle(
                (sx, sy),
                int(self._page.POINT_RADIUS * max(0.5, self.view.zoom**0.5)),
                color=0xFFA500 if i == self._page.drag_idx else 0xFF0000,
                thickness=-1,
            )

        # Draw point index labels
        for i in range(len(points)):
            sx, sy = self.view.get_view_coords(points[i][0], points[i][1])
            drawer.text(str(i), (sx + 5, sy - 5), 0.5, color=0xFFFFFF, thickness=1)


class PathEditPage:
    """Path editing page"""

    SIDEBAR_W = 240
    STATUS_BAR_H = 32
    QUICK_BAR_H = 32
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
        self.window_w, self.window_h = 1280, 720
        self.window_name = "MapTracker Tool - Path Editor"
        self.view = ViewportManager(
            self.window_w, self.window_h, zoom=1.0, min_zoom=0.5, max_zoom=10.0
        )
        self._map_layer = _MapLayer(self.view, self.img)
        self._path_layer = _PathLayer(self.view, self)
        self._realtime_layer = _RealtimePathLayer(self.view, self)
        self.view.fit_to(self.points)

        self.drag_idx = -1
        self.selected_idx = -1
        self.panning = False
        self.pan_start = (0, 0)
        self.mouse_pos: tuple[int, int] = (-1, -1)  # For crosshair display

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
        self.location_service = LocationService()
        self._recording_active = False
        self._recording_start_time = 0.0
        self._recording_last_ts = 0.0
        self._recording_last_poll = 0.0
        self._recorded_path: list[list[int]] = []
        self._recorded_keys: set[tuple[float, int, int]] = set()

        # Button hit-rects: (x1, y1, x2, y2) – populated by _render_sidebar
        self._btn_save_rect: tuple | None = None
        self._btn_record_rect: tuple | None = None
        self._btn_finish_rect: tuple | None = None
        self._btn_quick_generate_rect: tuple | None = None
        self._btn_quick_undo_rect: tuple | None = None
        self._quick_undo_state: dict | None = None
        self._frame_interval = 1.0 / 120.0
        self._last_render_ts = 0.0

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

    def _start_recording(self):
        self._recording_active = True
        self._recording_start_time = time.time()
        self._recording_last_ts = self._recording_start_time
        self._recording_last_poll = 0.0
        self._recorded_path = []
        self._recorded_keys.clear()
        self._update_status(0x78DCFF, "Realtime path recording started.")
        self.render_page(force=True)

    def _stop_recording(self):
        self._recording_active = False
        self._update_status(0xD2D200, "Realtime path recording stopped.")
        self.render_page(force=True)

    def _toggle_recording(self):
        if self._recording_active:
            self._stop_recording()
        else:
            self._start_recording()

    def _update_recording(self):
        if not self._recording_active:
            return False
        now = time.time()
        if now - self._recording_last_poll < 0.5:
            return False
        self._recording_last_poll = now

        locations = self.location_service.get_locations(
            self.map_name, self._recording_last_ts
        )
        if not locations:
            return False

        updated = False
        for loc in locations:
            ts = loc.get("timestamp")
            if ts is None or ts < self._recording_last_ts:
                continue
            x = loc.get("x")
            y = loc.get("y")
            if x is None or y is None:
                continue
            key = (ts, int(x), int(y))
            if key in self._recorded_keys:
                self._recording_last_ts = max(self._recording_last_ts, ts)
                continue
            if self._recorded_path and [x, y] == self._recorded_path[-1]:
                self._recording_last_ts = max(self._recording_last_ts, ts)
                continue
            self._recorded_path.append([x, y])
            self._recorded_keys.add(key)
            self._recording_last_ts = max(self._recording_last_ts, ts)
            updated = True

        if updated:
            if self._quick_undo_state and self._recorded_path:
                self._quick_undo_state = None
            if self._recorded_path:
                last_point = self._recorded_path[-1]
                self.view.maybe_center_to(last_point[0], last_point[1])
            self.render_page()
        return updated

    @staticmethod
    def _angle_close(v1: tuple[float, float], v2: tuple[float, float]) -> bool:
        x1, y1 = v1
        x2, y2 = v2
        n1 = math.hypot(x1, y1)
        n2 = math.hypot(x2, y2)
        if n1 == 0.0 or n2 == 0.0:
            return False
        dot = x1 * x2 + y1 * y2
        cos_val = max(-1.0, min(1.0, dot / (n1 * n2)))
        angle = math.degrees(math.acos(cos_val))
        return angle < 12.0

    def _generate_path_from_recorded(self):
        if len(self._recorded_path) < 2:
            return
        self._quick_undo_state = {
            "points": [list(p) for p in self.points],
            "recorded_path": [list(p) for p in self._recorded_path],
            "recorded_keys": set(self._recorded_keys),
            "selected_idx": self.selected_idx,
            "recording_active": self._recording_active,
            "recording_start_time": self._recording_start_time,
            "recording_last_ts": self._recording_last_ts,
            "recording_last_poll": self._recording_last_poll,
        }
        result: list[list[int]] = []
        for point in self._recorded_path:
            if len(result) < 2:
                result.append([point[0], point[1]])
                continue
            p2 = result[-2]
            p1 = result[-1]
            v1 = (point[0] - p2[0], point[1] - p2[1])
            v2 = (point[0] - p1[0], point[1] - p1[1])
            if self._angle_close(v1, v2):
                result.pop()
            result.append([point[0], point[1]])
        self.points = result
        self.selected_idx = len(self.points) - 1 if self.points else -1
        self._recorded_path = []
        self._recorded_keys.clear()
        self._recording_active = False
        self._update_status(
            0x50DC50, f"Generated path from realtime history ({len(self.points)} pts)"
        )

    def _undo_generate_path(self):
        if not self._quick_undo_state:
            return
        self.points = [list(p) for p in self._quick_undo_state["points"]]
        self._recorded_path = [list(p) for p in self._quick_undo_state["recorded_path"]]
        self._recorded_keys = set(self._quick_undo_state["recorded_keys"])
        self.selected_idx = int(self._quick_undo_state["selected_idx"])
        self._recording_active = bool(self._quick_undo_state["recording_active"])
        self._recording_start_time = float(
            self._quick_undo_state["recording_start_time"]
        )
        self._recording_last_ts = float(self._quick_undo_state["recording_last_ts"])
        self._recording_last_poll = float(self._quick_undo_state["recording_last_poll"])
        self._quick_undo_state = None
        self._update_status(0xD2D200, "Reverted the generated path.")

    def _get_map_coords(self, screen_x, screen_y):
        """Convert screen (viewport) coordinates to original map coordinates.

        The usable map area starts at x = SIDEBAR_W.
        """
        return self.view.get_real_coords(screen_x, screen_y)

    def _get_screen_coords(self, map_x, map_y):
        """Convert original map coordinates to screen (viewport) coordinates."""
        return self.view.get_view_coords(map_x, map_y)

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

    def render_page(self, *, force: bool = False):
        now = time.monotonic()
        if force or now - self._last_render_ts >= self._frame_interval:
            self._last_render_ts = now
            self._render()

    def _render(self):
        drawer = Drawer.new(self.window_w, self.window_h)
        self._map_layer.render(drawer)
        self._realtime_layer.render(drawer)
        self._path_layer.render(drawer)

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

        self._render_quick_bar(drawer)
        self._render_status_bar(drawer)
        self._render_sidebar(drawer)
        cv2.imshow(self.window_name, drawer.get_image())

    def _render_quick_bar(self, drawer: "Drawer"):
        x1 = self.SIDEBAR_W
        x2 = self.window_w
        y2 = max(0, self.window_h - self.STATUS_BAR_H)
        y1 = max(0, y2 - self.QUICK_BAR_H)
        self._btn_quick_generate_rect = None
        self._btn_quick_undo_rect = None

        if self._quick_undo_state and len(self._recorded_path) == 0:
            drawer.rect((x1, y1), (x2, y2), color=0x000000, thickness=-1)
            prompt = "You can undo the previous path generation."
            drawer.text(
                prompt,
                (x1 + 10, y2 - 10),
                0.45,
                color=0xFFFFFF,
                thickness=1,
            )

            btn_label = "[Undo!]"
            btn_size = drawer.get_text_size(btn_label, 0.45, thickness=1)
            btn_pad_x = 12
            btn_pad_y = 6
            btn_w = btn_size[0] + btn_pad_x * 2
            btn_h = btn_size[1] + btn_pad_y * 2
            btn_x2 = x2 - 10
            btn_x1 = btn_x2 - btn_w
            btn_y1 = y1 + (self.QUICK_BAR_H - btn_h) // 2
            btn_y2 = btn_y1 + btn_h
            self._btn_quick_undo_rect = (btn_x1, btn_y1, btn_x2, btn_y2)
            drawer.rect(
                (btn_x1, btn_y1), (btn_x2, btn_y2), color=0xB44022, thickness=-1
            )
            drawer.rect((btn_x1, btn_y1), (btn_x2, btn_y2), color=0xB4B4B4, thickness=1)
            drawer.text_centered(
                btn_label,
                (btn_x1 + btn_w // 2, btn_y2 - btn_pad_y),
                0.45,
                color=0xFFFFFF,
                thickness=1,
            )
            return

        if len(self._recorded_path) < 2:
            return

        drawer.rect((x1, y1), (x2, y2), color=0x000000, thickness=-1)
        prompt = "Do you want to generate a new path from the realtime path record?"
        prompt_x = x1 + 10
        prompt_y = y2 - 10
        drawer.text(
            prompt,
            (prompt_x, prompt_y),
            0.45,
            color=0x50DC50,
            thickness=1,
        )

        btn_label = "[Sure!]"
        btn_size = drawer.get_text_size(btn_label, 0.45, thickness=1)
        btn_pad_x = 12
        btn_pad_y = 6
        btn_w = btn_size[0] + btn_pad_x * 2
        btn_h = btn_size[1] + btn_pad_y * 2
        btn_x2 = x2 - 10
        btn_x1 = btn_x2 - btn_w
        btn_y1 = y1 + (self.QUICK_BAR_H - btn_h) // 2
        btn_y2 = btn_y1 + btn_h
        self._btn_quick_generate_rect = (btn_x1, btn_y1, btn_x2, btn_y2)
        drawer.rect((btn_x1, btn_y1), (btn_x2, btn_y2), color=0x1C8A1C, thickness=-1)
        drawer.rect((btn_x1, btn_y1), (btn_x2, btn_y2), color=0xB4B4B4, thickness=1)
        drawer.text_centered(
            btn_label,
            (btn_x1 + btn_w // 2, btn_y2 - btn_pad_y),
            0.45,
            color=0xFFFFFF,
            thickness=1,
        )

    def _render_status_bar(self, drawer: "Drawer"):
        x1 = self.SIDEBAR_W
        x2 = self.window_w
        y2 = self.window_h
        y1 = max(0, y2 - self.STATUS_BAR_H)
        drawer.rect((x1, y1), (x2, y2), color=0x000000, thickness=-1)
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
        """Draw the left sidebar with a solid black background."""
        sw = self.SIDEBAR_W
        h = self.window_h
        pad = 15

        # ── Extract and blend sidebar background ──────────────────────────
        drawer.rect((0, 0), (sw, h), color=0x000000, thickness=-1)

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

        # Realtime path recording button
        record_y0 = cy
        record_y1 = cy + btn_h
        self._btn_record_rect = (btn_x0, record_y0, btn_x0 + btn_w, record_y1)
        drawer.rect(
            (btn_x0, record_y0),
            (btn_x0 + btn_w, record_y1),
            color=0x1A40B8,
            thickness=-1,
        )
        drawer.rect(
            (btn_x0, record_y0),
            (btn_x0 + btn_w, record_y1),
            color=0xB4B4B4,
            thickness=1,
        )
        record_label = (
            "[R] Stop Path Recording"
            if self._recording_active
            else "[R] Record Realtime Path"
        )
        drawer.text_centered(
            record_label,
            (btn_x0 + btn_w // 2, record_y0 + btn_h - 8),
            0.42,
            color=0xFFFFFF,
            thickness=1,
        )
        cy = record_y1 + 8

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
            f"Zoom: {self.view.zoom:.2f}x",
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
        record_line = f"History: {len(self._recorded_path)}"
        if self._recording_active:
            record_line += " (Recording)"
        drawer.text(record_line, (pad, h - 25), 0.4, color=0x8FC8FF, thickness=1)

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
        # ── Map area events ──────────────────────────────────────────────
        mx, my = self._get_map_coords(x, y)
        if event == cv2.EVENT_MOUSEWHEEL:
            if flags > 0:
                self.view.zoom_in()
            else:
                self.view.zoom_out()

            self.view.set_view_origin(mx - x / self.view.zoom, my - y / self.view.zoom)
            self.render_page()

        elif event == cv2.EVENT_MOUSEMOVE:
            # Pan
            if self.panning:
                dx = (x - self.pan_start[0]) / self.view.zoom
                dy = (y - self.pan_start[1]) / self.view.zoom
                self.view.pan_by(-dx, -dy)
                self.pan_start = (x, y)
                self.render_page()
                return

            # Action (left button) dragging
            if self.action_mouse_down:
                if self.action_dragging and self.drag_idx != -1:
                    self.points[self.drag_idx] = [mx, my]
                    self.action_moved = True
                    self.render_page()
                    return

                dx = x - self.action_down_pos[0]
                dy = y - self.action_down_pos[1]
                if dx * dx + dy * dy > 25:
                    self.action_moved = True
                    if self.action_down_idx != -1:
                        self.action_dragging = True
                        self.drag_idx = self.action_down_idx
                        self.points[self.drag_idx] = [mx, my]
                        self.render_page()
                        return

            if (flags & cv2.EVENT_FLAG_LBUTTON) and self.drag_idx != -1:
                self.points[self.drag_idx] = [mx, my]
                self.action_dragging = True
            self.render_page()

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
                    self.render_page(force=True)
                elif self._hit_button(x, y, self._btn_record_rect):
                    self._toggle_recording()
                elif self._hit_button(x, y, self._btn_finish_rect):
                    self.done = True
                return  # Prevent event propagation

            if self._hit_button(x, y, self._btn_quick_generate_rect):
                self._generate_path_from_recorded()
                self.render_page(force=True)
                return
            if self._hit_button(x, y, self._btn_quick_undo_rect):
                self._undo_generate_path()
                self.render_page(force=True)
                return

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
                                0.01, self.view.zoom
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
            self.render_page()

    # ------------------------------------------------------------------
    # Main loop
    # ------------------------------------------------------------------

    def run(self):
        cv2.namedWindow(self.window_name)
        cv2.setMouseCallback(self.window_name, self._handle_mouse)

        self.render_page(force=True)
        while not self.done:
            if cv2.getWindowProperty(self.window_name, cv2.WND_PROP_VISIBLE) < 1:
                break
            key = cv2.waitKey(1) & 0xFF
            if key == 27 or key == ord("f") or key == ord("F"):  # ESC / F → Finish
                break
            if (
                (key == ord("s") or key == ord("S"))
                and self.pipeline_context
                and self.is_dirty
            ):
                self._do_save()
                self.render_page(force=True)
            if key == ord("r") or key == ord("R"):
                self._toggle_recording()
            self._update_recording()

        cv2.destroyAllWindows()
        return [list(p) for p in self.points]


def find_map_file(name: str, map_dir: str = MAP_DIR) -> str | None:
    """Find the filename corresponding to the given name on disk (keeping the suffix), return the filename or None."""
    if not os.path.isdir(map_dir):
        return None
    files = os.listdir(map_dir)
    if name in files:
        return name

    target_key = unique_map_key(name)
    for file_name in files:
        if unique_map_key(file_name) == target_key:
            return file_name
    return None


def unique_map_key(name: str) -> str:
    """Normalize map name for semantic comparison."""
    try:
        parsed = MapName.parse(name)
        if parsed.map_type == "tier":
            if not parsed.tier_suffix:
                return f"{parsed.map_type}:{parsed.map_id}:{parsed.map_level_id}"
            return (
                f"{parsed.map_type}:{parsed.map_id}:"
                f"{parsed.map_level_id}:{parsed.tier_suffix}"
            )
        return f"{parsed.map_type}:{parsed.map_id}:{parsed.map_level_id}"
    except ValueError:
        basename = os.path.basename(name.replace("\\", "/"))
        stem, _ = os.path.splitext(basename)
        return stem.lower()


class LocationService:
    """Read locations from a jsonl service log."""

    MESSAGE_KEYWORDS = ("Map tracking inference completed",)

    def __init__(self, log_file: str = SERVICE_LOG_FILE):
        self.log_file = log_file
        self._offset = 0
        self._buffer = b""
        self._last_map_key: str | None = None
        self._last_start_time = 0.0

    def _is_target_message(self, message: str | None) -> bool:
        if not message:
            return False
        return any(key in message for key in self.MESSAGE_KEYWORDS)

    def _parse_timestamp(self, value) -> float | None:
        if isinstance(value, (int, float)):
            return float(value)
        if isinstance(value, str):
            try:
                return float(value)
            except ValueError:
                pass
            try:
                if value.endswith("Z"):
                    value = value[:-1] + "+00:00"
                parsed = datetime.fromisoformat(value)
                if parsed.tzinfo is None:
                    parsed = parsed.replace(tzinfo=timezone.utc)
                return parsed.timestamp()
            except ValueError:
                return None
        return None

    def _parse_location_line(self, line: str, expected_map_name: str) -> dict | None:
        try:
            data_obj = json.loads(line)
        except Exception:
            return None
        if not isinstance(data_obj, dict):
            return None
        if not self._is_target_message(data_obj.get("message")):
            return None

        log_map_name = data_obj.get("MapName")
        if not log_map_name:
            return None
        if unique_map_key(log_map_name) != unique_map_key(expected_map_name):
            return None

        x = data_obj.get("X")
        y = data_obj.get("Y")
        if x is None or y is None:
            return None

        ts = None
        for key in ("time", "timestamp", "ts"):
            if key in data_obj:
                ts = self._parse_timestamp(data_obj.get(key))
                if ts is not None:
                    break

        return {
            "x": int(round(x)),
            "y": int(round(y)),
            "timestamp": ts,
            "raw": data_obj,
        }

    def get_locations(self, expected_map_name: str, start_time: float) -> list[dict]:
        if not os.path.exists(self.log_file):
            return []

        map_key = unique_map_key(expected_map_name)
        if self._last_map_key != map_key or start_time < self._last_start_time:
            self._offset = 0
            self._buffer = b""
        self._last_map_key = map_key
        self._last_start_time = start_time

        results: list[dict] = []
        try:
            with open(self.log_file, "rb") as f:
                f.seek(0, os.SEEK_END)
                end_pos = f.tell()
                if end_pos < self._offset:
                    self._offset = 0
                    self._buffer = b""
                if end_pos > self._offset:
                    f.seek(self._offset, os.SEEK_SET)
                    data = f.read(end_pos - self._offset)
                    self._offset = end_pos
                    if data:
                        self._buffer += data

            if self._buffer:
                lines = self._buffer.split(b"\n")
                self._buffer = lines[-1]
                for raw in lines[:-1]:
                    if not raw:
                        continue
                    line = raw.decode("utf-8", errors="ignore")
                    if not line.strip():
                        continue
                    record = self._parse_location_line(line, expected_map_name)
                    if record is None:
                        continue
                    ts = record.get("timestamp")
                    if ts is None or ts < start_time:
                        continue
                    results.append(record)
        except Exception:
            return []

        results.sort(key=lambda item: item.get("timestamp") or 0.0)
        deduped: list[dict] = []
        last_xy: tuple[int, int] | None = None
        for item in results:
            x = item.get("x")
            y = item.get("y")
            if x is None or y is None:
                continue
            xy = (int(x), int(y))
            if last_xy == xy:
                continue
            deduped.append(item)
            last_xy = xy
        return deduped


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
        "map_name": os.path.splitext(os.path.basename(raw_map_name))[0],
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
