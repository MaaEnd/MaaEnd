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
from utils import (
    _R,
    _G,
    _Y,
    _C,
    _A,
    _0,
    Drawer,
    cv2,
    MapName,
    ViewportManager,
    Layer,
    BasePage,
    MapImageLayer,
    StatusRecord,
)

from utils import Button
from utils import ScrollableListWidget, TextInputWidget, PageStepper, StepPage, StepData

MAP_DIR = "assets/resource/image/MapTracker/map"
SERVICE_LOG_FILE = "install/debug/go-service.log"
NODE_TYPE_MOVE = "MapTrackerMove"
NODE_TYPE_ASSERT_LOCATION = "MapTrackerAssertLocation"


def _resolve_editor_map_name(map_name: str, map_dir: str) -> str:
    raw_name = str(map_name)
    basename = os.path.basename(raw_name.replace("\\", "/"))
    has_ext = os.path.splitext(basename)[1] != ""
    if has_ext:
        if os.path.exists(os.path.join(map_dir, raw_name)):
            return raw_name
        return find_map_file(raw_name, map_dir) or raw_name
    return find_map_file(raw_name, map_dir) or raw_name


def _handle_view_mouse(
    page: "PathEditPage | AreaEditPage",
    event: int,
    x: int,
    y: int,
    flags: int,
    mx: float,
    my: float,
) -> bool:
    # Mouse wheel: zoom around cursor focus point.
    if event == cv2.EVENT_MOUSEWHEEL:
        if flags > 0:
            page.view.zoom_in()
        else:
            page.view.zoom_out()
        page.view.set_view_origin(mx - x / page.view.zoom, my - y / page.view.zoom)
        page.render_page()
        return True

    # Right-drag panning.
    if event == cv2.EVENT_RBUTTONDOWN:
        page.panning = True
        page.pan_start = (x, y)
        return True
    if event == cv2.EVENT_RBUTTONUP:
        page.panning = False
        return True
    if event == cv2.EVENT_MOUSEMOVE and page.panning:
        dx = (x - page.pan_start[0]) / page.view.zoom
        dy = (y - page.pan_start[1]) / page.view.zoom
        page.view.pan_by(-dx, -dy)
        page.pan_start = (x, y)
        page.render_page()
        return True
    return False


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
                color=0x22BBFF,
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
            drawer.text(str(i), (sx + 5, sy - 5), 0.5, color=0xFFFFFF)


class PathEditPage(BasePage):
    """Path editing page"""

    SIDEBAR_W: int = 240
    STATUS_BAR_H: int = 32
    QUICK_BAR_H = 32
    LINE_WIDTH = 1.5
    POINT_RADIUS = 4.25
    POINT_SELECTION_THRESHOLD = 10

    @staticmethod
    def _coord1(value: float) -> float:
        return round(float(value), 1)

    def __init__(
        self,
        map_name,
        initial_points=None,
        map_dir=MAP_DIR,
        *,
        pipeline_context: dict | None = None,
        window_name: str = "MapTracker Tool - Path Editor",
    ):
        super().__init__(window_name, 1280, 720)
        self.map_name = _resolve_editor_map_name(str(map_name), map_dir)
        self.map_path = os.path.join(map_dir, self.map_name)
        self.img = cv2.imread(self.map_path)

        if self.img is None:
            raise ValueError(f"Cannot load map: {self.map_name}")

        self.view = ViewportManager(
            self.window_w, self.window_h, zoom=1.0, min_zoom=0.5, max_zoom=10.0
        )
        self._map_layer = MapImageLayer(self.view, self.img)
        self.panning = False
        self.pan_start = (0, 0)
        self._status = StatusRecord(
            time.time(), 0xFFFFFF, "Welcome to MapTracker Editor!"
        )

        self.points = [list(p) for p in initial_points] if initial_points else []
        self._point_snapshot: list[list] = [list(p) for p in self.points]
        self.pipeline_context = pipeline_context  # None → N mode
        self._path_layer = _PathLayer(self.view, self)
        self._realtime_layer = _RealtimePathLayer(self.view, self)
        self.view.fit_to(self.points)

        self.drag_idx = -1
        self.selected_idx = -1

        # Action state for point interactions (left button)
        self.action_down_idx = -1
        self.action_mouse_down = False
        self.action_down_pos = (0, 0)
        self.action_moved = False
        self.action_dragging = False

        self.location_service = LocationService()
        self._recording_active = False
        self._recording_start_time = 0.0
        self._recording_last_ts = 0.0
        self._recording_last_poll = 0.0
        self._recorded_path: list[list[float]] = []
        self._recorded_keys: set[tuple[float, float, float]] = set()

        # Button hit-rects: (x1, y1, x2, y2) – populated by _render_sidebar
        self._btn_save_rect: tuple | None = None
        self._btn_record_rect: tuple | None = None
        self._btn_back_rect: tuple | None = None
        self._btn_finish_rect: tuple | None = None
        self._btn_quick_generate_rect: tuple | None = None
        self._btn_quick_undo_rect: tuple | None = None
        self._quick_undo_state: dict | None = None

        # Sidebar action buttons rendered by BasePage.
        self._save_button = Button(
            (-100, -100, -90, -90),
            "[S] Save",
            base_color=0x3C643C,
            hotkey=(ord("s"), ord("S")),
            on_click=self._on_click_save,
            font_scale=0.45,
        )
        self._record_button = Button(
            (-100, -100, -90, -90),
            "[R] Record Realtime Path",
            base_color=0x1A40B8,
            hotkey=(ord("r"), ord("R")),
            on_click=self._on_click_record,
            font_scale=0.42,
        )
        self._back_button = Button(
            (-100, -100, -90, -90),
            "[Esc] Back",
            base_color=0x4C4C64,
            hotkey=27,
            on_click=self._on_click_back,
            font_scale=0.45,
        )
        self._finish_button = Button(
            (-100, -100, -90, -90),
            "[Enter] Finish",
            base_color=0xB44022,
            hotkey=(10, 13),
            on_click=self._on_click_finish,
            font_scale=0.45,
        )
        self.buttons.extend(
            [
                self._save_button,
                self._record_button,
                self._back_button,
                self._finish_button,
            ]
        )

    # ------------------------------------------------------------------
    # Helpers
    # ------------------------------------------------------------------

    @property
    def is_dirty(self) -> bool:
        """True when current points differ from the initial snapshot."""
        return self.points != self._point_snapshot

    def _do_save(self):
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
        self.render_page()

    def _stop_recording(self):
        self._recording_active = False
        self._update_status(0xD2D200, "Realtime path recording stopped.")
        self.render_page()

    def _toggle_recording(self):
        if self._recording_active:
            self._stop_recording()
        else:
            self._start_recording()

    def _on_click_save(self):
        if self.pipeline_context and self.is_dirty:
            self._do_save()
            self.render_page()

    def _on_click_record(self):
        self._toggle_recording()
        self.render_page()

    def _on_click_back(self):
        if self.stepper and len(self.stepper.step_history) > 1:
            self.stepper.pop_step()
        else:
            self.done = True

    def _on_click_finish(self):
        self.done = True

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
            key = (ts, x, y)
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
    def _can_simplify(
        prev_p: tuple[float, float],
        mid_p: tuple[float, float],
        next_p: tuple[float, float],
        k: float = 2.0,
    ) -> bool:
        if k < 1:
            raise ValueError("k must be >= 1")
        prev_next_dx, prev_next_dy = next_p[0] - prev_p[0], next_p[1] - prev_p[1]
        d_prev_next = math.hypot(prev_next_dx, prev_next_dy)
        if d_prev_next < (k - 1) + 1e-6:
            return True
        mid_next_dx, mid_next_dy = next_p[0] - mid_p[0], next_p[1] - mid_p[1]
        sin_prev_next_sub_mid_next = abs(
            prev_next_dx * mid_next_dy - prev_next_dy * mid_next_dx
        ) / (d_prev_next * math.hypot(mid_next_dx, mid_next_dy) + 1e-6)
        # y = arcsin(k / (x + 1)) -> sin(y) = k / (x + 1) -> sin(y) * (x + 1) = k
        return sin_prev_next_sub_mid_next * (d_prev_next + 1) < k

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
            if self._can_simplify(p2, p1, point):
                result.pop()  # Remove p1
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
        mx, my = self.view.get_real_coords(screen_x, screen_y)
        return self._coord1(mx), self._coord1(my)

    def _get_screen_coords(self, map_x, map_y):
        return self.view.get_view_coords(map_x, map_y)

    def _is_on_line(self, cmx, cmy, p1, p2, threshold=10):
        x1, y1 = p1
        x2, y2 = p2
        px, py = cmx, cmy
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
    # Rendering overrides
    # ------------------------------------------------------------------

    def _render(self, drawer: Drawer) -> None:
        self._map_layer.render(drawer)
        self._render_content(drawer)

        # Crosshair
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

        self._render_ui(drawer)

    def _render_content(self, drawer: Drawer) -> None:
        self._realtime_layer.render(drawer)
        self._path_layer.render(drawer)

    def _update_status(self, color, message: str) -> None:
        self._status = StatusRecord(time.time(), color, message)

    def _render_status_bar(self, drawer: Drawer) -> None:
        x1 = self.SIDEBAR_W
        x2 = self.window_w
        y2 = self.window_h
        y1 = max(0, y2 - self.STATUS_BAR_H)
        drawer.rect((x1, y1), (x2, y2), color=0x000000, thickness=-1)
        if self._status:
            drawer.text(
                self._status.message, (x1 + 10, y2 - 10), 0.45, color=self._status.color
            )

    def _render_sidebar_bg(self, drawer: Drawer) -> None:
        sw = self.SIDEBAR_W
        h = self.window_h
        drawer.rect((0, 0), (sw, h), color=0x000000, thickness=-1)
        drawer.line((sw - 1, 0), (sw - 1, h), color=0xFFFFFF, thickness=1)

    def _render_ui(self, drawer: Drawer) -> None:
        self._render_status_bar(drawer)
        self._render_sidebar_bg(drawer)
        self._render_quick_bar(drawer)
        self._render_sidebar(drawer)

    @staticmethod
    def _hit_button(x: int, y: int, rect: tuple[int, int, int, int] | None) -> bool:
        if rect is None:
            return False
        x1, y1, x2, y2 = rect
        return x1 <= x <= x2 and y1 <= y <= y2

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
            drawer.text(prompt, (x1 + 10, y2 - 10), 0.45, color=0xFFFFFF)

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
        drawer.text(prompt, (prompt_x, prompt_y), 0.45, color=0x50DC50)

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

    def _render_sidebar(self, drawer: "Drawer"):
        self._render_sidebar_bg(drawer)
        sw = self.SIDEBAR_W
        h = self.window_h
        pad = 15

        # ── Tips section ─────────────────────────────────────────────────
        cy = pad + 15
        drawer.text("[ Mouse Tips ]", (pad, cy), 0.5, color=0x40FFFF)
        cy += 10
        tips = [
            "Left Click: Add/Delete Point",
            "Left Drag: Move Point",
            "Right Drag: Move Map",
            "Scroll: Zoom",
        ]
        for line in tips:
            cy += 20
            drawer.text(line, (pad, cy), 0.4, color=0xC8C8C8)
        cy += 15  # small gap after tips

        # ── Buttons ──────────────────────────────────────────────────────
        btn_h = 30
        btn_w = sw - pad * 2
        btn_x0 = pad
        has_pipeline = self.pipeline_context is not None
        dirty = self.is_dirty

        hidden_rect = (-100, -100, -90, -90)
        self._save_button.rect = hidden_rect
        self._record_button.rect = hidden_rect
        self._back_button.rect = hidden_rect
        self._finish_button.rect = hidden_rect

        if has_pipeline:
            save_y0 = cy
            save_y1 = cy + btn_h
            self._btn_save_rect = (btn_x0, save_y0, btn_x0 + btn_w, save_y1)
            self._save_button.rect = self._btn_save_rect
            self._save_button.text = "[S] Save"
            self._save_button.base_color = 0x64C800 if dirty else 0x3C643C
            self._save_button.text_color = 0xFFFFFF if dirty else 0x648264
            cy = save_y1 + 8
        else:
            self._btn_save_rect = None

        record_y0 = cy
        record_y1 = cy + btn_h
        self._btn_record_rect = (btn_x0, record_y0, btn_x0 + btn_w, record_y1)
        self._record_button.rect = self._btn_record_rect
        self._record_button.base_color = 0x1A40B8
        self._record_button.text_color = 0xFFFFFF
        self._record_button.text = (
            "[R] Stop Path Recording"
            if self._recording_active
            else "[R] Record Realtime Path"
        )
        cy = record_y1 + 8

        back_y0 = cy
        back_y1 = cy + btn_h
        self._btn_back_rect = (btn_x0, back_y0, btn_x0 + btn_w, back_y1)
        self._back_button.rect = self._btn_back_rect
        self._back_button.text = "[Esc] Back"
        self._back_button.base_color = 0x4C4C64
        self._back_button.text_color = 0xFFFFFF
        cy = back_y1 + 8

        finish_y0 = cy
        finish_y1 = cy + btn_h
        self._btn_finish_rect = (btn_x0, finish_y0, btn_x0 + btn_w, finish_y1)
        self._finish_button.rect = self._btn_finish_rect
        self._finish_button.text = "[Enter] Finish"
        self._finish_button.base_color = 0xB44022
        self._finish_button.text_color = 0xFFFFFF

        # Status messages moved to map area status bar

        # ── Status section (bottom) ──────────────────────────────────────
        drawer.text(f"Zoom: {self.view.zoom:.2f}x", (pad, h - 75), 0.45, color=0xD2D200)

        if 0 <= self.selected_idx < len(self.points):
            p = self.points[self.selected_idx]
            line = f"Point #{self.selected_idx} ({p[0]:.1f}, {p[1]:.1f})"
        else:
            line = f"Points: {len(self.points)}"
        drawer.text(line, (pad, h - 50), 0.45, color=0xFFFFFF)
        record_line = f"History: {len(self._recorded_path)}"
        if self._recording_active:
            record_line += " (Recording)"
        drawer.text(record_line, (pad, h - 25), 0.4, color=0x8FC8FF)

    # ------------------------------------------------------------------
    # Mouse / keyboard / idle
    # ------------------------------------------------------------------

    def _get_point_at(self, x, y) -> int:
        for i, p in enumerate(self.points):
            sx, sy = self._get_screen_coords(p[0], p[1])
            dist = math.hypot(x - sx, y - sy)
            if dist < self.POINT_SELECTION_THRESHOLD:
                return i
        return -1

    def _on_mouse(self, event, x, y, flags, param) -> None:
        mx, my = self._get_map_coords(x, y)

        if _handle_view_mouse(self, event, x, y, flags, mx, my):
            return

        if event == cv2.EVENT_MOUSEMOVE:
            if self.action_mouse_down:
                if self.action_dragging and self.drag_idx != -1:
                    self.points[self.drag_idx] = [self._coord1(mx), self._coord1(my)]
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
                        self.points[self.drag_idx] = [
                            self._coord1(mx),
                            self._coord1(my),
                        ]
                        self.render_page()
                        return

            if (flags & cv2.EVENT_FLAG_LBUTTON) and self.drag_idx != -1:
                self.points[self.drag_idx] = [self._coord1(mx), self._coord1(my)]
                self.action_dragging = True
                self.render_page()
                return

            # Keep crosshair and hover feedback responsive.
            self.render_page()

        elif event == cv2.EVENT_LBUTTONDOWN:
            # Sidebar action buttons are handled by BasePage/Button.
            if x < self.SIDEBAR_W:
                return

            if self._hit_button(x, y, self._btn_quick_generate_rect):
                self._generate_path_from_recorded()
                self.render_page()
                return
            if self._hit_button(x, y, self._btn_quick_undo_rect):
                self._undo_generate_path()
                self.render_page()
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
                self.render_page()

        elif event == cv2.EVENT_LBUTTONUP:
            if self.action_dragging and self.drag_idx != -1:
                self.drag_idx = -1
            else:
                if not (self.action_moved and self.action_down_idx == -1):
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
                                f"Deleted Point #{del_idx} ({deleted_point[0]:.1f}, {deleted_point[1]:.1f})",
                            )
                            self.render_page()
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
                                self.points.insert(
                                    i, [self._coord1(mx), self._coord1(my)]
                                )
                                self.selected_idx = i
                                self._update_status(
                                    0x78DCFF,
                                    f"Added Point #{i} ({mx:.1f}, {my:.1f})",
                                )
                                inserted = True
                                self.render_page()
                                break
                        if not inserted:
                            self.points.append([self._coord1(mx), self._coord1(my)])
                            self.selected_idx = len(self.points) - 1
                            self._update_status(
                                0x78DCFF,
                                f"Added Point #{self.selected_idx} ({mx:.1f}, {my:.1f})",
                            )
                            self.render_page()

            self.action_down_idx = -1
            self.action_mouse_down = False
            self.action_down_pos = (0, 0)
            self.action_moved = False
            self.action_dragging = False

    def _on_key(self, key: int) -> None:
        if key == 27:  # Esc
            if self.stepper and len(self.stepper.step_history) > 1:
                self.stepper.pop_step()
            else:
                self.done = True
        elif key in (10, 13):  # Enter
            self.done = True
        elif key in (ord("s"), ord("S")) and self.pipeline_context and self.is_dirty:
            self._do_save()
            self.render_page()
        elif key in (ord("r"), ord("R")):
            self._toggle_recording()

    def _on_idle(self) -> None:
        self._update_recording()

    # ------------------------------------------------------------------
    # Main loop
    # ------------------------------------------------------------------

    def run(self) -> list[list]:
        super().run()
        return [list(p) for p in self.points]


class AreaEditPage(BasePage):
    SIDEBAR_W: int = 240
    STATUS_BAR_H: int = 32

    @staticmethod
    def _coord1(value: float) -> float:
        return round(float(value), 1)

    def __init__(
        self,
        map_name,
        initial_target=None,
        map_dir=MAP_DIR,
        *,
        pipeline_context: dict | None = None,
        window_name: str = "MapTracker Tool - Area Editor",
    ):
        super().__init__(window_name, 1280, 720)
        self.map_name = _resolve_editor_map_name(str(map_name), map_dir)
        self.map_path = os.path.join(map_dir, self.map_name)
        self.img = cv2.imread(self.map_path)
        if self.img is None:
            raise ValueError(f"Cannot load map: {self.map_name}")

        self.view = ViewportManager(
            self.window_w, self.window_h, zoom=1.0, min_zoom=0.5, max_zoom=10.0
        )
        self._map_layer = MapImageLayer(self.view, self.img)
        self.panning = False
        self.pan_start = (0, 0)
        self._status = StatusRecord(time.time(), 0xFFFFFF, "Welcome to Area Editor!")

        self.pipeline_context = pipeline_context
        self.target: list[float] | None = None
        if initial_target and len(initial_target) == 4:
            self.target = [self._coord1(v) for v in initial_target]
        self._target_snapshot = list(self.target) if self.target is not None else None

        self._drawing = False
        self._draw_start: tuple[float, float] | None = None

        self._save_button = Button(
            (-100, -100, -90, -90),
            "[S] Save",
            base_color=0x3C643C,
            hotkey=(ord("s"), ord("S")),
            on_click=self._on_click_save,
            font_scale=0.45,
        )
        self._back_button = Button(
            (-100, -100, -90, -90),
            "[Esc] Back",
            base_color=0x4C4C64,
            hotkey=27,
            on_click=self._on_click_back,
            font_scale=0.45,
        )
        self._finish_button = Button(
            (-100, -100, -90, -90),
            "[Enter] Finish",
            base_color=0xB44022,
            hotkey=(10, 13),
            on_click=self._on_click_finish,
            font_scale=0.45,
        )
        self.buttons.extend([self._save_button, self._back_button, self._finish_button])

    @property
    def is_dirty(self) -> bool:
        return self.target != self._target_snapshot

    def _get_map_coords(self, screen_x, screen_y):
        mx, my = self.view.get_real_coords(screen_x, screen_y)
        return self._coord1(mx), self._coord1(my)

    def _get_screen_coords(self, map_x, map_y):
        return self.view.get_view_coords(map_x, map_y)

    def _normalized_target(
        self, p1: tuple[float, float], p2: tuple[float, float]
    ) -> list[float]:
        x1, y1 = p1
        x2, y2 = p2
        left = min(x1, x2)
        top = min(y1, y2)
        w = abs(x2 - x1)
        h = abs(y2 - y1)
        return [self._coord1(left), self._coord1(top), self._coord1(w), self._coord1(h)]

    def _update_status(self, color, message: str) -> None:
        self._status = StatusRecord(time.time(), color, message)

    def _do_save(self):
        if self.pipeline_context is None or self.target is None:
            return
        handler: PipelineHandler = self.pipeline_context["handler"]
        node_name: str = self.pipeline_context["node_name"]
        raw_map_name = self.pipeline_context.get("original_map_name", self.map_name)
        map_name_stem = os.path.splitext(os.path.basename(raw_map_name))[0]
        if handler.replace_assert_location(node_name, map_name_stem, self.target):
            self._target_snapshot = list(self.target)
            self._update_status(0x50DC50, "Saved changes!")
            print(f"  {_G}Area saved to file.{_0}")
        else:
            self._update_status(0xFC4040, "Failed to save changes!")
            print(f"  {_Y}Failed to save area to file.{_0}")

    def _on_click_save(self):
        if self.pipeline_context and self.is_dirty and self.target is not None:
            self._do_save()
            self.render_page()

    def _on_click_back(self):
        if self.stepper and len(self.stepper.step_history) > 1:
            self.stepper.pop_step()
        else:
            self.done = True

    def _on_click_finish(self):
        self.done = True

    def _render_status_bar(self, drawer: Drawer) -> None:
        x1 = self.SIDEBAR_W
        x2 = self.window_w
        y2 = self.window_h
        y1 = max(0, y2 - self.STATUS_BAR_H)
        drawer.rect((x1, y1), (x2, y2), color=0x000000, thickness=-1)
        if self._status:
            drawer.text(
                self._status.message,
                (x1 + 10, y2 - 10),
                0.45,
                color=self._status.color,
                thickness=1,
            )

    def _render_sidebar_bg(self, drawer: Drawer) -> None:
        sw = self.SIDEBAR_W
        h = self.window_h
        drawer.rect((0, 0), (sw, h), color=0x000000, thickness=-1)
        drawer.line((sw - 1, 0), (sw - 1, h), color=0xFFFFFF, thickness=1)

    def _render_ui(self, drawer: Drawer) -> None:
        self._render_status_bar(drawer)
        self._render_sidebar_bg(drawer)

        sw = self.SIDEBAR_W
        h = self.window_h
        pad = 15
        cy = pad + 15
        drawer.text("[ Mouse Tips ]", (pad, cy), 0.5, color=0x40FFFF, thickness=1)
        cy += 10
        for line in [
            "Left Drag: Draw Rectangle",
            "Right Drag: Move Map",
            "Scroll: Zoom",
        ]:
            cy += 20
            drawer.text(line, (pad, cy), 0.4, color=0xC8C8C8, thickness=1)
        cy += 20

        btn_h = 30
        btn_w = sw - pad * 2
        btn_x0 = pad
        hidden_rect = (-100, -100, -90, -90)
        self._save_button.rect = hidden_rect
        self._back_button.rect = hidden_rect
        self._finish_button.rect = hidden_rect

        if self.pipeline_context is not None:
            self._save_button.rect = (btn_x0, cy, btn_x0 + btn_w, cy + btn_h)
            self._save_button.base_color = 0x64C800 if self.is_dirty else 0x3C643C
            self._save_button.text_color = 0xFFFFFF if self.is_dirty else 0x648264
            cy += btn_h + 8

        self._back_button.rect = (btn_x0, cy, btn_x0 + btn_w, cy + btn_h)
        cy += btn_h + 8
        self._finish_button.rect = (btn_x0, cy, btn_x0 + btn_w, cy + btn_h)

        drawer.text(
            f"Zoom: {self.view.zoom:.2f}x",
            (pad, h - 70),
            0.45,
            color=0xD2D200,
            thickness=1,
        )

    def _render(self, drawer: Drawer) -> None:
        self._map_layer.render(drawer)
        if self.target is not None:
            x, y, w, h = self.target
            p1 = self._get_screen_coords(x, y)
            p2 = self._get_screen_coords(x + w, y + h)
            x1, y1 = min(p1[0], p2[0]), min(p1[1], p2[1])
            x2, y2 = max(p1[0], p2[0]), max(p1[1], p2[1])
            drawer.mask(p1, p2, color=0x22BBFF, alpha=0.2)
            drawer.rect(p1, p2, color=0x22BBFF, thickness=2)

            origin_text = f"({x:.1f}, {y:.1f})"
            h_text = f"H={h:.1f}"
            w_text = f"W={w:.1f}"

            ox = max(self.SIDEBAR_W + 4, min(x1 + 4, self.window_w - 220))
            oy = max(20, y1 - 8)
            drawer.text(origin_text, (ox, oy), 0.45, color=0xFFFFFF, thickness=1)

            hx = max(self.SIDEBAR_W + 4, min(x1 + 4, self.window_w - 90))
            h_size = drawer.get_text_size(h_text, 0.45, thickness=1)
            hy = max(
                h_size[1] + 2,
                min(y2 + h_size[1] + 2, self.window_h - self.STATUS_BAR_H - 6),
            )
            drawer.text(h_text, (hx, hy), 0.45, color=0xA8F0FF, thickness=1)

            wx = max(self.SIDEBAR_W + 4, min(x2 + 8, self.window_w - 90))
            wy = max(20, min(y2 - 6, self.window_h - self.STATUS_BAR_H - 6))
            drawer.text(w_text, (wx, wy), 0.45, color=0xA8F0FF, thickness=1)

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
        self._render_ui(drawer)

    def _on_mouse(self, event, x, y, flags, param) -> None:
        mx, my = self._get_map_coords(x, y)

        if _handle_view_mouse(self, event, x, y, flags, mx, my):
            return

        if event == cv2.EVENT_LBUTTONDOWN:
            if x < self.SIDEBAR_W:
                return
            self._drawing = True
            self._draw_start = (mx, my)
            self.target = [mx, my, 0.0, 0.0]
            self.render_page()
            return

        if event == cv2.EVENT_MOUSEMOVE:
            if self._drawing and self._draw_start is not None:
                self.target = self._normalized_target(self._draw_start, (mx, my))
                self.render_page()
                return
            self.render_page()

        if event == cv2.EVENT_LBUTTONUP and self._drawing:
            self._drawing = False
            if self._draw_start is not None:
                self.target = self._normalized_target(self._draw_start, (mx, my))
                self._draw_start = None
                self._update_status(0x78DCFF, "Updated target area.")
                self.render_page()

    def _on_key(self, key: int) -> None:
        if key == 27:
            if self.stepper and len(self.stepper.step_history) > 1:
                self.stepper.pop_step()
            else:
                self.done = True
        elif key in (10, 13):
            self.done = True
        elif (
            key in (ord("s"), ord("S"))
            and self.pipeline_context
            and self.is_dirty
            and self.target is not None
        ):
            self._do_save()
            self.render_page()

    def run(self) -> list[float] | None:
        super().run()
        return list(self.target) if self.target is not None else None


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
        if not self._is_target_message(data_obj.get("message") or data_obj.get("msg")):
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
        try:
            x = float(x)
            y = float(y)
        except (TypeError, ValueError):
            return None

        ts = None
        for key in ("time", "timestamp", "ts"):
            if key in data_obj:
                ts = self._parse_timestamp(data_obj.get(key))
                if ts is not None:
                    break

        return {
            "x": x,
            "y": y,
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
        last_xy: tuple[float, float] | None = None
        for item in results:
            x = item.get("x")
            y = item.get("y")
            if x is None or y is None:
                continue
            xy = (round(x, 1), round(y, 1))
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
        try:
            with open(self.file_path, "r", encoding="utf-8") as f:
                self._content = f.read()
            return True
        except Exception as e:
            print(f"{_R}Error reading file:{_0} {e}")
            return False

    @staticmethod
    def _extract_json_array(text: str, field_name: str) -> tuple[int, int, str] | None:
        key_match = re.search(r'"' + re.escape(field_name) + r'"\s*:\s*\[', text)
        if not key_match:
            return None
        start = text.find("[", key_match.start())
        if start < 0:
            return None

        i = start
        depth = 0
        in_str = False
        escape = False
        while i < len(text):
            ch = text[i]
            if in_str:
                if escape:
                    escape = False
                elif ch == "\\":
                    escape = True
                elif ch == '"':
                    in_str = False
            else:
                if ch == '"':
                    in_str = True
                elif ch == "[":
                    depth += 1
                elif ch == "]":
                    depth -= 1
                    if depth == 0:
                        end = i + 1
                        return (start, end, text[start:end])
            i += 1
        return None

    @staticmethod
    def _find_matching_brace(text: str, start: int) -> int:
        i = start
        depth = 0
        in_str = False
        escape = False
        while i < len(text):
            ch = text[i]
            if in_str:
                if escape:
                    escape = False
                elif ch == "\\":
                    escape = True
                elif ch == '"':
                    in_str = False
            else:
                if ch == '"':
                    in_str = True
                elif ch == "{":
                    depth += 1
                elif ch == "}":
                    depth -= 1
                    if depth == 0:
                        return i
            i += 1
        return -1

    @staticmethod
    def _iter_top_level_nodes(content: str) -> list[tuple[str, str]]:
        nodes: list[tuple[str, str]] = []
        root_start = content.find("{")
        if root_start < 0:
            return nodes

        i = root_start + 1
        n = len(content)
        while i < n:
            while i < n and content[i] in " \t\r\n,":
                i += 1
            if i >= n or content[i] == "}":
                break
            if content[i] != '"':
                i += 1
                continue

            # Parse key string
            key_start = i + 1
            i += 1
            escape = False
            while i < n:
                ch = content[i]
                if escape:
                    escape = False
                elif ch == "\\":
                    escape = True
                elif ch == '"':
                    break
                i += 1
            if i >= n:
                break
            key = content[key_start:i]
            i += 1

            while i < n and content[i] in " \t\r\n":
                i += 1
            if i >= n or content[i] != ":":
                continue
            i += 1

            while i < n and content[i] in " \t\r\n":
                i += 1
            if i >= n or content[i] != "{":
                continue

            obj_start = i
            obj_end = PipelineHandler._find_matching_brace(content, obj_start)
            if obj_end < 0:
                break
            node_content = content[obj_start : obj_end + 1]
            nodes.append((key, node_content))
            i = obj_end + 1

        return nodes

    @staticmethod
    def _find_top_level_node_bounds(
        content: str, node_name: str
    ) -> tuple[int, int, str] | None:
        root_start = content.find("{")
        if root_start < 0:
            return None

        i = root_start + 1
        n = len(content)
        while i < n:
            while i < n and content[i] in " \t\r\n,":
                i += 1
            if i >= n or content[i] == "}":
                break
            if content[i] != '"':
                i += 1
                continue

            key_start = i + 1
            i += 1
            escape = False
            while i < n:
                ch = content[i]
                if escape:
                    escape = False
                elif ch == "\\":
                    escape = True
                elif ch == '"':
                    break
                i += 1
            if i >= n:
                break
            key = content[key_start:i]
            i += 1

            while i < n and content[i] in " \t\r\n":
                i += 1
            if i >= n or content[i] != ":":
                continue
            i += 1

            while i < n and content[i] in " \t\r\n":
                i += 1
            if i >= n or content[i] != "{":
                continue

            obj_start = i
            obj_end = PipelineHandler._find_matching_brace(content, obj_start)
            if obj_end < 0:
                break
            if key == node_name:
                return (obj_start, obj_end + 1, content[obj_start : obj_end + 1])
            i = obj_end + 1

        return None

    @staticmethod
    def _parse_tracker_move_fields(node_content: str) -> dict | None:
        if f'"custom_action": "{NODE_TYPE_MOVE}"' not in node_content:
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
            "node_type": NODE_TYPE_MOVE,
            "map_name": map_name,
            "path": path,
            "is_new_structure": is_new_structure,
        }

    @staticmethod
    def _parse_assert_location_fields(node_content: str) -> dict | None:
        if f'"custom_recognition": "{NODE_TYPE_ASSERT_LOCATION}"' not in node_content:
            return None

        expected_range = PipelineHandler._extract_json_array(node_content, "expected")
        if expected_range is None:
            return None
        try:
            expected = json.loads(expected_range[2])
        except Exception:
            return None
        if not isinstance(expected, list) or len(expected) == 0:
            return None
        first = expected[0]
        if not isinstance(first, dict):
            return None
        map_name = first.get("map_name")
        target = first.get("target")
        if (
            not isinstance(map_name, str)
            or not isinstance(target, list)
            or len(target) != 4
        ):
            return None

        return {
            "node_type": NODE_TYPE_ASSERT_LOCATION,
            "map_name": map_name,
            "target": [float(v) for v in target],
            "expected": expected,
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
        for node_name, node_content in self._iter_top_level_nodes(self._content):
            entry: dict = {"content": node_content}
            tracker = self._parse_tracker_move_fields(node_content)
            if tracker is None:
                tracker = self._parse_assert_location_fields(node_content)
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
                        "node_type": entry.get("node_type", NODE_TYPE_MOVE),
                        "map_name": entry["map_name"],
                        "path": entry.get("path", []),
                        "target": entry.get("target"),
                        "is_new_structure": entry.get("is_new_structure", False),
                    }
                )
        return results

    def get_tracker_nodes(self) -> list[dict]:
        """Return a list of all MapTrackerMove node summaries (same shape as read_nodes)."""
        return [
            {
                "node_name": name,
                "node_type": entry.get("node_type", NODE_TYPE_MOVE),
                "map_name": entry["map_name"],
                "path": entry.get("path", []),
                "target": entry.get("target"),
                "is_new_structure": entry.get("is_new_structure", False),
            }
            for name, entry in self.nodes.items()
            if entry.get("is_tracker")
        ]

    def _replace_node_body(self, node_name: str, new_body: str) -> bool:
        bounds = self._find_top_level_node_bounds(self._content, node_name)
        if bounds is None:
            print(f"{_R}Error: Node {node_name} not found in file when saving.{_0}")
            return False
        node_start, node_end, _ = bounds
        self._content = self._content[:node_start] + new_body + self._content[node_end:]
        return True

    def replace_path(self, node_name: str, new_path: list) -> bool:
        """Regex-replace the path list for *node_name* in the pipeline file.

        Updates ``self.nodes`` on success so the in-memory state stays current.
        """
        if not self._load():
            return False

        bounds = self._find_top_level_node_bounds(self._content, node_name)
        if bounds is None:
            print(f"{_R}Error: Node {node_name} not found in file when saving.{_0}")
            return False
        _, _, body = bounds

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
                formatted_path += f"{indent_lg}[{p[0]:.1f}, {p[1]:.1f}]{comma}\n"
            formatted_path += f"{indent_sm}]"

        new_body = (
            body[: path_match.start(2)] + formatted_path + body[path_match.end(2) :]
        )
        if not self._replace_node_body(node_name, new_body):
            return False

        try:
            with open(self.file_path, "w", encoding="utf-8") as f:
                f.write(self._content)
        except Exception as e:
            print(f"{_R}Error writing file:{_0} {e}")
            return False

        # Keep in-memory state consistent
        if node_name in self.nodes:
            self.nodes[node_name]["path"] = [
                [round(p[0], 1), round(p[1], 1)] for p in new_path
            ]
        return True

    def replace_assert_location(
        self, node_name: str, map_name: str, target: list[float]
    ) -> bool:
        if not self._load():
            return False

        bounds = self._find_top_level_node_bounds(self._content, node_name)
        if bounds is None:
            print(f"{_R}Error: Node {node_name} not found in file when saving.{_0}")
            return False
        _, _, body = bounds

        expected_range = self._extract_json_array(body, "expected")
        if expected_range is None:
            print(
                f"{_R}Error: 'expected' field not found in node {node_name} when saving.{_0}"
            )
            return False

        try:
            expected = json.loads(expected_range[2])
        except Exception:
            print(
                f"{_R}Error: failed to parse 'expected' field in node {node_name}.{_0}"
            )
            return False
        if (
            not isinstance(expected, list)
            or len(expected) == 0
            or not isinstance(expected[0], dict)
        ):
            print(f"{_R}Error: invalid 'expected' structure in node {node_name}.{_0}")
            return False

        expected[0]["map_name"] = map_name
        expected[0]["target"] = [round(float(v), 1) for v in target]

        formatted_expected = json.dumps(expected, ensure_ascii=False, indent=4)
        line_start = body.rfind("\n", 0, expected_range[0]) + 1
        line_prefix = body[line_start : expected_range[0]]
        base_indent = re.match(r"[ \t]*", line_prefix).group(0)
        formatted_expected = formatted_expected.replace("\n", "\n" + base_indent)

        new_body = (
            body[: expected_range[0]] + formatted_expected + body[expected_range[1] :]
        )
        if not self._replace_node_body(node_name, new_body):
            return False

        try:
            with open(self.file_path, "w", encoding="utf-8") as f:
                f.write(self._content)
        except Exception as e:
            print(f"{_R}Error writing file:{_0} {e}")
            return False

        if node_name in self.nodes:
            self.nodes[node_name]["map_name"] = map_name
            self.nodes[node_name]["target"] = [round(float(v), 1) for v in target]
        return True


class ModeSelectStep(StepPage):
    def __init__(self):
        super().__init__(StepData("mode", "Select Mode", can_go_back=False))

    def _render_content(self, drawer):
        drawer.text_centered(
            "Choose an operation mode:",
            (self.WINDOW_W // 2, 180),
            0.8,
            color=0xDDDDDD,
            thickness=2,
        )
        btn_w, btn_h = 280, 90
        spacing = 30
        total_w = btn_w * 3 + spacing * 2
        start_x = (self.WINDOW_W - total_w) // 2
        y1 = 280

        if not self.buttons:
            self.buttons.append(
                Button(
                    (start_x, y1, start_x + btn_w, y1 + btn_h),
                    "Create New Path (N)",
                    base_color=0x334455,
                    hotkey=ord("n"),
                    on_click=lambda: self.stepper.push_step(
                        MapSelectStep(node_type=NODE_TYPE_MOVE)
                    ),
                )
            )
            self.buttons.append(
                Button(
                    (
                        start_x + btn_w + spacing,
                        y1,
                        start_x + btn_w * 2 + spacing,
                        y1 + btn_h,
                    ),
                    "Import from Pipeline (I)",
                    base_color=0x554433,
                    hotkey=ord("i"),
                    on_click=lambda: self.stepper.push_step(FileSelectStep()),
                )
            )
            self.buttons.append(
                Button(
                    (
                        start_x + (btn_w + spacing) * 2,
                        y1,
                        start_x + (btn_w + spacing) * 2 + btn_w,
                        y1 + btn_h,
                    ),
                    "Create Assert Area (A)",
                    base_color=0x355536,
                    hotkey=ord("a"),
                    on_click=lambda: self.stepper.push_step(
                        MapSelectStep(node_type=NODE_TYPE_ASSERT_LOCATION)
                    ),
                )
            )


class MapSelectStep(StepPage):
    def __init__(self, *, node_type: str = NODE_TYPE_MOVE):
        title = (
            "Select Map for Path"
            if node_type == NODE_TYPE_MOVE
            else "Select Map for Assert Area"
        )
        super().__init__(StepData("map_select", title))
        self.node_type = node_type
        self.map_list = ScrollableListWidget(item_height=40)
        self._map_preview_cache: dict[str, object] = {}
        try:
            map_files = [
                f for f in os.listdir(MAP_DIR) if f.lower().endswith((".png", ".jpg"))
            ]
            map_files.sort(key=lambda name: (len(name), name.lower()))
            self.map_list.set_items(
                [{"label": m, "sub_label": "", "data": m} for m in map_files]
            )
        except Exception:
            pass

        self.map_list.set_preview_generator(self._generate_map_preview)

    def _generate_map_preview(self, item: dict):
        map_name = str(item.get("data") or "")
        if map_name == "":
            return None
        if map_name in self._map_preview_cache:
            return self._map_preview_cache[map_name]
        map_path = os.path.join(MAP_DIR, map_name)
        img = cv2.imread(map_path, cv2.IMREAD_UNCHANGED)
        if img is None:
            self._map_preview_cache[map_name] = None
            return None
        self._map_preview_cache[map_name] = img
        return img

    def _render_content(self, drawer):
        self.map_list.render(
            drawer, (50, 100, self.WINDOW_W - 50, self.WINDOW_H - self.FOOTER_H - 20)
        )

    def _handle_content_mouse(self, event, x, y, flags, param):
        rect = (50, 100, self.WINDOW_W - 50, self.WINDOW_H - self.FOOTER_H - 20)
        if event == cv2.EVENT_LBUTTONDOWN:
            idx = self.map_list.handle_click(x, y, rect)
            if idx >= 0:
                self._submit(self.map_list.items[idx]["data"])
        elif event == cv2.EVENT_MOUSEWHEEL:
            if self.map_list.handle_wheel(x, y, flags, rect):
                self.stepper.request_render()

    def _handle_content_key(self, key):
        is_up = self.is_up_key(key)
        is_down = self.is_down_key(key)
        if is_up or is_down:
            self.map_list.navigate(-1 if is_up else 1)
            self.stepper.request_render()
        elif key in (10, 13) and self.map_list.selected_idx >= 0:
            self._submit(self.map_list.items[self.map_list.selected_idx]["data"])

    def _submit(self, map_name):
        if self.node_type == NODE_TYPE_ASSERT_LOCATION:
            self.stepper.push_step(RegionEditorAdapterStep(map_name, mode="create"))
        else:
            self.stepper.push_step(EditorAdapterStep(map_name, mode="create"))


class FileSelectStep(StepPage):
    def __init__(self):
        super().__init__(StepData("file_select", "Select Pipeline JSON"))
        self.file_list = ScrollableListWidget(item_height=40)
        self.search_input = TextInputWidget("Search JSON files...")
        self._all_files = []
        pipeline_dir = "assets/resource/pipeline"
        if os.path.exists(pipeline_dir):
            for root, _, files in os.walk(pipeline_dir):
                for f in files:
                    if f.endswith(".json"):
                        path = os.path.join(root, f)
                        enabled = self._is_eligible_pipeline_file(path)
                        self._all_files.append(
                            {
                                "label": f,
                                "sub_label": os.path.relpath(
                                    path, pipeline_dir
                                ).replace(os.path.sep, "/"),
                                "data": path,
                                "disabled": not enabled,
                            }
                        )
        self._all_files.sort(
            key=lambda x: (
                bool(x.get("disabled", False)),
                str(x.get("sub_label", "")).lower(),
                str(x.get("label", "")).lower(),
            )
        )
        self.file_list.set_items(self._all_files)

    @staticmethod
    def _is_eligible_pipeline_file(file_path: str) -> bool:
        try:
            size = os.path.getsize(file_path)
            if size >= 1024 * 1024:
                return False
            with open(file_path, "r", encoding="utf-8", errors="ignore") as f:
                content = f.read()
            return NODE_TYPE_MOVE in content or NODE_TYPE_ASSERT_LOCATION in content
        except Exception:
            return False

    def _render_content(self, drawer):
        self.search_input.render(drawer, (50, 100, self.WINDOW_W - 50, 140))
        self.file_list.render(
            drawer, (50, 160, self.WINDOW_W - 50, self.WINDOW_H - self.FOOTER_H - 20)
        )

    def _handle_content_mouse(self, event, x, y, flags, param):
        rect = (50, 160, self.WINDOW_W - 50, self.WINDOW_H - self.FOOTER_H - 20)
        if event == cv2.EVENT_LBUTTONDOWN:
            idx = self.file_list.handle_click(x, y, rect)
            if idx >= 0:
                self.stepper.push_step(
                    NodeSelectStep(self.file_list.items[idx]["data"])
                )
        elif event == cv2.EVENT_MOUSEWHEEL:
            if self.file_list.handle_wheel(x, y, flags, rect):
                self.stepper.request_render()

    def _handle_content_key(self, key):
        if self.search_input.handle_key(key):
            q = self.search_input.text.lower()
            filtered = [
                f
                for f in self._all_files
                if q in f["label"].lower() or q in f["sub_label"].lower()
            ]
            self.file_list.set_items(filtered)
            self.stepper.request_render()
            return
        is_up = self.is_up_key(key)
        is_down = self.is_down_key(key)
        if is_up or is_down:
            self.file_list.navigate(-1 if is_up else 1)
            self.stepper.request_render()
        elif key in (10, 13) and self.file_list.selected_idx >= 0:
            self.stepper.push_step(
                NodeSelectStep(
                    self.file_list.items[self.file_list.selected_idx]["data"]
                )
            )


class NodeSelectStep(StepPage):
    def __init__(self, file_path):
        super().__init__(
            StepData("node_select", f"Select Node from {os.path.basename(file_path)}")
        )
        self.file_path = file_path
        self.node_list = ScrollableListWidget(item_height=40)
        self.handler = PipelineHandler(file_path)
        nodes = self.handler.read_nodes()
        self.candidates = nodes
        self.node_list.set_items(
            [
                {
                    "label": n["node_name"],
                    "sub_label": self._build_node_sub_label(n),
                    "data": n["node_name"],
                }
                for n in nodes
            ]
        )

    @staticmethod
    def _build_node_sub_label(node: dict) -> str:
        node_type = node.get("node_type", NODE_TYPE_MOVE)
        map_name = node.get("map_name", "Unknown")
        if node_type == NODE_TYPE_ASSERT_LOCATION:
            return f"Type: {NODE_TYPE_ASSERT_LOCATION} | Map: {map_name}"
        path = node.get("path", [])
        return f"Type: {NODE_TYPE_MOVE} | Map: {map_name} | Pts: {len(path)}"

    def _render_content(self, drawer):
        self.node_list.render(
            drawer, (50, 100, self.WINDOW_W - 50, self.WINDOW_H - self.FOOTER_H - 20)
        )

    def _handle_content_mouse(self, event, x, y, flags, param):
        rect = (50, 100, self.WINDOW_W - 50, self.WINDOW_H - self.FOOTER_H - 20)
        if event == cv2.EVENT_LBUTTONDOWN:
            idx = self.node_list.handle_click(x, y, rect)
            if idx >= 0:
                self._submit(idx)
        elif event == cv2.EVENT_MOUSEWHEEL:
            if self.node_list.handle_wheel(x, y, flags, rect):
                self.stepper.request_render()

    def _handle_content_key(self, key):
        is_up = self.is_up_key(key)
        is_down = self.is_down_key(key)
        if is_up or is_down:
            self.node_list.navigate(-1 if is_up else 1)
            self.stepper.request_render()
        elif key in (10, 13) and self.node_list.selected_idx >= 0:
            self._submit(self.node_list.selected_idx)

    def _submit(self, idx):
        selected = self.candidates[idx]
        import_context = {
            "file_path": self.file_path,
            "handler": self.handler,
            "node_name": selected["node_name"],
            "original_map_name": selected["map_name"],
            "is_new_structure": selected.get("is_new_structure", False),
            "node_type": selected.get("node_type", NODE_TYPE_MOVE),
        }
        if selected.get("node_type") == NODE_TYPE_ASSERT_LOCATION:
            self.stepper.push_step(
                RegionEditorAdapterStep(
                    selected["map_name"],
                    mode="import",
                    import_context=import_context,
                    initial_target=selected.get("target"),
                )
            )
            return

        self.stepper.push_step(
            EditorAdapterStep(
                selected["map_name"],
                mode="import",
                import_context=import_context,
                initial_points=selected.get("path", []),
            )
        )


class EditorAdapterStep(BasePage):
    """Adapts PathEditPage directly into Stepper loop!"""

    def __init__(
        self, map_name, mode="create", import_context=None, initial_points=None
    ):
        super().__init__("MapTracker App", 1280, 720)
        self.map_name = map_name
        self.mode = mode
        self.import_context = import_context
        self.initial_points = initial_points or []
        self.editor = None
        self._finished_once = False

    def on_enter(self, stepper: PageStepper):
        """Create (if needed) and enter the embedded path editor."""
        if not self.editor:
            self.editor = PathEditPage(
                self.map_name,
                self.initial_points,
                window_name=stepper.window_name,
                pipeline_context=self.import_context if self.import_context else None,
            )
        # Returning from ExportStep should allow finishing again.
        self._finished_once = False
        self.editor.done = False
        self.editor.on_enter(stepper)

    def on_exit(self):
        """Forward exit lifecycle to the embedded editor."""
        if self.editor:
            self.editor.on_exit()

    def render(self):
        """Render editor frame and handle transition to export step."""
        if self.editor is None:
            return None
        if self.editor.done and not self._finished_once:
            self._finished_once = True
            self.editor.stepper.push_step(
                ExportStep(
                    self.editor.points,
                    self.import_context,
                    self.map_name,
                    node_type=NODE_TYPE_MOVE,
                )
            )
            return None
        return self.editor.render()

    def handle_mouse(self, event, x, y, flags, param):
        """Forward mouse events to the embedded editor."""
        if self.editor is None:
            return
        self.editor.handle_mouse(event, x, y, flags, param)

    def handle_key(self, key):
        """Handle adapter-level shortcuts and forward remaining keys."""
        if self.editor is None:
            return
        if key == 27:
            # We want ESC to mean "BACK to wizard"!
            self.editor.stepper.pop_step()
            return
        elif key == 13:  # Enter = Next (Export)
            # Advance to Export step if we want to save
            self.editor.stepper.push_step(
                ExportStep(
                    self.editor.points,
                    self.import_context,
                    self.map_name,
                    node_type=NODE_TYPE_MOVE,
                )
            )
            return
        self.editor.handle_key(key)

    def handle_idle(self):
        """Forward idle ticks to the embedded editor."""
        if self.editor is None:
            return
        self.editor.handle_idle()


class ExportStep(StepPage):
    def __init__(
        self, points, import_context, map_name, *, node_type: str = NODE_TYPE_MOVE
    ):
        super().__init__(StepData("export", "Export / Save Result"))
        self.points = points
        self.import_context = import_context
        self.map_name = map_name
        self.node_type = node_type

        self.options = [
            {
                "label": (
                    "Just Save to File (Replace path)"
                    if node_type == NODE_TYPE_MOVE
                    else "Just Save to File (Replace target)"
                ),
                "data": "S",
                "disabled": import_context is None,
            },
            {"label": "Print Context Dict", "data": "D"},
            {"label": "Print Node JSON", "data": "J"},
            {
                "label": (
                    "Print Point List"
                    if node_type == NODE_TYPE_MOVE
                    else "Print Target Rect"
                ),
                "data": "L",
            },
        ]
        self.list_widget = ScrollableListWidget(45)
        self.list_widget.set_items(self.options)
        self.saved_text = ""

    def _render_content(self, drawer):
        self.list_widget.render(drawer, (100, 150, self.WINDOW_W - 100, 350))
        if self.saved_text:
            drawer.text_centered(
                self.saved_text,
                (self.WINDOW_W // 2, 450),
                0.8,
                color=0x50DC50,
                thickness=2,
            )

    def _handle_content_mouse(self, event, x, y, flags, param):
        rect = (100, 150, self.WINDOW_W - 100, 350)
        if event == cv2.EVENT_LBUTTONDOWN:
            idx = self.list_widget.handle_click(x, y, rect)
            if idx >= 0:
                self._submit(self.list_widget.items[idx]["data"])

    def _handle_content_key(self, key):
        if key in (10, 13) and self.list_widget.selected_idx >= 0:
            self._submit(self.list_widget.items[self.list_widget.selected_idx]["data"])
        elif key in (82, 0x260000, 65362):
            self.list_widget.navigate(-1)
            self.stepper.request_render()
        elif key in (84, 0x280000, 65364):
            self.list_widget.navigate(1)
            self.stepper.request_render()

    def _submit(self, mode):
        if mode == "S":
            handler = self.import_context["handler"]
            node_name = self.import_context["node_name"]
            if self.node_type == NODE_TYPE_ASSERT_LOCATION:
                raw_map_name = self.import_context.get(
                    "original_map_name", self.map_name
                )
                map_name_stem = os.path.splitext(os.path.basename(raw_map_name))[0]
                ok = handler.replace_assert_location(
                    node_name, map_name_stem, self.points
                )
            else:
                ok = handler.replace_path(node_name, self.points)
            if ok:
                self.saved_text = f"Successfully updated node '{node_name}'!"
                print(f"\n{_G}Successfully updated node {_0}'{node_name}'")
            else:
                self.saved_text = "Failed to update node!"
            self.stepper.request_render()

        elif mode == "J":
            raw_map_name = (
                self.import_context.get("original_map_name", self.map_name)
                if self.import_context
                else self.map_name
            )
            map_stem = os.path.splitext(os.path.basename(raw_map_name))[0]
            if self.node_type == NODE_TYPE_ASSERT_LOCATION:
                param_data = {
                    "expected": [
                        {
                            "map_name": map_stem,
                            "target": [round(float(v), 1) for v in self.points],
                        }
                    ]
                }
                node_data = {
                    "recognition": "Custom",
                    "custom_recognition": NODE_TYPE_ASSERT_LOCATION,
                    "custom_recognition_param": param_data,
                    "action": "DoNothing",
                }
            else:
                param_data = {
                    "map_name": map_stem,
                    "path": [[round(p[0], 1), round(p[1], 1)] for p in self.points],
                }
                is_new = (
                    self.import_context.get("is_new_structure", False)
                    if self.import_context
                    else False
                )
                if is_new:
                    node_data = {
                        "action": {
                            "custom_action": NODE_TYPE_MOVE,
                            "custom_action_param": param_data,
                        }
                    }
                else:
                    node_data = {
                        "action": "Custom",
                        "custom_action": NODE_TYPE_MOVE,
                        "custom_action_param": param_data,
                    }
            print(f"\n{_C}--- JSON Snippet ---{_0}\n")
            print(json.dumps({"NodeName": node_data}, indent=4, ensure_ascii=False))
            self.saved_text = "JSON output printed to terminal!"
            self.stepper.request_render()

        elif mode == "D":
            raw_map_name = (
                self.import_context.get("original_map_name", self.map_name)
                if self.import_context
                else self.map_name
            )
            map_stem = os.path.splitext(os.path.basename(raw_map_name))[0]
            if self.node_type == NODE_TYPE_ASSERT_LOCATION:
                param_data = {
                    "expected": [
                        {
                            "map_name": map_stem,
                            "target": [round(float(v), 1) for v in self.points],
                        }
                    ]
                }
            else:
                param_data = {
                    "map_name": map_stem,
                    "path": [[round(p[0], 1), round(p[1], 1)] for p in self.points],
                }
            print(f"\n{_C}--- Parameters Dict ---{_0}\n")
            print(json.dumps(param_data, indent=4, ensure_ascii=False))
            self.saved_text = "Dict output printed to terminal!"
            self.stepper.request_render()

        elif mode == "L":
            if self.node_type == NODE_TYPE_ASSERT_LOCATION:
                target_rect = [round(float(v), 1) for v in self.points]
                print(f"\n{_C}--- Target Rect ---{_0}\n")
                print(target_rect)
                self.saved_text = "Target rect printed to terminal!"
            else:
                point_list = [[round(p[0], 1), round(p[1], 1)] for p in self.points]
                print(f"\n{_C}--- Point List ---{_0}\n")
                print(point_list)
                self.saved_text = "Point list printed to terminal!"
            self.stepper.request_render()


class RegionEditorAdapterStep(BasePage):
    def __init__(
        self, map_name, mode="create", import_context=None, initial_target=None
    ):
        super().__init__("MapTracker App", 1280, 720)
        self.map_name = map_name
        self.mode = mode
        self.import_context = import_context
        self.initial_target = initial_target
        self.editor = None
        self._finished_once = False

    def on_enter(self, stepper: PageStepper):
        if not self.editor:
            self.editor = AreaEditPage(
                self.map_name,
                self.initial_target,
                window_name=stepper.window_name,
                pipeline_context=self.import_context if self.import_context else None,
            )
        self._finished_once = False
        self.editor.done = False
        self.editor.on_enter(stepper)

    def on_exit(self):
        if self.editor:
            self.editor.on_exit()

    def render(self):
        if self.editor is None:
            return None
        if self.editor.done and not self._finished_once:
            self._finished_once = True
            target = (
                self.editor.target
                if self.editor.target is not None
                else [0.0, 0.0, 0.0, 0.0]
            )
            self.editor.stepper.push_step(
                ExportStep(
                    target,
                    self.import_context,
                    self.map_name,
                    node_type=NODE_TYPE_ASSERT_LOCATION,
                )
            )
            return None
        return self.editor.render()

    def handle_mouse(self, event, x, y, flags, param):
        if self.editor is None:
            return
        self.editor.handle_mouse(event, x, y, flags, param)

    def handle_key(self, key):
        if self.editor is None:
            return
        if key == 27:
            self.editor.stepper.pop_step()
            return
        elif key in (10, 13):
            target = (
                self.editor.target
                if self.editor.target is not None
                else [0.0, 0.0, 0.0, 0.0]
            )
            self.editor.stepper.push_step(
                ExportStep(
                    target,
                    self.import_context,
                    self.map_name,
                    node_type=NODE_TYPE_ASSERT_LOCATION,
                )
            )
            return
        self.editor.handle_key(key)

    def handle_idle(self):
        if self.editor is None:
            return
        self.editor.handle_idle()


class App(PageStepper):
    def __init__(self):
        super().__init__("MapTracker App")
        self.points = []
        self.import_context = None


def main():
    app = App()
    app.push_step(ModeSelectStep())
    app.run()


if __name__ == "__main__":
    main()
